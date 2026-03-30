package ninfo

import (
	"encoding/json"

	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj holds a reference to the running core and logger.
type Obj struct {
	logger   yggcore.Logger
	core     *yggcore.Core
	nodeInfo yggcore.AddHandlerFunc
}

// //

type adminCaptureObj struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCaptureObj) AddHandler(name, _ string, _ []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// // // // // // // // // //

// New creates an ninfo module.
// Captures getNodeInfo via core.SetAdmin.
func New(core *yggcore.Core, logger yggcore.Logger) (*Obj, error) {
	if core == nil {
		return nil, ErrCoreRequired
	}
	if logger == nil {
		return nil, ErrLoggerRequired
	}

	capture := &adminCaptureObj{handlers: make(map[string]yggcore.AddHandlerFunc)}
	_ = core.SetAdmin(capture)

	nodeInfo := capture.handlers["getNodeInfo"]
	if nodeInfo == nil {
		return nil, ErrNodeInfoNotCaptured
	}

	return &Obj{
		logger:   logger,
		core:     core,
		nodeInfo: nodeInfo,
	}, nil
}

// //

func (obj *Obj) callNodeInfo(key [32]byte) (json.RawMessage, error) {
	req, _ := json.Marshal(map[string]string{
		"key": encodeHexKey(key[:]),
	})
	raw, err := obj.nodeInfo(req)
	if err != nil {
		return nil, err
	}

	resp, ok := raw.(yggcore.GetNodeInfoResponse)
	if !ok {
		return nil, ErrUnexpectedResponse
	}

	for _, msg := range resp {
		return msg, nil
	}
	return nil, ErrEmptyResponse
}

// Close releases resources held by the module.
func (obj *Obj) Close() error {
	return nil
}
