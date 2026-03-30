package ninfo

import (
	"errors"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj holds a reference to the running core and logger.
type Obj struct {
	logger yggcore.Logger
	core   *yggcore.Core
}

// New creates an ninfo module.
func New(core *yggcore.Core, logger yggcore.Logger) (*Obj, error) {
	if core == nil {
		return nil, errors.New("core is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}

	obj := new(Obj)
	obj.logger = logger
	obj.core = core

	return obj, nil
}

// Close releases resources held by the module.
func (obj *Obj) Close() error {
	return nil
}
