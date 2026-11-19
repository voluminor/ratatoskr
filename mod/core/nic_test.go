package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	gvisorsync "gvisor.dev/gvisor/pkg/sync"
)

// // // // // // // // // //

func closeWithin(t *testing.T, node *Obj) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- node.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("close node: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("node shutdown blocked")
	}
}

// closeEchoConn ignores teardown errors from deliberately closed TCP exchanges.
func closeEchoConn(conn net.Conn) {
	_ = conn.Close()
}

func echoRoundTrip(node *Obj, address string, earlyClose bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := node.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial echo: %w", err)
	}
	defer closeEchoConn(conn)
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	payload := bytes.Repeat([]byte("echo"), 320)
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("write echo: %w", err)
	}
	if earlyClose {
		return nil
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		return fmt.Errorf("read echo: %w", err)
	}
	if !bytes.Equal(got, payload) {
		return fmt.Errorf("echo payload mismatch")
	}
	return nil
}

// //

func TestNetstackAtomicCompareAndSwap(t *testing.T) {
	// Netstack park callbacks use an assembly CAS under -race. A broken ARM64
	// implementation prevents parking and can starve TCP exchanges on macOS.
	var state uintptr
	if !gvisorsync.RaceUncheckedAtomicCompareAndSwapUintptr(&state, 0, 42) || state != 42 {
		t.Fatalf("CAS did not replace the expected value: state = %d", state)
	}
	if gvisorsync.RaceUncheckedAtomicCompareAndSwapUintptr(&state, 0, 1) || state != 42 {
		t.Fatalf("CAS changed a value that did not match: state = %d", state)
	}
}

func TestNICHandlesEarlyTCPCloses(t *testing.T) {
	server, err := New(ConfigObj{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeWithin(t, server) })
	client, err := New(ConfigObj{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeWithin(t, client) })

	// Keep routing, encryption and both TCP/IP stacks real without external peers.
	left, right := net.Pipe()
	t.Cleanup(func() {
		_ = left.Close()
		_ = right.Close()
	})
	serverCore, clientCore := server.corePtr.Load(), client.corePtr.Load()
	go func() { _ = serverCore.HandleConn(clientCore.PublicKey(), left, 0) }()
	go func() { _ = clientCore.HandleConn(serverCore.PublicKey(), right, 0) }()

	address := net.JoinHostPort(server.Address().String(), "18080")
	listener, err := server.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer closeEchoConn(conn)
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	readyBy := time.Now().Add(10 * time.Second)
	for {
		if err = echoRoundTrip(client, address, false); err == nil {
			break
		}
		if time.Now().After(readyBy) {
			t.Fatalf("echo did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Early closes produce late segments and synchronous TCP resets. Sending
	// those replies must not block the core reader delivering incoming packets.
	var workers sync.WaitGroup
	errors := make(chan error, 8)
	progress := make(chan struct{}, 1)
	stop := make(chan struct{})
	defer close(stop)
	for worker := 0; worker < cap(errors); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 128; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if err := echoRoundTrip(client, address, i%4 == 0); err != nil {
					errors <- err
					return
				}
				select {
				case progress <- struct{}{}:
				default:
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	// Detect stalled exchanges without imposing a throughput requirement on
	// slower race-enabled CI runners. Every successful exchange resets the timer.
	const idleTimeout = 15 * time.Second
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()
wait:
	for {
		select {
		case <-done:
			break wait
		case <-progress:
			idle.Reset(idleTimeout)
		case <-idle.C:
			t.Fatal("TCP exchanges made no progress for 15s despite operation deadlines")
		}
	}
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if err := echoRoundTrip(client, address, false); err != nil {
		t.Errorf("echo after early closes: %v", err)
	}
}
