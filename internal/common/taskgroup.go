package common

import (
	"context"
	"sync"
)

// TaskGroupObj owns a cancellable set of background tasks. Go and Stop are
// serialized so WaitGroup.Add can never race Wait, and Stop exposes one stable
// completion channel to every waiter.
type TaskGroupObj struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	done     chan struct{}
	stopping bool
}

func NewTaskGroup(parent context.Context) *TaskGroupObj {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &TaskGroupObj{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (g *TaskGroupObj) Context() context.Context { return g.ctx }

// Go transfers ownership of fn to the group. It returns false once shutdown has
// started; in that case fn is not called.
func (g *TaskGroupObj) Go(fn func(context.Context)) bool {
	if fn == nil {
		return false
	}
	g.mu.Lock()
	if g.stopping {
		g.mu.Unlock()
		return false
	}
	g.wg.Add(1)
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		fn(g.ctx)
	}()
	return true
}

// Do runs fn in the caller while registering it as owned work. The boolean is
// false when shutdown has already started. It avoids a helper goroutine for
// synchronous operations that Stop still needs to wait for.
func (g *TaskGroupObj) Do(fn func(context.Context) error) (error, bool) {
	if fn == nil {
		return nil, false
	}
	g.mu.Lock()
	if g.stopping {
		g.mu.Unlock()
		return nil, false
	}
	g.wg.Add(1)
	g.mu.Unlock()
	defer g.wg.Done()
	return fn(g.ctx), true
}

// Stop starts cancellation and returns a channel closed after every accepted
// task exits. It is idempotent and does not itself block.
func (g *TaskGroupObj) Stop() <-chan struct{} {
	g.mu.Lock()
	if !g.stopping {
		g.stopping = true
		g.cancel()
		done := g.done
		go func() {
			g.wg.Wait()
			close(done)
		}()
	}
	done := g.done
	g.mu.Unlock()
	return done
}

func (g *TaskGroupObj) Wait() { <-g.Stop() }
