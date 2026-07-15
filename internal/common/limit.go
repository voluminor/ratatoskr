package common

import "sync"

// // // // // // // // // //

// DynamicLimitObj is a runtime-adjustable admission counter. Non-positive
// limits allow unlimited acquisitions.
type DynamicLimitObj struct {
	mu     sync.Mutex
	limit  int
	active uint32
	ready  chan struct{}
}

// NewDynamicLimit creates a limit with no active acquisitions.
func NewDynamicLimit(limit int) *DynamicLimitObj {
	l := &DynamicLimitObj{}
	l.Set(limit)
	return l
}

// Set changes the limit and wakes blocked admission loops.
func (l *DynamicLimitObj) Set(limit int) {
	l.mu.Lock()
	l.limit = limit
	l.signalLocked()
	l.mu.Unlock()
}

// Limit returns the configured limit.
func (l *DynamicLimitObj) Limit() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// Active returns the number of acquired slots.
func (l *DynamicLimitObj) Active() uint32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active
}

// Acquire reserves a slot when the current limit permits it.
func (l *DynamicLimitObj) Acquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquireLocked()
}

// AcquireOrReady either reserves a slot or returns a channel closed after the
// limit changes or a slot is released. Callers must retry after the wake-up.
func (l *DynamicLimitObj) AcquireOrReady() (bool, <-chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.acquireLocked() {
		return true, nil
	}
	return false, l.readyLocked()
}

// Release returns one acquired slot. Calls with no active slot are ignored.
func (l *DynamicLimitObj) Release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
		l.signalLocked()
	}
	l.mu.Unlock()
}

func (l *DynamicLimitObj) acquireLocked() bool {
	if l.limit > 0 && int(l.active) >= l.limit {
		return false
	}
	l.active++
	return true
}

func (l *DynamicLimitObj) readyLocked() chan struct{} {
	if l.ready == nil {
		l.ready = make(chan struct{})
	}
	return l.ready
}

func (l *DynamicLimitObj) signalLocked() {
	if l.ready == nil {
		return
	}
	close(l.ready)
	l.ready = nil
}
