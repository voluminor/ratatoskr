package traceroute

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync/atomic"
	"testing"
	"time"
)

// // // // // // // // // //

func TestWorkerPool_basic(t *testing.T) {
	var calls atomic.Int64
	call := func(_ context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		calls.Add(1)
		return []ed25519.PublicKey{key}, 10 * time.Millisecond, nil
	}

	pool := newWorkerPool(4, call)
	defer pool.stop()

	ctx := context.Background()
	results := make(chan peerResultObj, 10)
	keys := genKeyN(t, 10)
	for _, k := range keys {
		pool.submit(ctx, k, results)
	}

	for range 10 {
		r := <-results
		if r.err != nil {
			t.Fatalf("unexpected error: %v", r.err)
		}
		if len(r.peers) != 1 {
			t.Fatalf("expected 1 peer, got %d", len(r.peers))
		}
	}
	if calls.Load() != 10 {
		t.Fatalf("expected 10 calls, got %d", calls.Load())
	}
}

func TestWorkerPool_ctxCancel(t *testing.T) {
	call := func(ctx context.Context, _ ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}

	pool := newWorkerPool(2, call)

	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan peerResultObj, 1)

	cancel()
	pool.submit(ctx, genKey(t), results)
	r := <-results
	if r.err == nil {
		t.Fatal("expected error on cancelled context")
	}

	pool.stop()
}

func TestWorkerPool_stopDrainsAll(t *testing.T) {
	var count atomic.Int64
	call := func(_ context.Context, _ ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		count.Add(1)
		return nil, 0, nil
	}

	pool := newWorkerPool(2, call)
	ctx := context.Background()
	results := make(chan peerResultObj, 5)
	for range 5 {
		pool.submit(ctx, genKey(t), results)
	}
	for range 5 {
		<-results
	}
	pool.stop()
	if count.Load() != 5 {
		t.Fatalf("expected 5 calls, got %d", count.Load())
	}
}

// // // // // // // // // //

func BenchmarkWorkerPool(b *testing.B) {
	call := func(_ context.Context, k ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error) {
		return []ed25519.PublicKey{k}, 0, nil
	}
	pool := newWorkerPool(8, call)
	defer pool.stop()

	ctx := context.Background()
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	results := make(chan peerResultObj, b.N+1)

	for b.Loop() {
		pool.submit(ctx, pk, results)
		<-results
	}
}
