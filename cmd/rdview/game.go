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

	"rdp-go/internal/protocol"
	"rdp-go/internal/view"
)

const (
	titleCaptured     = "rdview — CAPTURED (Ctrl+Alt+Home to release)"
	titleReleased     = "rdview — RELEASED (focus window, Ctrl+Alt+Home to capture)"
	titleDisconnected = "rdview — Disconnected"
)

type game struct {
	screen, input net.Conn
	w, h          int

	mu      sync.Mutex // guards backing, dirty
	backing *image.RGBA
	dirty   bool
	img     *ebiten.Image

	dead      atomic.Bool
	closeOnce sync.Once
	wmu       sync.Mutex // serializes input-conn writes; held while checking dead
	lastPong  atomic.Int64

	// Update/Layout-only state (game loop goroutine).
	captured     bool
	skipHomeUp   bool
	ww, wh       int
	mx, my       uint16
	lastMove     time.Time
	deadAt       time.Time
	disconnected bool
}

func newGame(screen, input net.Conn, w, h int) *game {
	g := &game{screen: screen, input: input, w: w, h: h, backing: image.NewRGBA(image.Rect(0, 0, w, h))}
	g.lastPong.Store(time.Now().UnixNano())
	return g
}

// fail marks the session dead and closes both conns (idempotent).
func (g *game) fail() {
	g.dead.Store(true)
	g.closeOnce.Do(func() {
		g.screen.Close()
		g.input.Close()
	})
}

// send writes one message on the input conn; never writes after disconnect.
func (g *game) send(m protocol.Msg) {
	g.wmu.Lock()
	defer g.wmu.Unlock()
	if g.dead.Load() {
		return
	}
	g.input.SetWriteDeadline(time.Now().Add(6 * time.Second))
	if protocol.Write(g.input, m) != nil {
		g.fail()
	}
}

func (g *game) start() {
	go func() { // frames
		for {
			m, err := protocol.Read(g.screen)
			if err != nil {
				g.fail()
				return
			}
			f, ok := m.(protocol.Frame)
			if !ok {
				continue
			}
			img, err := jpeg.Decode(bytes.NewReader(f.Data))
			if err != nil {
				continue // one bad tile is not fatal
			}
			x, y := int(f.X), int(f.Y)
			g.mu.Lock()
			draw.Draw(g.backing, image.Rect(x, y, x+int(f.W), y+int(f.H)), img, image.Point{}, draw.Src)
			g.dirty = true
			g.mu.Unlock()
		}
	}()
	go func() { // input reader
		for {
			m, err := protocol.Read(g.input)
			if err != nil {
				g.fail()
				return
			}
			switch m.(type) {
			case protocol.Ping:
				g.send(protocol.Pong{})
			case protocol.Pong:
				g.lastPong.Store(time.Now().UnixNano())
			}
		}
	}()
	go func() { // keepalive
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			if g.dead.Load() {
				return
			}
			if time.Since(time.Unix(0, g.lastPong.Load())) > 6*time.Second {
				g.fail()
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
			ebiten.SetWindowTitle(titleDisconnected)
		}
		if time.Since(g.deadAt) >= 2*time.Second {
			g.disconnected = true
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
			g.send(protocol.Key{VK: 0xA2, Down: 0}) // LCtrl up
			g.send(protocol.Key{VK: 0xA4, Down: 0}) // LAlt up
			g.send(protocol.Release{})
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
		if g.img != nil {
			g.img.Deallocate()
		}
		g.img = ebiten.NewImageFromImage(g.backing)
		g.dirty = false
	}
	g.mu.Unlock()
	if g.img == nil {
		return
	}
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
