// Command rapview is the viewer: it shows a host's screen (LAN or, with -tsnet, tailnet)
// and forwards input while captured.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"rap/internal/protocol"

	"tailscale.com/tsnet"
)

//go:embed icon/*.png
var iconFS embed.FS

func main() { os.Exit(run(os.Args[1:])) }

// windowIcon loads the embedded 16/32/48 px icons. Ebiten ignores the exe's
// icon resource (rsrc_windows_amd64.syso), so the window needs them set.
func windowIcon() []image.Image {
	var imgs []image.Image
	for _, n := range []string{"icon/icon16.png", "icon/icon32.png", "icon/icon48.png"} {
		f, err := iconFS.Open(n)
		if err != nil {
			continue
		}
		if img, _, err := image.Decode(f); err == nil {
			imgs = append(imgs, img)
		}
		f.Close()
	}
	return imgs
}

func run(args []string) int {
	fs := flag.NewFlagSet("rapview", flag.ContinueOnError)
	host := fs.String("host", "", "host address (required)")
	port := fs.Int("port", 7700, "host TCP port")
	pw := fs.String("password", "", "shared password (required)")
	ts := fs.Bool("tsnet", false, "dial over your tailnet via embedded Tailscale instead of the LAN")
	relogin := fs.Bool("relogin", false, "with -tsnet: forget the saved Tailscale login and sign in again (e.g. another account)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *host == "" || *pw == "" {
		fmt.Fprintln(os.Stderr, "rapview: -host and -password are required")
		fs.Usage()
		return 2
	}
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	var c net.Conn
	var err error
	if *ts {
		var s *tsnet.Server
		c, s, err = tailnetDial(addr, *relogin)
		if err == nil {
			logPath(s, c)
			go func() {
				time.Sleep(10 * time.Second) // usually starts on DERP, then upgrades to direct
				logPath(s, c)
			}()
		}
	} else {
		c, err = net.DialTimeout("tcp", addr, 5*time.Second)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rapview:", err)
		return 2
	}
	c.SetDeadline(time.Now().Add(10 * time.Second)) // a silent host must not hang startup
	w, h, err := protocol.ClientHandshake(c, *pw)
	if err != nil {
		c.Close()
		fmt.Fprintln(os.Stderr, "rapview:", err)
		return 2
	}
	c.SetDeadline(time.Time{})
	if w == 0 || h == 0 || w > 8192 || h > 8192 {
		c.Close()
		fmt.Fprintln(os.Stderr, "rapview: unsupported screen size", w, "x", h)
		return 2
	}
	g := newGame(c, int(w), int(h))
	defer g.fail() // closes the conn on any exit
	ebiten.SetWindowSize(1280, 720)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle(titleCaptured)
	ebiten.SetWindowIcon(windowIcon())
	g.start()
	if err := ebiten.RunGame(g); err != nil && err != ebiten.Termination {
		fmt.Fprintln(os.Stderr, "rapview:", err)
		return 1
	}
	if g.disconnected {
		return 1
	}
	return 0
}

var (
	loginMu  sync.Mutex
	lastOpen string
)

// openLogin is tsnet's UserLogf: it logs the message and opens a login URL in
// the browser once (tsnet repeats the URL every few seconds).
func openLogin(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	for _, f := range strings.Fields(msg) {
		if !strings.HasPrefix(f, "https://") {
			continue
		}
		loginMu.Lock()
		defer loginMu.Unlock()
		if f == lastOpen {
			return
		}
		lastOpen = f
		log.Print(msg)
		if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", f).Start(); err != nil {
			log.Printf("could not open browser: %v", err)
		}
		return
	}
	log.Print(msg)
}

// tailnetDial joins the user's tailnet as "rapview-<computer name>" (first run, or relogin,
// opens the login page in the browser; state persists in the user config dir) and dials addr
// over it. The server is never closed: it lives as long as the connection,
// i.e. the process. Relogin logs out the saved session but keeps the state dir
// (device identity), so the URL prints on the first run and the same account
// reuses this device's admin-panel entry.
func tailnetDial(addr string, relogin bool) (net.Conn, *tsnet.Server, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, err
	}
	dir = filepath.Join(dir, "tsnet-rapview")
	name, _ := os.Hostname()
	hostname := "rapview-" + name
	if relogin {
		old := &tsnet.Server{Dir: dir, Hostname: hostname, Logf: func(string, ...any) {}}
		lc, err := old.LocalClient()
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err = lc.Logout(ctx)
			cancel()
		}
		old.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	s := &tsnet.Server{Dir: dir, Hostname: hostname, Logf: func(string, ...any) {}, UserLogf: openLogin}
	if _, err := s.Up(context.Background()); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, err := s.Dial(ctx, "tcp", addr)
	return c, s, err
}

// logPath logs whether the tailnet path to c's peer is direct or relayed.
func logPath(s *tsnet.Server, c net.Conn) {
	lc, err := s.LocalClient()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := lc.Status(ctx)
	if err != nil {
		return
	}
	ap, err := netip.ParseAddrPort(c.RemoteAddr().String())
	if err != nil {
		return
	}
	for _, p := range st.Peer {
		for _, ip := range p.TailscaleIPs {
			if ip != ap.Addr() {
				continue
			}
			if p.CurAddr != "" {
				log.Printf("path: direct %s", p.CurAddr)
			} else {
				log.Printf("path: relay %s", p.Relay)
			}
			return
		}
	}
}
