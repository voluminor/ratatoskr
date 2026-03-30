package ninfo

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/voluminor/ratatoskr/mod/sigils"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj holds a reference to the running core and logger.
type Obj struct {
	logger   yggcore.Logger
	core     *yggcore.Core
	nodeInfo yggcore.AddHandlerFunc
	sigils   map[string]sigils.Interface
}

// //

// ImportModeObj controls how ImportSigils handles name conflicts.
type ImportModeObj int

const (
	// ImportAppend adds sigils, returns error for each name conflict.
	ImportAppend ImportModeObj = iota
	// ImportReplace adds sigils, overwrites on name conflict.
	ImportReplace
	// ImportReset clears all existing sigils, then writes from source.
	ImportReset
)

// // // // // // // // // //

type adminCaptureObj struct {
	handlers map[string]yggcore.AddHandlerFunc
}

func (a *adminCaptureObj) AddHandler(name, _ string, _ []string, fn yggcore.AddHandlerFunc) error {
	a.handlers[name] = fn
	return nil
}

// //

func (obj *Obj) callNodeInfo(key [32]byte) (json.RawMessage, error) {
	req, _ := json.Marshal(map[string]string{
		"key": hex.EncodeToString(key[:]),
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
		sigils:   make(map[string]sigils.Interface),
	}, nil
}

// Close releases resources held by the module.
func (obj *Obj) Close() error {
	return nil
}

// // // // // // // // // //

// AddSigil registers parse sigils used by Ask/AskAddr.
// Invalid or duplicate names are skipped and collected as errors.
func (obj *Obj) AddSigil(sg ...sigils.Interface) []error {
	var errs []error
	for _, si := range sg {
		name := si.GetName()
		if !sigils.ValidateName(name) {
			errs = append(errs, fmt.Errorf("sigil[%s] is invalid", name))
			continue
		}
		if _, ok := obj.sigils[name]; ok {
			errs = append(errs, fmt.Errorf("duplicated sigil[%s]", name))
			continue
		}
		obj.sigils[name] = si
	}
	return errs
}

// GetSigil returns a registered parse sigil by name, or nil if not found.
func (obj *Obj) GetSigil(name string) sigils.Interface {
	return obj.sigils[name]
}

// DelSigil removes a parse sigil by name.
func (obj *Obj) DelSigil(name string) error {
	if _, ok := obj.sigils[name]; !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}
	delete(obj.sigils, name)
	return nil
}

// //

// ImportSigils transfers sigils from a SigilsObj into parse sigils.
// Behavior on name conflict is controlled by mode:
//   - ImportAppend: error on conflict, keep existing
//   - ImportReplace: overwrite on conflict
//   - ImportReset: clear all existing, write only from source
func (obj *Obj) ImportSigils(src *SigilsObj, mode ImportModeObj) []error {
	if mode == ImportReset {
		obj.sigils = make(map[string]sigils.Interface, len(src.sigils))
		for name, si := range src.sigils {
			obj.sigils[name] = si
		}
		return nil
	}

	var errs []error
	for name, si := range src.sigils {
		if _, exists := obj.sigils[name]; exists {
			if mode == ImportAppend {
				errs = append(errs, fmt.Errorf("sigil[%s] already exists", name))
				continue
			}
		}
		obj.sigils[name] = si
	}
	return errs
}

// //

func (obj *Obj) sigilSlice() []sigils.Interface {
	if len(obj.sigils) == 0 {
		return nil
	}
	out := make([]sigils.Interface, 0, len(obj.sigils))
	for _, si := range obj.sigils {
		out = append(out, si)
	}
	return out
}
