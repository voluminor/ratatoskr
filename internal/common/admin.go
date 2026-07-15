package common

import yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

// // // // // // // // // //

// AdminCaptureObj records handlers registered through Yggdrasil SetAdmin.
type AdminCaptureObj struct {
	// Handlers maps each registered command name to its callback.
	Handlers map[string]yggcore.AddHandlerFunc
}

// NewAdminCapture returns an empty handler capture.
func NewAdminCapture() *AdminCaptureObj {
	return &AdminCaptureObj{Handlers: make(map[string]yggcore.AddHandlerFunc)}
}

// AddHandler records the callback under its command name.
func (a *AdminCaptureObj) AddHandler(name, _ string, _ []string, fn yggcore.AddHandlerFunc) error {
	a.Handlers[name] = fn
	return nil
}
