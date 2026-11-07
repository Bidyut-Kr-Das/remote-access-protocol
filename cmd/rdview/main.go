// Command rdview is the viewer: it shows a LAN host's screen and forwards input while captured.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"rdp-go/internal/protocol"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("rdview", flag.ContinueOnError)
	host := fs.String("host", "", "host address (required)")
	port := fs.Int("port", 7700, "host TCP port")
	pw := fs.String("password", "", "shared password (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *host == "" || *pw == "" {
		fmt.Fprintln(os.Stderr, "rdview: -host and -password are required")
		fs.Usage()
		return 2
	}
	addr := net.JoinHostPort(*host, strconv.Itoa(*port))
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rdview:", err)
		return 2
	}
	c.SetDeadline(time.Now().Add(10 * time.Second)) // a silent host must not hang startup
	w, h, err := protocol.ClientHandshake(c, *pw)
	if err != nil {
		c.Close()
		fmt.Fprintln(os.Stderr, "rdview:", err)
		return 2
	}
	c.SetDeadline(time.Time{})
	if w == 0 || h == 0 || w > 8192 || h > 8192 {
		c.Close()
		fmt.Fprintln(os.Stderr, "rdview: unsupported screen size", w, "x", h)
		return 2
	}
	g := newGame(c, int(w), int(h))
	defer g.fail() // closes the conn on any exit
	ebiten.SetWindowSize(1280, 720)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetWindowTitle(titleCaptured)
	g.start()
	if err := ebiten.RunGame(g); err != nil && err != ebiten.Termination {
		fmt.Fprintln(os.Stderr, "rdview:", err)
		return 1
	}
	if g.disconnected {
		return 1
	}
	return 0
}
