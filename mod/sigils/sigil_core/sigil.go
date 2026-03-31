package sigil_core

import (
	"fmt"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// Obj manages sigils and assembles local NodeInfo.
type Obj struct {
	localNodeInfo map[string]any
	sigils        map[string]sigils.Interface
}

// New creates a Obj with the given base NodeInfo and optional sigils.
// Returned errors are non-fatal: each failed sigil is skipped,
// the rest are applied normally.
func New(nodeInfo map[string]any, sg ...sigils.Interface) (*Obj, []error) {
	s := new(Obj)
	s.sigils = make(map[string]sigils.Interface)

	if nodeInfo == nil {
		nodeInfo = make(map[string]any)
	}
	s.localNodeInfo = nodeInfo

	errs := make([]error, 0)
	if len(sg) > 0 {
		errs = append(errs, s.Add(sg...)...)
	}

	return s, errs
}

// //

// NodeInfo returns the assembled map ready for yggcore.NodeInfo option.
func (s *Obj) NodeInfo() map[string]any {
	return s.localNodeInfo
}

func (s *Obj) Sigils() map[string]sigils.Interface {
	return s.sigils
}

func (s *Obj) String() string {
	return fmt.Sprintf("%s %s", target.GlobalName, s.localNodeInfo[target.GlobalName].(string))
}

func (s *Obj) LenSigils() int {
	return len(s.sigils)
}

func (s *Obj) LenLocal() int {
	return len(s.localNodeInfo)
}

func (s *Obj) Len() int {
	return len(s.sigils) + len(s.localNodeInfo)
}

// //

// Add registers sigils and writes their keys into localNodeInfo.
// Each sigil is validated, checked for name conflicts, and then
// applied via SetParams. On failure it is skipped and the error is collected.
// After all sigils are processed, the ratatoskr metadata key is updated.
func (s *Obj) Add(sg ...sigils.Interface) []error {
	errs := make([]error, 0)
	defer func() {
		s.localNodeInfo[target.GlobalName] = CompileInfo(s.sigils)
	}()

	for _, si := range sg {
		if !sigils.ValidateName(si.GetName()) {
			errs = append(errs, fmt.Errorf("sigil[%s] is invalid", si.GetName()))
			continue
		}

		if _, ok := s.sigils[si.GetName()]; ok {
			errs = append(errs, fmt.Errorf("duplicated sigil[%s]", si.GetName()))
			continue
		}

		bufMap, err := si.SetParams(s.localNodeInfo)
		if err != nil {
			errs = append(errs, fmt.Errorf("sigil[%s] not add: %v", si.GetName(), err))
			continue
		}

		s.sigils[si.GetName()] = si
		s.localNodeInfo = bufMap
	}

	return errs
}

// Get returns a registered sigil by name, or nil if not found.
func (s *Obj) Get(name string) sigils.Interface {
	sg, ok := s.sigils[name]
	if !ok {
		return nil
	}
	return sg
}

// Del removes a sigil and deletes its keys from localNodeInfo.
func (s *Obj) Del(name string) error {
	sg, ok := s.sigils[name]
	if !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}

	delete(s.sigils, sg.GetName())

	for _, key := range sg.GetParams() {
		delete(s.localNodeInfo, key)
	}

	s.localNodeInfo[target.GlobalName] = CompileInfo(s.sigils)

	return nil
}
