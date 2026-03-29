package ninfo

import (
	"errors"
	"fmt"

	"github.com/voluminor/ratatoskr/target"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type Obj struct {
	logger yggcore.Logger
	core   *yggcore.Core

	localNodeInfo map[string]any
	sigils        map[string]SigilInterface
}

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
	errors := make([]error, 0)

	if NodeInfo == nil {
		NodeInfo = make(map[string]any)
	}
	obj.localNodeInfo = NodeInfo

	if len(sigils) == 0 {
		return obj, errors
	}

	errors = append(errors, obj.Add(sigils...)...)

	return obj, errors
}

func (obj *Obj) NodeInfo() map[string]any {
	return obj.localNodeInfo
}

func (obj *Obj) String() string {
	return fmt.Sprintf("%s %s", target.GlobalName, obj.localNodeInfo[target.GlobalName].(string))
}

// //

func (obj *Obj) Add(sigils ...SigilInterface) []error {
	errors := make([]error, 0)
	defer func() {
		obj.localNodeInfo[target.GlobalName] = compileRatatoskrInfo(obj.sigils)
	}()

	for _, sg := range sigils {
		if !ValidateSigilIName(sg.GetName()) {
			errors = append(errors, fmt.Errorf("sigil[%s] is invalid", sg.GetName()))
			continue
		}

		if _, ok := obj.sigils[sg.GetName()]; ok {
			errors = append(errors, fmt.Errorf("duplicated sigil[%s]", sg.GetName()))
			continue
		}

		obj.sigils[sg.GetName()] = sg
	}

	for name, sg := range obj.sigils {
		bufMap, err := sg.SetParams(obj.localNodeInfo)
		if err != nil {
			errors = append(errors, fmt.Errorf("sigil[%s] not add: %v", name, err))
			continue
		}

		obj.localNodeInfo = bufMap
	}

	return errors
}

func (obj *Obj) Get(name string) SigilInterface {
	sg, ok := obj.sigils[name]
	if !ok {
		return nil
	}

	return sg
}

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
