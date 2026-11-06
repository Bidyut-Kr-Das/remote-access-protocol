# rdp-go

LAN remote control for Windows over a custom wire protocol (v1). `rdhost` runs on computer B (the one being controlled); `rdview` runs on computer A (the one you sit at). Wire format: `docs/superpowers/specs/2026-11-06-lan-remote-protocol-design.md`.

## Requirements

- Windows on both machines, same LAN.
- Go 1.22+ to build.

## Build

```
go build -o rdhost.exe ./cmd/rdhost
go build -o rdview.exe ./cmd/rdview
```

## Run

On B:

```
rdhost -password <pw>
```

Default port 7700 (`-port` to change). Allow it in Windows Firewall when prompted.

On A:

```
rdview -host <B ip> -password <pw>
```

## Hotkey

Ctrl+Alt+Home toggles the window title between:

- CAPTURED: keyboard and mouse on A go to B.
- RELEASED: input stays on A; B is view-only.

## Known limits (v1 non-goals)

- Internet / NAT traversal / relay.
- TLS. Password is challenge-hashed; frame data is plaintext on LAN.
- Login screen, UAC prompts, session 0 capture.
- Clipboard, audio, file transfer, multi-monitor.
- Auto-reconnect.
- Forwarding Win key, Alt+Tab, Ctrl+Alt+Del from A (OS keeps them).

## Acceptance checklist (two PCs, not yet run)

Start `rdhost -password test123` on B, `rdview -host <B ip> -password test123` on A. Expect B's desktop, title CAPTURED. Record pass/fail:

- [ ] 1. Open Notepad on B via A's mouse; type `hello` -> appears on B.
- [ ] 2. Ctrl+Alt+Home; type -> lands on A, not B; title RELEASED.
- [ ] 3. Ctrl+Alt+Home again; type -> lands on B.
- [ ] 4. Hold `a` on A, kill rdview -> B stops repeating `a` within 1 s.
- [ ] 5. Wrong password x3 -> right password refused for 60 s, then works.
- [ ] 6. Resize rdview window -> aspect kept, clicks land where pointed.
