package core

import (
	"fmt"
	"sync"
)

// // // // // // // // // //

// componentObj — thread-safe Enable/Disable lifecycle; at most one active instance
type componentObj[T any] struct {
	name   string
	mu     sync.RWMutex
	value  T
	active bool
	stopFn func() error
}

// enable creates the component; error if already active
func (c *componentObj[T]) enable(start func() (T, func() error, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.active {
		return fmt.Errorf("%w: %s", ErrAlreadyEnabled, c.name)
	}
	val, stop, err := start()
	if err != nil {
		return err
	}
	c.value = val
	c.active = true
	c.stopFn = stop
	return nil
}

// disable stops the component; no-op if inactive
func (c *componentObj[T]) disable() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.active {
		return nil
	}
	err := c.stopFn()
	var zero T
	c.value = zero
	c.active = false
	c.stopFn = nil
	return err
}

// get returns the instance and whether it is active.
func (c *componentObj[T]) get() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value, c.active
}
