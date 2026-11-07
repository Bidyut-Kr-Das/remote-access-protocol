# rdp-go

LAN remote control for Windows over a custom wire protocol (v1). `rdhost` runs on computer B (the one being controlled); `rdview` runs on computer A (the one you sit at). Wire format: `docs/superpowers/specs/2026-11-06-lan-remote-protocol-design.md`.

## Requirements

- Windows on both machines, same LAN.
- Go 1.26+ to build.

## Build

```
go build -o rdhost.exe ./cmd/rdhost
go build -o rdview.exe ./cmd/rdview
```

## Run

On B:

```
.\rdhost.exe -password <pw>
```

Default port 7700 (`-port` to change). Allow it in Windows Firewall when prompted.

On A:

```
.\rdview.exe -host <B ip> -password <pw>
```

`-port` defaults to 7700.

## Hotkey

Ctrl+Alt+Home toggles capture mode (shown in the window title); the chord itself is never sent to B:

- CAPTURED: keyboard and mouse on A go to B.
- RELEASED: input stays on A; B is view-only.

## Clipboard

Text copied on either PC appears on the other within about half a second, in both capture modes. Text only (no images or files), up to 256 KB. It crosses the LAN in plaintext like everything else.

## Known limits (v1 non-goals)

- Internet / NAT traversal / relay.
- TLS. Password is challenge-hashed; frame data is plaintext on LAN.
- Login screen, UAC prompts, session 0 capture.
- Clipboard images/files, audio, file transfer, multi-monitor.
- Auto-reconnect.
- Forwarding Win key, Alt+Tab, Ctrl+Alt+Del from A (OS keeps them).
- Input into windows running as administrator is blocked by Windows (UIPI) unless rdhost itself runs elevated.

## Acceptance checklist (two PCs, not yet run)

Start `.\rdhost.exe -password test123` on B, `.\rdview.exe -host <B ip> -password test123` on A. Expect B's desktop, title CAPTURED. Record pass/fail:

- [ ] 1. Open Notepad on B via A's mouse; type `hello` → appears on B.
- [ ] 2. Ctrl+Alt+Home; type → lands on A, not B; title RELEASED.
- [ ] 3. Ctrl+Alt+Home again; type → lands on B.
- [ ] 4. Hold `a` on A, kill rdview → B stops repeating `a` within 1 s.
- [ ] 5. Wrong password ×3 → right password refused for 60 s, then works.
- [ ] 6. Resize rdview window → aspect kept, clicks land where pointed.
- [ ] 7. Copy text on A, paste on B; copy on B, paste on A → both arrive; no ping-pong re-copies.
