// Command rapview is the viewer: it shows a host's screen (LAN or, with -tsnet, tailnet)
// and forwards input while captured.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"rap/internal/protocol"

	"tailscale.com/tsnet"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("rapview", flag.ContinueOnError)
	host := fs.String("host", "", "host address (required)")
	port := fs.Int("port", 7700, "host TCP port")
	pw := fs.String("password", "", "shared password (required)")
	ts := fs.Bool("tsnet", false, "dial over your tailnet via embedded Tailscale instead of the LAN")
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
		c, err = tailnetDial(addr)
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

// tailnetDial joins the user's tailnet as "rapview" (first run prints a login
// URL; state persists in the user config dir) and dials addr over it. The
// server is never closed: it lives as long as the connection, i.e. the process.
func tailnetDial(addr string) (net.Conn, error) {
	s := &tsnet.Server{Hostname: "rapview", Logf: func(string, ...any) {}}
	if _, err := s.Up(context.Background()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.Dial(ctx, "tcp", addr)
}
