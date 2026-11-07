package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"rap/internal/clipboard"
	"rap/internal/protocol"
	"rap/internal/view"
)

const (
	titleCaptured     = "rapview — CAPTURED (Ctrl+Alt+Home to release)"
	titleReleased     = "rapview — RELEASED (focus window, Ctrl+Alt+Home to capture)"
	titleDisconnected = "rapview — Disconnected"
)

type game struct {
	conn net.Conn
	w, h int

	mu      sync.Mutex // guards backing, dirty
	backing *image.RGBA
	dirty   bool
	img     *ebiten.Image

	dead      atomic.Bool
	closeOnce sync.Once
	clipDone  chan struct{}
	clip      *clipboard.Syncer
	wmu       sync.Mutex // serializes conn writes; held while checking dead

	// Update/Layout-only state (game loop goroutine).
	captured     bool
	skipHomeUp   bool
	ww, wh       int
	mx, my       uint16
	lastMove     time.Time
	deadAt       time.Time
	disconnected bool
}

func newGame(conn net.Conn, w, h int) *game {
	return &game{conn: conn, w: w, h: h, captured: true, backing: image.NewRGBA(image.Rect(0, 0, w, h)),
		img: ebiten.NewImage(w, h), clipDone: make(chan struct{}), clip: clipboard.New()}
}

// fail marks the session dead and closes the conn (idempotent).
func (g *game) fail() {
	g.dead.Store(true)
	g.closeOnce.Do(func() {
		close(g.clipDone)
		g.conn.Close()
	})
}

// send writes one message; never writes after disconnect.
func (g *game) send(m protocol.Msg) {
	g.wmu.Lock()
	defer g.wmu.Unlock()
	if g.dead.Load() {
		return
	}
	g.conn.SetWriteDeadline(time.Now().Add(6 * time.Second))
	if protocol.Write(g.conn, m) != nil {
		g.fail()
	}
}

func (g *game) start() {
	go func() { // reader
		for {
			g.conn.SetReadDeadline(time.Now().Add(6 * time.Second)) // host pongs our 2 s pings
			m, err := protocol.Read(g.conn)
			if err != nil {
				g.fail()
				return
			}
			switch m := m.(type) {
			case protocol.Frame:
				img, err := jpeg.Decode(bytes.NewReader(m.Data))
				if err != nil {
					continue // one bad tile is not fatal
				}
				x, y := int(m.X), int(m.Y)
				g.mu.Lock()
				draw.Draw(g.backing, image.Rect(x, y, x+int(m.W), y+int(m.H)), img, image.Point{}, draw.Src)
				g.dirty = true
				g.mu.Unlock()
			case protocol.Clipboard:
				g.clip.Apply(m.Text)
			}
		}
	}()
	go g.clip.Run(g.clipDone, func(text string) { g.send(protocol.Clipboard{Text: text}) })
	go func() { // keepalive; the reader's deadline detects a dead host
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			if g.dead.Load() {
				return
			}
			g.send(protocol.Ping{})
		}
	}()
}

func (g *game) Update() error {
	if g.dead.Load() {
		if g.deadAt.IsZero() {
			g.deadAt = time.Now()
			g.disconnected = true
			ebiten.SetWindowTitle(titleDisconnected)
		}
		if time.Since(g.deadAt) >= 2*time.Second {
			return ebiten.Termination
		}
		return nil
	}
	chord := ebiten.IsKeyPressed(ebiten.KeyControl) && ebiten.IsKeyPressed(ebiten.KeyAlt)
	if chord && inpututil.IsKeyJustPressed(ebiten.KeyHome) {
		g.skipHomeUp = true
		g.captured = !g.captured
		if g.captured {
			ebiten.SetWindowTitle(titleCaptured)
		} else {
			g.send(protocol.Release{}) // ups every held key, Ctrl/Alt included
			ebiten.SetWindowTitle(titleReleased)
		}
	}
	if !g.captured {
		if inpututil.IsKeyJustReleased(ebiten.KeyHome) {
			g.skipHomeUp = false
		}
		return nil
	}
	for _, k := range inpututil.AppendJustPressedKeys(nil) {
		if k == ebiten.KeyHome && chord {
			continue
		}
		if vk, ok := view.VK(k); ok {
			g.send(protocol.Key{VK: vk, Down: 1})
		}
	}
	// SendInput key-downs don't auto-repeat: resend held non-modifier keys after 0.5s, 30/s at 60 TPS.
	for _, k := range inpututil.AppendPressedKeys(nil) {
		if k == ebiten.KeyHome && (chord || g.skipHomeUp) {
			continue
		}
		vk, ok := view.VK(k)
		if d := inpututil.KeyPressDuration(k); !ok || (vk >= 0xA0 && vk <= 0xA5) || vk == 0x14 || d < 30 || (d-30)%2 != 0 {
			continue
		}
		g.send(protocol.Key{VK: vk, Down: 1})
	}
	for _, k := range inpututil.AppendJustReleasedKeys(nil) {
		if k == ebiten.KeyHome && (chord || g.skipHomeUp) {
			g.skipHomeUp = false
			continue
		}
		if vk, ok := view.VK(k); ok {
			g.send(protocol.Key{VK: vk, Down: 0})
		}
	}
	cx, cy := ebiten.CursorPosition()
	if x, y, ok := view.MapCursor(cx, cy, g.w, g.h, g.ww, g.wh); ok && (x != g.mx || y != g.my) && time.Since(g.lastMove) >= 8*time.Millisecond {
		g.mx, g.my, g.lastMove = x, y, time.Now()
		g.send(protocol.MouseMove{X: x, Y: y})
	}
	for _, b := range [...]struct {
		eb ebiten.MouseButton
		id byte
	}{{ebiten.MouseButtonLeft, 1}, {ebiten.MouseButtonRight, 2}, {ebiten.MouseButtonMiddle, 3}} {
		if inpututil.IsMouseButtonJustPressed(b.eb) {
			g.send(protocol.MouseButton{Button: b.id, Down: 1})
		}
		if inpututil.IsMouseButtonJustReleased(b.eb) {
			g.send(protocol.MouseButton{Button: b.id, Down: 0})
		}
	}
	if _, y := ebiten.Wheel(); y != 0 {
		g.send(protocol.MouseWheel{Delta: int16(y * 120)})
	}
	return nil
}

func (g *game) Draw(dst *ebiten.Image) {
	g.mu.Lock()
	if g.dirty {
		g.img.WritePixels(g.backing.Pix)
		g.dirty = false
	}
	g.mu.Unlock()
	s, ox, oy := view.Fit(g.w, g.h, g.ww, g.wh)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(s, s)
	op.GeoM.Translate(ox, oy)
	op.Filter = ebiten.FilterLinear
	dst.DrawImage(g.img, op)
}

func (g *game) Layout(ow, oh int) (int, int) {
	g.ww, g.wh = ow, oh
	return ow, oh
}
