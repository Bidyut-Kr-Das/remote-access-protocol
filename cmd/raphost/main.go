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
	"os/exec"
	"path/filepath"
	"strings"
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
	relogin := fs.Bool("relogin", false, "with -tsnet: forget the saved Tailscale login and sign in again (e.g. another account)")
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
		ln, err = tailnetListen(*port, *relogin)
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

// tailnetListen joins the user's tailnet as "rap-<computer name>" (first run,
// or relogin, opens the login page in the browser) and listens there. Node state persists in
// the user config dir, so later runs connect without logging in again.
// Relogin logs out the saved session but keeps the state dir (device
// identity), so the URL prints on the first run and the same account reuses
// this device's admin-panel entry instead of adding "<name>-1".
func tailnetListen(port int, relogin bool) (net.Listener, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "tsnet-raphost")
	name, _ := os.Hostname()
	hostname := "rap-" + name
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
			return nil, err
		}
	}
	s := &tsnet.Server{Dir: dir, Hostname: hostname, Logf: func(string, ...any) {}, UserLogf: openLogin}
	st, err := s.Up(context.Background())
	if err != nil {
		return nil, err
	}
	log.Printf("on tailnet as %s (%v)", st.Self.DNSName, st.TailscaleIPs)
	return s.Listen("tcp", fmt.Sprintf(":%d", port))
}

// ioTimeout bounds each per-message read and write; a peer silent this long is gone.
const ioTimeout = 15 * time.Second

// serve runs one session on c. Every write goes through one lock: a protocol.Write
// may take several conn writes (tsnet's conn can interleave them), so frame,
// pong and clipboard writers must not overlap.
func serve(c net.Conn, w, h uint16) {
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	log.Printf("session started %s", ip)
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	var wg sync.WaitGroup
	wg.Add(2)
	var wmu sync.Mutex
	write := func(m protocol.Msg) error {
		wmu.Lock()
		defer wmu.Unlock()
		c.SetWriteDeadline(time.Now().Add(ioTimeout)) // fresh per write; any error ends the session
		return protocol.Write(c, m)
	}
	ack := make(chan struct{}, 1)

	go func() { // screen: capture, encode and send one frame, then wait for the viewer's Ack
		defer wg.Done()
		var last time.Time
		needAck := false
		for {
			if needAck {
				select {
				case <-done:
					return
				case <-ack:
				}
			}
			if d := time.Until(last.Add(100 * time.Millisecond)); d > 0 { // 10 fps cap
				select {
				case <-done:
					return
				case <-time.After(d):
				}
			}
			last = time.Now()
			img, err := capture.Grab()
			if err != nil {
				needAck = false
				continue // e.g. secure desktop; retry after the interval
			}
			f, err := capture.Encode(img, 70)
			if err != nil {
				needAck = false
				continue
			}
			if write(f) != nil {
				stop()
				return
			}
			needAck = true
		}
	}()

	clip := clipboard.New()
	go func() { // clipboard
		defer wg.Done()
		clip.Run(done, func(text string) {
			write(protocol.Clipboard{Text: text})
		})
	}()

	inj := input.NewInjector(w, h) // not goroutine-safe: used only here
	var cause error
	for {
		c.SetReadDeadline(time.Now().Add(ioTimeout)) // viewer pings every 2 s
		m, err := protocol.Read(c)
		if err != nil {
			cause = err
			break
		}
		switch m := m.(type) {
		case protocol.Ping:
			write(protocol.Pong{})
		case protocol.Ack:
			select {
			case ack <- struct{}{}:
			default:
			}
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
	log.Printf("session ended %s: %v", ip, cause)
}
