package common

import (
	"context"
	"sync"
)

// TaskGroupObj owns cancellable work and rejects new tasks after shutdown starts.
type TaskGroupObj struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	done     chan struct{}
	stopping bool
}

// NewTaskGroup creates a task group derived from parent. A nil parent uses
// context.Background.
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

// Context returns the context cancelled by Stop.
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

// Do runs fn in the caller and tracks it as owned work. The boolean is false
// when shutdown has already started.
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

// Wait starts shutdown and blocks until every accepted task exits.
func (g *TaskGroupObj) Wait() { <-g.Stop() }
