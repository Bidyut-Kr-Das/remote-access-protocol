// Command rdhost is the host agent: it shares this screen and accepts input from one LAN client.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"rdp-go/internal/capture"
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
	wg.Add(2)

	go func() { // capture
		defer wg.Done()
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
			sess.Screen.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
			if err := protocol.Write(sess.Screen, *f); err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue // slow client: drop this frame
				}
				stop()
				return
			}
		}
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
			switch m.(type) {
			case protocol.Ping:
				protocol.Write(sess.Input, protocol.Pong{})
			case protocol.Pong:
				lastPong.Store(time.Now().UnixNano())
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
