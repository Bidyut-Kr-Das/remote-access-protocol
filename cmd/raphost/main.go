// Command raphost is the host agent: it shares this screen and accepts input from one viewer
// over the LAN or, with -tsnet, the user's tailnet.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"rap/internal/capture"
	"rap/internal/clipboard"
	"rap/internal/host"
	"rap/internal/input"
	"rap/internal/protocol"

	"tailscale.com/tsnet"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("raphost", flag.ContinueOnError)
	port := fs.Int("port", 7700, "TCP port to listen on")
	pw := fs.String("password", "", "shared password (required)")
	ts := fs.Bool("tsnet", false, "listen on your tailnet via embedded Tailscale instead of the LAN")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *pw == "" {
		fmt.Fprintln(os.Stderr, "raphost: -password is required")
		fs.Usage()
		return 2
	}
	w, h, err := capture.ScreenSize()
	if err != nil {
		log.Print(err)
		return 1
	}
	mgr := host.NewManager(*pw, w, h, protocol.NewLimiter(3, 60*time.Second, time.Now), 5*time.Second)
	var ln net.Listener
	if *ts {
		ln, err = tailnetListen(*port)
	} else {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", *port))
	}
	if err != nil {
		log.Print(err)
		return 1
	}
	log.Printf("listening on %s (%dx%d)", ln.Addr(), w, h)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Print(err)
			return 1
		}
		go func() {
			if mgr.Accept(c) {
				serve(c, w, h)
				mgr.End()
			}
		}()
	}
}

// tailnetListen joins the user's tailnet as "rap-<computer name>" (first run
// prints a login URL) and listens there. Node state persists in the user
// config dir, so later runs connect without logging in again.
func tailnetListen(port int) (net.Listener, error) {
	name, _ := os.Hostname()
	s := &tsnet.Server{Hostname: "rap-" + name, Logf: func(string, ...any) {}}
	st, err := s.Up(context.Background())
	if err != nil {
		return nil, err
	}
	log.Printf("on tailnet as %s (%v)", st.Self.DNSName, st.TailscaleIPs)
	return s.Listen("tcp", fmt.Sprintf(":%d", port))
}

// serve runs one session on c. Each protocol.Write is one conn write, so the
// frame, pong and clipboard writers below need no lock between them.
func serve(c net.Conn, w, h uint16) {
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	log.Printf("session started %s", ip)
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	var wg sync.WaitGroup
	wg.Add(3)
	frames := make(chan protocol.Frame, 1)

	go func() { // screen sender
		defer wg.Done()
		for f := range frames {
			// Generous deadline: a client stalled this long is gone; any error ends the session.
			c.SetWriteDeadline(time.Now().Add(6 * time.Second))
			if protocol.Write(c, f) != nil {
				stop()
				return
			}
		}
	}()

	go func() { // capture
		defer wg.Done()
		defer close(frames)
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
			}
			img, err := capture.Grab()
			if err != nil {
				continue // e.g. secure desktop; try next tick
			}
			f, err := capture.Encode(img, 70)
			if err != nil {
				continue
			}
			select {
			case frames <- *f:
			default: // sender still busy with an earlier frame: skip this tick, never queue
			}
		}
	}()

	clip := clipboard.New()
	go func() { // clipboard
		defer wg.Done()
		clip.Run(done, func(text string) {
			c.SetWriteDeadline(time.Now().Add(6 * time.Second))
			protocol.Write(c, protocol.Clipboard{Text: text})
		})
	}()

	inj := input.NewInjector(w, h) // not goroutine-safe: used only here
	for {
		c.SetReadDeadline(time.Now().Add(6 * time.Second)) // viewer pings every 2 s
		m, err := protocol.Read(c)
		if err != nil {
			break
		}
		switch m := m.(type) {
		case protocol.Ping:
			protocol.Write(c, protocol.Pong{})
		case protocol.Clipboard:
			clip.Apply(m.Text)
		default:
			inj.Apply(m)
		}
	}
	inj.Release()
	stop()
	c.Close()
	wg.Wait()
	log.Printf("session ended %s", ip)
}
