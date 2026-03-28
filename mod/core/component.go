package core

import (
	"fmt"
	"sync"
)

// // // // // // // // // //

// componentObj — thread-safe Enable/Disable lifecycle; at most one active instance
type componentObj struct {
	name   string
	mu     sync.RWMutex
	value  any
	stopFn func() error
}

// enable creates the component; error if already active
func (c *componentObj) enable(start func() (any, func() error, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.value != nil {
		return fmt.Errorf("%w: %s", ErrAlreadyEnabled, c.name)
	}
	val, stop, err := start()
	if err != nil {
		return err
	}
	c.value = val
	c.stopFn = stop
	return nil
}

// disable stops the component; no-op if inactive
func (c *componentObj) disable() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.value == nil {
		return nil
	}
	err := c.stopFn()
	c.value = nil
	c.stopFn = nil
	return err
}

// get returns the instance; nil if inactive
func (c *componentObj) get() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}
