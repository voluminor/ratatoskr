package probe

import (
	"context"
	"crypto/ed25519"
	"sync"
	"time"
)

// // // // // // // // // //

// peerTaskObj is a unit of work for the worker pool.
type peerTaskObj struct {
	ctx    context.Context
	key    ed25519.PublicKey
	result chan<- peerResultObj
}

// peerResultObj is the outcome of a single remote peer query.
type peerResultObj struct {
	key   ed25519.PublicKey
	peers []ed25519.PublicKey
	rtt   time.Duration
	err   error
}

// //

// workerPoolObj runs a fixed number of goroutines that execute remote peer queries.
// Limits both concurrency and total goroutine count.
type workerPoolObj struct {
	tasks chan peerTaskObj
	wg    sync.WaitGroup
	call  func(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error)
}

// //

func newWorkerPool(size int, call func(ctx context.Context, key ed25519.PublicKey) ([]ed25519.PublicKey, time.Duration, error)) *workerPoolObj {
	p := &workerPoolObj{
		tasks: make(chan peerTaskObj, size),
		call:  call,
	}
	p.wg.Add(size)
	for range size {
		go p.worker()
	}
	return p
}

func (p *workerPoolObj) worker() {
	defer p.wg.Done()
	for t := range p.tasks {
		peers, rtt, err := p.call(t.ctx, t.key)
		t.result <- peerResultObj{key: t.key, peers: peers, rtt: rtt, err: err}
	}
}

// submit sends a task to the pool. Blocks if all workers are busy.
func (p *workerPoolObj) submit(ctx context.Context, key ed25519.PublicKey, result chan<- peerResultObj) {
	select {
	case p.tasks <- peerTaskObj{ctx: ctx, key: key, result: result}:
	case <-ctx.Done():
		result <- peerResultObj{key: key, err: ctx.Err()}
	}
}

// stop shuts down the pool and waits for all workers to finish.
func (p *workerPoolObj) stop() {
	close(p.tasks)
	p.wg.Wait()
}
