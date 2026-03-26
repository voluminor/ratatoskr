package core

import (
	"fmt"
	"sync"
)

// // // // // // // // // //

// componentObj — обобщённый управляемый компонент с Enable/Disable lifecycle.
// Потокобезопасен; гарантирует что активен не более одного экземпляра.
// value хранит типизированный экземпляр для доступа через get()
type componentObj struct {
	name   string
	mu     sync.RWMutex
	value  any
	stopFn func() error
}

// enable создаёт компонент; ошибка если уже активен.
// start возвращает экземпляр (для типизированного доступа) и функцию остановки
func (c *componentObj) enable(start func() (any, func() error, error)) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.value != nil {
		return fmt.Errorf("%s already enabled", c.name)
	}
	val, stop, err := start()
	if err != nil {
		return err
	}
	c.value = val
	c.stopFn = stop
	return nil
}

// disable останавливает компонент; no-op если не активен
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

// get возвращает типизированный экземпляр (nil если неактивен)
func (c *componentObj) get() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}
