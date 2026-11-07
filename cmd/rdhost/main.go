// Command rdhost is the host agent: it shares this screen and accepts input from one LAN client.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"rdp-go/internal/capture"
	"rdp-go/internal/clipboard"
	"rdp-go/internal/host"
	"rdp-go/internal/input"
	"rdp-go/internal/protocol"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("rdhost", flag.ContinueOnError)
	port := fs.Int("port", 7700, "TCP port to listen on")
	pw := fs.String("password", "", "shared password (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *pw == "" {
		fmt.Fprintln(os.Stderr, "rdhost: -password is required")
		fs.Usage()
		return 2
	}
	w, h, err := capture.ScreenSize()
	if err != nil {
		log.Print(err)
		return 1
	}
	mgr := host.NewManager(*pw, w, h, protocol.NewLimiter(3, 60*time.Second, time.Now), 5*time.Second)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
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
			if sess, ok := mgr.Accept(c); ok {
				serve(sess, w, h)
				mgr.End()
			}
		}()
	}
}

func serve(sess *host.Session, w, h uint16) {
	ip, _, _ := net.SplitHostPort(sess.Input.RemoteAddr().String())
	log.Printf("session started %s", ip)
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	var lastPong atomic.Int64
	lastPong.Store(time.Now().UnixNano())
	var wg sync.WaitGroup
	wg.Add(4)
	frames := make(chan protocol.Frame, 1)

	go func() { // screen sender: the only writer on Screen, so frames never interleave
		defer wg.Done()
		for f := range frames {
			// Generous deadline: a client stalled this long is gone; any error ends the session.
			sess.Screen.SetWriteDeadline(time.Now().Add(6 * time.Second))
			if protocol.Write(sess.Screen, f) != nil {
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
	go func() { // clipboard: each protocol.Write is one conn write, so it may share Input with ping/pong
		defer wg.Done()
		clip.Run(done, func(text string) {
			sess.Input.SetWriteDeadline(time.Now().Add(6 * time.Second))
			protocol.Write(sess.Input, protocol.Clipboard{Text: text})
		})
	}()

	go func() { // input reader; sole owner of inj (Injector is not goroutine-safe)
		defer wg.Done()
		inj := input.NewInjector(w, h)
		defer inj.Release()
		for {
			m, err := protocol.Read(sess.Input)
			if err != nil {
				stop()
				return
			}
			switch m := m.(type) {
			case protocol.Ping:
				protocol.Write(sess.Input, protocol.Pong{})
			case protocol.Pong:
				lastPong.Store(time.Now().UnixNano())
			case protocol.Clipboard:
				clip.Apply(m.Text)
			default:
				inj.Apply(m)
			}
		}
	}()

	t := time.NewTicker(2 * time.Second) // keepalive
	defer t.Stop()
loop:
	for {
		select {
		case <-done:
			break loop
		case <-t.C:
		}
		if time.Since(time.Unix(0, lastPong.Load())) > 6*time.Second {
			break loop
		}
		// Deadline keeps a stalled peer from blocking keepalive forever.
		sess.Input.SetWriteDeadline(time.Now().Add(6 * time.Second))
		if protocol.Write(sess.Input, protocol.Ping{}) != nil {
			break loop
		}
	}
	stop()
	sess.Screen.Close()
	sess.Input.Close() // unblocks the reader, which then releases held keys
	wg.Wait()
	log.Printf("session ended %s", ip)
}
