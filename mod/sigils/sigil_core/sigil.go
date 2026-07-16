package sigil_core

import (
	"fmt"
	"sync"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// Obj owns a concurrency-safe sigil registry and its assembled NodeInfo.
type Obj struct {
	mu            sync.RWMutex
	localNodeInfo map[string]any
	sigils        map[string]sigils.Interface
}

// New copies the base NodeInfo and adds each valid sigil. It returns per-sigil
// errors while keeping every entry that was assembled successfully.
func New(nodeInfo map[string]any, sg ...sigils.Interface) (*Obj, []error) {
	s := new(Obj)
	s.sigils = make(map[string]sigils.Interface)

	errs := make([]error, 0)
	var err error
	s.localNodeInfo, err = common.CloneNodeInfo(nodeInfo)
	if err != nil {
		errs = append(errs, fmt.Errorf("base NodeInfo: %w", err))
	}
	if s.localNodeInfo == nil {
		s.localNodeInfo = make(map[string]any)
	}

	if len(sg) > 0 {
		errs = append(errs, s.Add(sg...)...)
	}

	return s, errs
}

// //

// NodeInfo returns a deep copy of the assembled NodeInfo.
func (s *Obj) NodeInfo() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cloned, err := common.CloneNodeInfo(s.localNodeInfo)
	if err != nil {
		return nil
	}
	return cloned
}

// Sigils returns an independent registry whose values are cloned sigils.
func (s *Obj) Sigils() map[string]sigils.Interface {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]sigils.Interface, len(s.sigils))
	for name, sg := range s.sigils {
		if sg == nil {
			continue
		}
		cloned := sg.Clone()
		if cloned == nil {
			continue
		}
		out[name] = cloned
	}
	return out
}

// String returns the Ratatoskr metadata key and value.
func (s *Obj) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, _ := s.localNodeInfo[target.Name].(string)
	if value == "" {
		value = CompileInfo(s.sigils)
	}
	return fmt.Sprintf("%s %s", target.Name, value)
}

// LenSigils returns the number of registered sigils.
func (s *Obj) LenSigils() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sigils)
}

// LenLocal returns the number of assembled NodeInfo keys, including metadata.
func (s *Obj) LenLocal() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.localNodeInfo)
}

// Len returns LenSigils plus LenLocal.
func (s *Obj) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sigils) + len(s.localNodeInfo)
}

// //

// Add clones and registers each valid sigil, merges its parameters, and updates
// metadata. Invalid entries are skipped and returned as independent errors.
func (s *Obj) Add(sg ...sigils.Interface) []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sigils == nil {
		s.sigils = make(map[string]sigils.Interface)
	}
	if s.localNodeInfo == nil {
		s.localNodeInfo = make(map[string]any)
	}
	errs := make([]error, 0)
	defer func() {
		s.localNodeInfo[target.Name] = CompileInfo(s.sigils)
	}()

	for i, si := range sg {
		if si == nil {
			errs = append(errs, fmt.Errorf("sigil[%d] is nil", i))
			continue
		}
		if !sigils.ValidateName(si.GetName()) {
			errs = append(errs, fmt.Errorf("sigil[%s] is invalid", si.GetName()))
			continue
		}

		if _, ok := s.sigils[si.GetName()]; ok {
			errs = append(errs, fmt.Errorf("duplicated sigil[%s]", si.GetName()))
			continue
		}
		if len(s.sigils) >= maxInfoSigils {
			errs = append(errs, fmt.Errorf("too many sigils: %d (max %d)", len(s.sigils)+1, maxInfoSigils))
			continue
		}

		params, err := common.CloneNodeInfo(si.Params())
		if err != nil {
			errs = append(errs, fmt.Errorf("sigil[%s] has invalid NodeInfo params: %w", si.GetName(), err))
			continue
		}
		bufMap, err := sigils.MergeParams(s.localNodeInfo, params)
		if err != nil {
			errs = append(errs, fmt.Errorf("sigil[%s] not add: %v", si.GetName(), err))
			continue
		}

		clone := si.Clone()
		if clone == nil {
			errs = append(errs, fmt.Errorf("sigil[%s] Clone returned nil", si.GetName()))
			continue
		}
		name := si.GetName()
		s.sigils[name] = clone
		s.localNodeInfo = bufMap
	}

	return errs
}

// Get returns a clone of the named sigil, or nil when it is not registered.
func (s *Obj) Get(name string) sigils.Interface {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sg, ok := s.sigils[name]
	if !ok {
		return nil
	}
	return sg.Clone()
}

// Del removes a sigil, its populated keys, and its metadata entry.
func (s *Obj) Del(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sg, ok := s.sigils[name]
	if !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}

	delete(s.sigils, sg.GetName())

	for key := range sg.Params() {
		delete(s.localNodeInfo, key)
	}

	s.localNodeInfo[target.Name] = CompileInfo(s.sigils)

	return nil
}
