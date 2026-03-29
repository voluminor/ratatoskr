package ninfo

import (
	"errors"
	"fmt"

	"github.com/voluminor/ratatoskr/target"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// Obj assembles local NodeInfo from sigils and holds
// a reference to the running core for future remote queries.
type Obj struct {
	logger yggcore.Logger
	core   *yggcore.Core

	localNodeInfo map[string]any
	sigils        map[string]SigilInterface
}

// New creates an ninfo module.
// NodeInfo is the base map (may be nil); sigils are applied on top.
// Returned errors are non-fatal: each failed sigil is skipped,
// the rest are applied normally.
func New(core *yggcore.Core, logger yggcore.Logger, NodeInfo map[string]any, sigils ...SigilInterface) (*Obj, []error) {
	if core == nil {
		return nil, []error{errors.New("core is required")}
	}
	if logger == nil {
		return nil, []error{errors.New("logger is required")}
	}

	obj := new(Obj)
	obj.logger = logger
	obj.core = core
	obj.sigils = make(map[string]SigilInterface)

	if NodeInfo == nil {
		NodeInfo = make(map[string]any)
	}
	obj.localNodeInfo = NodeInfo

	errs := make([]error, 0)
	if len(sigils) > 0 {
		errs = append(errs, obj.Add(sigils...)...)
	}

	return obj, errs
}

// //

// NodeInfo returns the assembled map ready for yggcore.NodeInfo option.
func (obj *Obj) NodeInfo() map[string]any {
	return obj.localNodeInfo
}

// String returns a short human-readable summary.
func (obj *Obj) String() string {
	return fmt.Sprintf("%s %s", target.GlobalName, obj.localNodeInfo[target.GlobalName].(string))
}

// //

// Add registers sigils and writes their keys into localNodeInfo.
// Each sigil is validated, checked for name/key conflicts, and then
// applied via SetParams. On success the sigil is registered;
// on failure it is skipped and the error is collected.
// After all sigils are processed, the ratatoskr metadata key is updated.
func (obj *Obj) Add(sigils ...SigilInterface) []error {
	errs := make([]error, 0)
	defer func() {
		obj.localNodeInfo[target.GlobalName] = compileRatatoskrInfo(obj.sigils)
	}()

	for _, sg := range sigils {
		if !ValidateSigilName(sg.GetName()) {
			errs = append(errs, fmt.Errorf("sigil[%s] is invalid", sg.GetName()))
			continue
		}

		if _, ok := obj.sigils[sg.GetName()]; ok {
			errs = append(errs, fmt.Errorf("duplicated sigil[%s]", sg.GetName()))
			continue
		}

		bufMap, err := sg.SetParams(obj.localNodeInfo)
		if err != nil {
			errs = append(errs, fmt.Errorf("sigil[%s] not add: %v", sg.GetName(), err))
			continue
		}

		obj.sigils[sg.GetName()] = sg
		obj.localNodeInfo = bufMap
	}

	return errs
}

// Get returns a registered sigil by name, or nil if not found.
func (obj *Obj) Get(name string) SigilInterface {
	sg, ok := obj.sigils[name]
	if !ok {
		return nil
	}
	return sg
}

// Del removes a sigil and deletes its keys from localNodeInfo.
func (obj *Obj) Del(name string) error {
	sg, ok := obj.sigils[name]
	if !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}

	delete(obj.sigils, sg.GetName())

	for _, key := range sg.GetParams() {
		delete(obj.localNodeInfo, key)
	}

	return nil
}
