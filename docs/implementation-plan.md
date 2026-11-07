# Remote Access Protocol (RAP) v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two Windows Go binaries, `raphost` (B) and `rapview` (A), so A sees B's screen and drives B's keyboard/mouse over LAN, with Ctrl+Alt+Home handing input back to A.

**Architecture:** `internal/protocol` owns framing, messages and the challenge-hash handshake, pure Go, fully unit-tested. `internal/input` wraps Win32 `SendInput` behind a func var and tracks held keys. `internal/capture` turns a screenshot into a JPEG FRAME. `cmd/raphost` accepts two TCP channels per session; `cmd/rapview` is an Ebiten window that forwards input while "captured".

**Tech Stack:** Go 1.22+, `github.com/kbinani/screenshot`, `github.com/hajimehoshi/ebiten/v2`, `golang.org/x/sys/windows`, stdlib `encoding/binary`, `crypto/sha256`, `image/jpeg`.

**Spec:** `docs/protocol-design.md`

## Global Constraints

- Go 1.22+. Module path `rap`.
- Dependencies limited to: `github.com/kbinani/screenshot`, `github.com/hajimehoshi/ebiten/v2`, `golang.org/x/sys`.
- Wire: `[u32 BE len][u8 type][payload]`, `len = 1 + len(payload)`, max len 16 MiB (16777216). All integers big-endian.
- Message type bytes exactly as the spec table (0x01–0x04, 0x10, 0x20–0x24, 0x30, 0x31).
- Default port 7700. Lockout: 3 DENY per IP → ignore 60 s. Channel pairing timeout 5 s. PING every 2 s, dead after 6 s without PONG.
- Capture: 100 ms tick, JPEG quality 70, codec byte 1.
- Hotkey Ctrl+Alt+Home. Window titles verbatim from spec.
- Commits: no model attribution lines. Plain `feat:`/`test:`/`docs:` messages.
- Windows-only files carry `//go:build windows`. All tests must run on windows.

## Review Focus

1. **Partial TCP reads.** A FRAME arrives in many segments; decoder must `io.ReadFull`. Test in Task 1 feeds one byte at a time.
2. **Non-ASCII or empty password.** Hash over raw bytes must match both sides; empty password refused at CLI. Tests in Task 2 (unicode) and Task 6 (empty flag exits 2).
3. **Mouse coords beyond B's screen** (A window scaled, rounding). B clamps to `[0,w-1]`,`[0,h-1]`. Test in Task 4.
4. **Viewer window 0×0 or minimized** → divide by zero in coord mapping. `MapCursor` returns ok=false. Test in Task 7.
5. **Lockout counter never reset** → legit user locked after earlier typos. Successful handshake clears the IP's counter. Test in Task 2.

---

### Task 1: Module + protocol framing and messages

**Files:**
- Create: `go.mod`, `internal/protocol/msg.go`, `internal/protocol/frame.go`
- Test: `internal/protocol/frame_test.go`

**Interfaces:**
- Produces:
  ```go
  const (
      TypeChallenge byte = 0x01; TypeHello = 0x02; TypeOK = 0x03; TypeDeny = 0x04
      TypeFrame = 0x10
      TypeMouseMove = 0x20; TypeMouseButton = 0x21; TypeMouseWheel = 0x22; TypeKey = 0x23; TypeRelease = 0x24
      TypePing = 0x30; TypePong = 0x31
  )
  const MaxLen = 16 << 20
  const Version byte = 1
  const ChannelScreen byte = 1; ChannelInput byte = 2
  var ErrOversize = errors.New("protocol: message too large")
  var ErrUnknownType = errors.New("protocol: unknown message type")

  type Msg interface{ Type() byte; Marshal() []byte }   // Marshal = payload only
  type Challenge struct{ Nonce [32]byte }
  type Hello struct{ Version, Channel byte; Hash [32]byte }
  type OK struct{ W, H uint16 }
  type Deny struct{}
  type Frame struct{ X, Y, W, H uint16; Codec byte; Data []byte }
  type MouseMove struct{ X, Y uint16 }
  type MouseButton struct{ Button, Down byte }
  type MouseWheel struct{ Delta int16 }
  type Key struct{ VK uint16; Down byte }
  type Release struct{}
  type Ping struct{}; type Pong struct{}

  func Write(w io.Writer, m Msg) error
  func Read(r io.Reader) (Msg, error)
  // Read: io.ReadFull for header and body; len==0 or len>MaxLen → ErrOversize (body not read);
  // unknown type → ErrUnknownType; short payload for type → io.ErrUnexpectedEOF
  ```

- [ ] **Step 1: `go mod init rap`** (Go 1.22 line in go.mod).

- [ ] **Step 2: Write failing tests** in `frame_test.go`:
  - `TestRoundTripAll`: table with one instance of every message type with non-zero fields (Frame with 3-byte Data, MouseWheel{-120}, Key{VK:0x41,Down:1}); `Write` to `bytes.Buffer`, `Read` back, `reflect.DeepEqual`.
  - `TestReadOneByteAtATime`: wrap buffer in `iotest.OneByteReader`, `Read` a Frame, equal.
  - `TestRejectLenZero`: bytes `00 00 00 00` → `ErrOversize`.
  - `TestRejectOversize`: header len = MaxLen+1 → `ErrOversize`, and only 4 bytes consumed (reader counting bytes).
  - `TestRejectUnknownType`: len=1, type 0x7F → `ErrUnknownType`.
  - `TestHeaderLayout`: `Write(MouseMove{X:1,Y:2})` bytes == `00 00 00 05 20 00 01 00 02`.

- [ ] **Step 3: Run `go test ./internal/protocol/`** → FAIL (undefined).

- [ ] **Step 4: Implement `msg.go` (types, `Type()`, `Marshal()`) and `frame.go` (`Write`, `Read`, per-type unmarshal).** `encoding/binary.BigEndian`; `Read` allocates body of exactly `len`.

- [ ] **Step 5: Run `go test ./internal/protocol/`** → PASS.

- [ ] **Step 6: Commit** `feat(protocol): framing and message codec`.

---

### Task 2: Handshake (server + client) with lockout

**Files:**
- Create: `internal/protocol/handshake.go`
- Test: `internal/protocol/handshake_test.go`

**Interfaces:**
- Consumes: Task 1 `Write`/`Read`, message types.
- Produces:
  ```go
  func HashPassword(password string, nonce [32]byte) [32]byte   // sha256(password||nonce)

  type Limiter struct{ ... }                      // per-IP deny counter; concurrency-safe
  func NewLimiter(max int, window time.Duration, now func() time.Time) *Limiter
  func (l *Limiter) Blocked(ip string) bool
  func (l *Limiter) Fail(ip string)               // increments; at max, block until now+window
  func (l *Limiter) Reset(ip string)

  var ErrDenied = errors.New("protocol: denied")

  // Server side. accept decides whether channel is acceptable right now; returns screen size.
  func ServerHandshake(c io.ReadWriter, ip string, password string, lim *Limiter,
      accept func(channel byte) (w, h uint16, ok bool)) (channel byte, err error)
  // Client side.
  func ClientHandshake(c io.ReadWriter, channel byte, password string) (w, h uint16, err error)
  ```
  ServerHandshake: if `lim.Blocked(ip)` → return ErrDenied without writing. Else Write Challenge (crypto/rand nonce), Read Hello; wrong version → Deny; wrong hash → Deny + `lim.Fail(ip)`; `!accept(channel)` → Deny (no Fail). Success → `lim.Reset(ip)`, Write OK, return channel.

- [ ] **Step 1: Write failing tests** (`net.Pipe()`, server in goroutine):
  - `TestHandshakeOK`: password `"pässwörd✓"`, accept returns (1920,1080,true) → client gets 1920×1080, server returns channel 2.
  - `TestHandshakeWrongPassword`: client uses `"x"` → both sides `ErrDenied`.
  - `TestHandshakeBadVersion`: hand-written Hello with Version 2 → client reads Deny.
  - `TestHandshakeAcceptRejects`: accept false → Deny; limiter not incremented (3 such → still not Blocked).
  - `TestLimiter`: fake clock; 3 `Fail` → `Blocked` true; advance 60 s → false; `Fail`×2, `Reset`, `Fail`×2 → not blocked.
  - `TestHandshakeBlockedIP`: pre-blocked IP → server returns ErrDenied immediately; nothing written.

- [ ] **Step 2: Run tests** → FAIL.

- [ ] **Step 3: Implement `handshake.go`.**

- [ ] **Step 4: Run `go test ./internal/protocol/ -race`** → PASS.

- [ ] **Step 5: Commit** `feat(protocol): challenge-hash handshake with per-IP lockout`.

---

### Task 3: Host session manager (two channels, pairing timeout)

**Files:**
- Create: `internal/host/session.go`
- Test: `internal/host/session_test.go`

**Interfaces:**
- Consumes: Task 2 `ServerHandshake`, `Limiter`.
- Produces:
  ```go
  type Session struct{ Screen, Input net.Conn }
  type Manager struct{ ... }
  func NewManager(password string, w, h uint16, lim *protocol.Limiter, pairTimeout time.Duration) *Manager
  // Accept runs the handshake on c (ip from c.RemoteAddr). Returns (sess, true) exactly once per
  // completed session, from the call that supplied the second channel; (nil,false) otherwise.
  // Denied or timed-out conns are closed by Accept.
  func (m *Manager) Accept(c net.Conn) (*Session, bool)
  func (m *Manager) End()   // clears active session so a new one may start
  ```
  Rules: one session at a time; HELLO while session has both channels → Deny; duplicate channel in pending → Deny; first channel starts pairTimeout timer; timer fires first → close pending conn, clear pending.

- [ ] **Step 1: Write failing tests** (`net.Listen("tcp","127.0.0.1:0")`, clients via `ClientHandshake`):
  - `TestPairTwoChannels`: screen then input → second Accept returns Session with both non-nil.
  - `TestDuplicateChannelDenied`: screen twice → second client gets ErrDenied.
  - `TestThirdConnDeniedWhileActive`: after pair, new input conn → ErrDenied; after `End()` → accepted.
  - `TestPairTimeout`: pairTimeout 50 ms, screen only → that client's Read errors within 200 ms; then a new pair succeeds.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `session.go`.** One mutex, `pending *Session`, `active bool`, `*time.Timer` via `time.AfterFunc`.

- [ ] **Step 4: Run `go test ./internal/host/ -race`** → PASS.

- [ ] **Step 5: Commit** `feat(host): two-channel session manager`.

---

### Task 4: Input injection with held-state tracking

**Files:**
- Create: `internal/input/input.go` (portable), `internal/input/sendinput_windows.go`
- Test: `internal/input/input_test.go`

**Interfaces:**
- Consumes: Task 1 message types.
- Produces:
  ```go
  const (
      MouseMoveF = 0x0001; MouseAbsF = 0x8000; MouseLDown = 0x0002; MouseLUp = 0x0004
      MouseRDown = 0x0008; MouseRUp = 0x0010; MouseMDown = 0x0020; MouseMUp = 0x0040; MouseWheelF = 0x0800
      KeyExtended = 0x0001; KeyUp = 0x0002; KeyScancode = 0x0008
  )
  type Event struct {
      Mouse bool
      X, Y  int32   // absolute 0..65535 for moves
      Flags uint32  // Mouse*F or Key* values above
      Scan  uint16
      Wheel int32
  }
  var Send = func(ev []Event) {}                      // sendinput_windows.go init() replaces
  var ScanOf = func(vk uint16) uint16 { return vk }   // windows: MapVirtualKeyW(vk, 0)

  type Injector struct{ W, H uint16; held map[uint16]bool; buttons [4]bool }
  func NewInjector(w, h uint16) *Injector
  func (i *Injector) Apply(m protocol.Msg)   // MouseMove/MouseButton/MouseWheel/Key/Release; others ignored
  func (i *Injector) Release()               // up for every held key/button, then clear
  func IsExtended(vk uint16) bool
  // true for: 0x21-0x28 (PgUp..Down), 0x2C (Snapshot), 0x2D Ins, 0x2E Del, 0x5B/0x5C Win, 0x5D Apps,
  //           0x6F Divide, 0x90 NumLock, 0xA3 RCtrl, 0xA5 RAlt
  ```
  Scaling: `abs = clamp(x,0,W-1) * 65535 / (W-1)`; same for Y. Key event: `Flags = KeyScancode | (KeyExtended if IsExtended) | (KeyUp if Down==0)`, `Scan = ScanOf(vk)`.

- [ ] **Step 1: Write failing tests** (`Send` → recorder slice, `ScanOf` identity):
  - `TestMouseMoveScalesAndClamps`: W=1920,H=1080; {0,0}→(0,0); {1919,1079}→(65535,65535); {5000,5000}→(65535,65535).
  - `TestKeyFlags`: Key{0x25,1} → Flags == KeyScancode|KeyExtended; Key{0x41,0} → Flags == KeyScancode|KeyUp.
  - `TestReleaseUpsEverything`: Key A down, Key B down, Key A down again, MouseButton{1,1}, Release → recorder gets exactly 3 up events (A, B, L-up); second Release → 0 events.
  - `TestWheel`: MouseWheel{-120} → Event{Mouse:true, Flags:MouseWheelF, Wheel:-120}.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `input.go`** and **`sendinput_windows.go`** (`//go:build windows`): `init()` sets `Send` to build Win32 `INPUT` records (type DWORD + 32-byte union, 40 bytes total on amd64, mouse layout `dx,dy int32; mouseData,dwFlags,time uint32; dwExtraInfo uintptr`; keyboard `wVk,wScan uint16; dwFlags,time uint32; dwExtraInfo uintptr`) and call `user32.SendInput(n, &arr[0], 40)`; `ScanOf` → `MapVirtualKeyW(vk, 0)`.

- [ ] **Step 4: Run `go test ./internal/input/`** → PASS.

- [ ] **Step 5: Commit** `feat(input): SendInput injector with held-key release`.

---

### Task 5: Screen capture to FRAME

**Files:**
- Create: `internal/capture/capture.go`
- Test: `internal/capture/capture_test.go`

**Interfaces:**
- Produces:
  ```go
  func ScreenSize() (w, h uint16, err error)   // screenshot.GetDisplayBounds(0)
  func Grab() (*image.RGBA, error)             // screenshot.CaptureDisplay(0)
  func Encode(img image.Image, quality int) (*protocol.Frame, error)   // full-rect, Codec 1
  ```

- [ ] **Step 1: Write failing test** `TestEncodeProducesDecodableJPEG`: 64×48 solid-red RGBA → Frame{0,0,64,48,Codec:1}; `jpeg.Decode(bytes.NewReader(f.Data))` bounds 64×48.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `capture.go`** (`go get github.com/kbinani/screenshot`).

- [ ] **Step 4: Run `go test ./internal/capture/`** → PASS.

- [ ] **Step 5: Commit** `feat(capture): screenshot to JPEG frame`.

---

### Task 6: `raphost` binary

**Files:**
- Create: `cmd/raphost/main.go`, `cmd/raphost/dpi_windows.go`
- Test: `cmd/raphost/main_test.go`

**Interfaces:**
- Consumes: Tasks 2–5.
- Produces: `func run(args []string) int` (exit code); `main` calls `os.Exit(run(os.Args[1:]))`.

Behavior:
- `dpi_windows.go` `init()`: `user32.SetProcessDpiAwarenessContext(-4)` (PER_MONITOR_AWARE_V2); ignore error.
- Flags `-port` (default 7700), `-password` (empty → usage to stderr, return 2).
- `capture.ScreenSize()` → `host.NewManager(pw, w, h, protocol.NewLimiter(3, 60*time.Second, time.Now), 5*time.Second)`.
- Listen `:port`; per accepted conn goroutine → `mgr.Accept`; on `(sess,true)` → `serve(sess, w, h)` then `mgr.End()`.
- `serve`: `inj := input.NewInjector(w,h)`; `done := make(chan struct{})` closed once by whichever goroutine fails.
  - Capture goroutine: 100 ms ticker → `Grab`→`Encode(70)`→`SetWriteDeadline(now+100ms)`→`protocol.Write(Screen, f)`. Timeout error → skip tick; other error → close done.
  - Input reader: `protocol.Read(Input)` loop → `inj.Apply(m)`; Ping → Write Pong; Pong → store `lastPong`. Error → close done.
  - Keepalive: every 2 s Write Ping; if `time.Since(lastPong) > 6s` → close done.
  - On done: close both conns, `inj.Release()`, log "session ended <ip>".

- [ ] **Step 1: Write failing test** `TestRunRequiresPassword`: `run([]string{"-port","1"})` == 2.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement.** Log one line per session start/end with remote IP.

- [ ] **Step 4: `go test ./cmd/raphost/ && go build ./cmd/raphost`** → PASS, builds.

- [ ] **Step 5: Commit** `feat(raphost): host agent binary`.

---

### Task 7: Viewer key table and cursor mapping

**Files:**
- Create: `internal/view/vk.go`, `internal/view/geom.go`
- Test: `internal/view/view_test.go`

**Interfaces:**
- Produces:
  ```go
  func VK(k ebiten.Key) (vk uint16, ok bool)
  // fixed map: A-Z→0x41.., 0-9→0x30.., F1-F12→0x70.., arrows 0x25-0x28, Enter 0x0D, Esc 0x1B, Tab 0x09,
  // Backspace 0x08, Space 0x20, Ins 0x2D, Del 0x2E, Home 0x24, End 0x23, PgUp 0x21, PgDn 0x22,
  // ShiftL/R 0xA0/0xA1, ControlL/R 0xA2/0xA3, AltL/R 0xA4/0xA5, CapsLock 0x14,
  // OEM: Semicolon 0xBA, Equal 0xBB, Comma 0xBC, Minus 0xBD, Period 0xBE, Slash 0xBF, Backquote 0xC0,
  //      BracketLeft 0xDB, Backslash 0xDC, BracketRight 0xDD, Quote 0xDE; Numpad0-9 0x60.., NumpadEnter 0x0D
  func Fit(iw, ih, ww, wh int) (scale, ox, oy float64)     // letterbox image into window
  func MapCursor(cx, cy, iw, ih, ww, wh int) (x, y uint16, ok bool)   // ok=false: zero window or in bars
  ```

- [ ] **Step 1: Write failing tests**:
  - `TestVKKnown`: KeyA→0x41, KeyArrowLeft→0x25, KeyControlLeft→0xA2, KeyEnter→0x0D; `ebiten.KeyMax+1` → ok=false.
  - `TestFitLetterbox`: 1920×1080 into 960×1080 → scale 0.5, ox 0, oy 270.
  - `TestMapCursorZeroWindow`: ww=0 → ok=false.
  - `TestMapCursorInBars`: 1920×1080 into 960×1080; (10,10) → ok=false; (10,300) → (20,60) ok.
  - `TestMapCursorClampsEdge`: cx=959 → x=1919 (never iw).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** (`go get github.com/hajimehoshi/ebiten/v2`).

- [ ] **Step 4: `go test ./internal/view/`** → PASS.

- [ ] **Step 5: Commit** `feat(view): key table and letterbox mapping`.

---

### Task 8: `rapview` binary

**Files:**
- Create: `cmd/rapview/main.go`, `cmd/rapview/game.go`

**Interfaces:**
- Consumes: Task 1 messages, Task 2 `ClientHandshake`, Task 7.
- Produces: `type game struct` implementing `ebiten.Game`; `func run(args []string) int`.

Behavior:
- Flags `-host` (required), `-port` 7700, `-password` (required). Dial screen, `ClientHandshake(ChannelScreen)`, then input, `ClientHandshake(ChannelInput)`. Failure → print reason to stderr, return 2.
- Frame goroutine: `protocol.Read(screen)` loop; Frame → `jpeg.Decode` → under mutex `draw.Draw` into persistent `*image.RGBA` backing (W×H from OK) at `(X,Y)`; `dirty=true`. Error → `disconnected=true`.
- Input reader goroutine: Ping → Write Pong; Pong → `lastPong`. Keepalive goroutine: Ping every 2 s; `lastPong` older than 6 s → disconnected.
- `Update()`:
  - If disconnected: title `rapview — Disconnected`; after 2 s return `ebiten.Termination`; `run` returns 1.
  - Hotkey: `IsKeyPressed(KeyControl) && IsKeyPressed(KeyAlt) && inpututil.IsKeyJustPressed(KeyHome)` → toggle `captured`. On capture→release: send Key up for 0xA2, 0xA4, then Release. Titles exactly: `rapview — CAPTURED (Ctrl+Alt+Home to release)` / `rapview — RELEASED (focus window, Ctrl+Alt+Home to capture)`.
  - If captured: `inpututil.AppendJustPressedKeys` → Key down; `AppendJustReleasedKeys` → Key up; skip KeyHome while Ctrl+Alt held; skip unmapped. Mouse: `MapCursor(ebiten.CursorPosition(), W, H, ww, wh)`; if ok, changed, and ≥ 8 ms since last → MouseMove. Buttons Left/Right/Middle just-pressed/released → MouseButton{1/2/3, 1/0}. `ebiten.Wheel()` y≠0 → MouseWheel{int16(y*120)}.
  - Any write error → disconnected, close both conns.
- `Draw()`: if dirty, replace `*ebiten.Image` with `ebiten.NewImageFromImage(backing)`; draw with `Fit` scale/offset. `Layout(ow, oh)` returns `(ow, oh)`; `ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)`.

- [ ] **Step 1: Implement `main.go` + `game.go`.** No unit test (Ebiten loop); testable logic lives in Task 7.

- [ ] **Step 2: `go vet ./... && go build ./cmd/rapview`** → builds clean.

- [ ] **Step 3: Commit** `feat(rapview): viewer binary with capture toggle`.

---

### Task 9: LAN acceptance

**Files:**
- Create: `README.md` (build, two run commands, hotkey, known limits = spec non-goals, acceptance results).

- [ ] **Step 1: Build** `go build -o raphost.exe ./cmd/raphost; go build -o rapview.exe ./cmd/rapview`.

- [ ] **Step 2: On B:** `raphost -password test123`. Allow in Windows Firewall when prompted.

- [ ] **Step 3: On A:** `rapview -host <B ip> -password test123`. Expect B's desktop, title CAPTURED.

- [ ] **Step 4: Checks** (record pass/fail in README):
  1. Open Notepad on B via A's mouse; type `hello` → appears on B.
  2. Ctrl+Alt+Home; type → lands on A, not B; title RELEASED.
  3. Ctrl+Alt+Home again; type → lands on B.
  4. Hold `a` on A, kill rapview → B stops repeating `a` within 1 s.
  5. Wrong password ×3 → right password refused for 60 s, then works.
  6. Resize rapview window → aspect kept, clicks land where pointed.

- [ ] **Step 5: Commit** `docs: README and acceptance results`.
