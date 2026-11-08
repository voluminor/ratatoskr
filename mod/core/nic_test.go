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

func echoRoundTrip(node *Obj, address string, earlyClose bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := node.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("dial echo: %w", err)
	}
	defer conn.Close()
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
	go func() { _ = serverCore.PacketConn.HandleConn(clientCore.PublicKey(), left, 0) }()
	go func() { _ = clientCore.PacketConn.HandleConn(serverCore.PublicKey(), right, 0) }()

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
				defer conn.Close()
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
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("TCP exchanges blocked despite operation deadlines")
	}
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if err := echoRoundTrip(client, address, false); err != nil {
		t.Errorf("echo after early closes: %v", err)
	}
}
