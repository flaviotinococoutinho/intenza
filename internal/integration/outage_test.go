//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flaviotinococoutinho/intenza/internal/adapter/mongo"
	"github.com/flaviotinococoutinho/intenza/internal/app"
)

// blackhole forwards TCP until paused; then it holds every byte without
// closing anything. To the client that is a paused container: no error, no
// answer, only its own timeouts.
type blackhole struct {
	ln     net.Listener
	target string
	paused atomic.Bool
}

func newBlackhole(t *testing.T, target string) *blackhole {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &blackhole{ln: ln, target: target}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go b.pipe(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return b
}

func (b *blackhole) pipe(c net.Conn) {
	u, err := net.Dial("tcp", b.target)
	if err != nil {
		_ = c.Close()
		return
	}
	go b.copy(u, c)
	b.copy(c, u)
}

func (b *blackhole) copy(dst io.WriteCloser, src io.ReadCloser) {
	defer func() { _ = dst.Close(); _ = src.Close() }()
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		for n > 0 && b.paused.Load() {
			time.Sleep(10 * time.Millisecond)
		}
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// With the database hung, a write fails within about one operation timeout.
// The driver's own abort of the failed transaction runs with a fresh timeout
// of its own; left alone, it doubled the wait.
func TestWriteFailsWithinTheTimeoutWhenTheDatabaseHangs(t *testing.T) {
	ctx := context.Background()
	u, err := url.Parse(mongoURI)
	if err != nil {
		t.Fatal(err)
	}
	hole := newBlackhole(t, u.Host)
	u.Host = hole.ln.Addr().String()

	const timeout = time.Second
	store, err := mongo.Connect(ctx, u.String(), fmt.Sprintf("it_hang_%d", time.Now().UnixNano()), timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Database().Drop(ctx); _ = store.Close(ctx) })
	read := func(ctx context.Context, tx app.Tx) error {
		if _, err := tx.Intent(ctx, "nada"); !errors.Is(err, app.ErrNotFound) {
			return err
		}
		return nil
	}
	if err := store.Tx(ctx, read); err != nil {
		t.Fatalf("warm-up: %v", err)
	}

	hole.paused.Store(true)
	t.Cleanup(func() { hole.paused.Store(false) }) // runs before the store closes
	start := time.Now()
	err = store.Tx(ctx, read)
	took := time.Since(start)
	if err == nil {
		t.Fatal("a hung database cannot have answered")
	}
	if took >= timeout*8/5 {
		t.Fatalf("failed after %v with a %v timeout: the abort is adding its own wait", took, timeout)
	}
	t.Logf("failed after %v: %v", took.Round(time.Millisecond), err)
}
