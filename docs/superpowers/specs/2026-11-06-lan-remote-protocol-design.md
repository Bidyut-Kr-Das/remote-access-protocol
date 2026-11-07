# Remote Access Protocol (RAP) — Design (v1)

Date: 2026-11-06
Status: approved

## Goal

Own remote-control protocol for Windows machines where the built-in Windows remote desktop host is unavailable (Home edition) and AnyDesk-class tools are unreliable. v1 scope: computer A views computer B's screen and drives B's keyboard and mouse over the same LAN, and A can hand keyboard/mouse back to itself with a hotkey. Over-internet transport, dirty-rect encoding and Windows-account auth are later versions and must not require wire changes to the messages defined here.

Language: Go 1.22+. Both ends Windows.

## Non-goals (v1)

- Internet / NAT traversal / relay.
- TLS. Password is challenge-hashed; frame data is plaintext on LAN.
- Login screen, UAC prompts, session 0 capture.
- Clipboard, audio, file transfer, multi-monitor.
- Auto-reconnect.
- Forwarding Win key, Alt+Tab, Ctrl+Alt+Del from A (OS keeps them).

## Topology

- B runs `raphost.exe`, listens on one TCP port (default 7700).
- A runs `rapview.exe`, opens one TCP connection to B.
- One session at a time on B.

## Wire format

Every message:

```
[u32 BE len][u8 type][payload]
```

`len` = 1 + len(payload). Max `len` 16 MiB; larger → receiver closes socket. All integers big-endian.

### Handshake (identical on both sockets)

1. B → A `CHALLENGE { [32]byte nonce }` immediately after accept. Nonce from `crypto/rand`, unique per socket.
2. A → B `HELLO { u8 version=1, [32]byte hash }` where `hash = SHA256(password_bytes || nonce)`.
3. B → A `OK { u16 screen_w, u16 screen_h }` or `DENY {}` then B closes.

Rules on B:
- Wrong hash → DENY. 3 DENY from same source IP → ignore that IP (close on accept) for 60 s.
- Unknown version → DENY.
- HELLO while a session is active → DENY.
- B closes a connection that does not finish the handshake within 5 s.

### Message types

| type | name         | dir  | payload |
|------|--------------|------|---------|
| 0x01 | CHALLENGE    | B→A  | `[32]byte nonce` |
| 0x02 | HELLO        | A→B  | `u8 version, [32]byte hash` |
| 0x03 | OK           | B→A  | `u16 screen_w, u16 screen_h` |
| 0x04 | DENY         | B→A  | none |
| 0x10 | FRAME        | B→A  | `u16 x, u16 y, u16 w, u16 h, u8 codec, bytes data` |
| 0x20 | MOUSE_MOVE   | A→B  | `u16 x, u16 y` (B screen pixels) |
| 0x21 | MOUSE_BUTTON | A→B  | `u8 button (1=L,2=R,3=M), u8 down (0/1)` |
| 0x22 | MOUSE_WHEEL  | A→B  | `i16 delta` (Windows units, +120 = one notch up) |
| 0x23 | KEY          | A→B  | `u16 vk (Windows virtual-key), u8 down (0/1)` |
| 0x24 | RELEASE      | A→B  | none |
| 0x30 | PING         | A→B  | none |
| 0x31 | PONG         | B→A  | none |

FRAME codec: `1` = JPEG. v1 always sends full-screen rect `(0,0,screen_w,screen_h)`. Receivers must honor `x,y,w,h` so dirty rects later need no change.

Keepalive: A sends PING every 2 s; B replies PONG. Either side reading nothing for 6 s → close.

Unknown type → close socket.

## Host agent: `raphost.exe` (computer B)

CLI: `raphost -port 7700 -password <pw>`. Console app, runs in the logged-in user session. Embedded manifest declares per-monitor DPI awareness so capture size equals physical pixels.

Capture loop:
- Tick every 100 ms. `kbinani/screenshot` primary monitor → `image/jpeg` quality 70 → FRAME.
- Write with a deadline; if the previous FRAME is still being written, skip this tick. Never queue frames.

Input:
- Decode message → Win32 `SendInput` via `golang.org/x/sys/windows` + `syscall.NewLazyDLL("user32.dll")`.
- MOUSE_MOVE: `MOUSEEVENTF_MOVE|MOUSEEVENTF_ABSOLUTE`, coords scaled to 0–65535 over the primary monitor size.
- MOUSE_BUTTON / MOUSE_WHEEL: matching `MOUSEEVENTF_*` flags.
- KEY: `KEYEVENTF_SCANCODE` with scan code from `MapVirtualKeyW(vk, MAPVK_VK_TO_VSC)`; add `KEYEVENTF_EXTENDEDKEY` for extended keys (arrows, Ins/Del/Home/End/PgUp/PgDn, right Ctrl/Alt, numpad Enter, divide). `KEYEVENTF_KEYUP` when down=0.
- Held-state tracking: `map[uint16]bool` for keys, `[4]bool` for buttons. On RELEASE or when either socket closes, send KEYUP / button-up for every held entry, then clear.

After a session ends B keeps listening.

## Viewer: `rapview.exe` (computer A)

CLI: `rapview -host <ip> -port 7700 -password <pw>`.

- Connects, handshakes.
- Ebiten window, resizable. Draws the latest FRAME scaled to fit, aspect preserved, letterboxed. One `*ebiten.Image` replaced per frame; FRAME `x,y,w,h` is drawn into a persistent full-size backing image.
- **Captured** (default after connect): every key down/up and every mouse event inside the window is forwarded to B. Window title: `rapview — CAPTURED (Ctrl+Alt+Home to release)`.
- **Release hotkey: Ctrl+Alt+Home.** Toggles captured ↔ released. On release: send RELEASE, stop forwarding, title `rapview — RELEASED (focus window, Ctrl+Alt+Home to capture)`. The hotkey chord is never forwarded; the Ctrl/Alt presses that formed it are sent as "up" to B on release.
- Mouse: window cursor → letterboxed image coords → B pixels. Send MOUSE_MOVE only when the pixel changes, max 125 per second.
- Keys: Ebiten key → Windows VK via a fixed table in `internal/input/vk.go`. Unmapped keys are dropped.

## Errors / disconnect

- Either socket closes or errors → close the other. B releases held input. A shows "Disconnected" in the title for 2 s then exits code 1.
- Malformed or oversize message → close.
- Handshake failure on A → print reason, exit code 2.

## Repo layout

```
D:\rap\
  go.mod
  cmd/raphost/main.go
  cmd/rapview/main.go
  internal/protocol/   # message types, encode/decode, handshake  (pure, testable)
  internal/capture/    # screenshot → JPEG
  internal/input/      # SendInput, VK table, held-state tracking
  docs/superpowers/specs/
```

Dependencies: `github.com/kbinani/screenshot`, `github.com/hajimehoshi/ebiten/v2`, `golang.org/x/sys`.

## Testing

- `internal/protocol`: round-trip encode/decode for every message; reject bad len (0, >16 MiB), bad version, unknown type.
- Handshake over `net.Pipe`: correct password → OK; wrong → DENY; 3 wrong from one IP → further connects closed within the 60 s window.
- `internal/input`: `SendInput` behind a func var; press 3 keys + 1 button, call Release → exactly 4 "up" events, held maps empty.
- Manual LAN acceptance: two PCs; type in Notepad on B from A; Ctrl+Alt+Home; typing now lands on A; Ctrl+Alt+Home again resumes B; kill `rapview` mid-keypress → no stuck key on B.

## Later (not in v1, wire already allows)

- Dirty-rect FRAMEs; additional codecs.
- Internet: same messages over QUIC streams or through a relay.
- Windows-account auth replacing shared password.
