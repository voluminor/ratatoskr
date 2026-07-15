package core

import (
	"fmt"
	"sync"
)

// // // // // // // // // //

type componentObj[T any] struct {
	name   string
	mu     sync.RWMutex
	value  T
	active bool
	stopFn func() error
}

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

func (c *componentObj[T]) get() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value, c.active
}
