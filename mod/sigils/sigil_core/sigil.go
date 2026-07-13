package sigil_core

import (
	"fmt"
	"sync"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// Obj manages sigils and assembles local NodeInfo.
type Obj struct {
	mu            sync.RWMutex
	localNodeInfo map[string]any
	sigils        map[string]sigils.Interface
}

// New creates a Obj with the given base NodeInfo and optional sigils.
// Returned errors are non-fatal: each failed sigil is skipped,
// the rest are applied normally.
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

// NodeInfo returns a copy of the assembled map ready for yggcore.NodeInfo option.
// A copy prevents a holder from structurally mutating the served map; nested
// values are already independent because each sigil's Params deep-copies them.
func (s *Obj) NodeInfo() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cloned, err := common.CloneNodeInfo(s.localNodeInfo)
	if err != nil {
		return nil
	}
	return cloned
}

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

func (s *Obj) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, _ := s.localNodeInfo[target.Name].(string)
	if value == "" {
		value = CompileInfo(s.sigils)
	}
	return fmt.Sprintf("%s %s", target.Name, value)
}

func (s *Obj) LenSigils() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sigils)
}

func (s *Obj) LenLocal() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.localNodeInfo)
}

func (s *Obj) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sigils) + len(s.localNodeInfo)
}

// //

// Add registers sigils and writes their keys into localNodeInfo.
// Each sigil is validated, checked for name conflicts, and then merged in via
// the package-owned MergeParams over the sigil's own Params — the sigil never
// receives the live map, so a contract-violating third-party sigil cannot mutate
// shared state. On failure it is skipped and the error is collected.
// After all sigils are processed, the ratatoskr metadata key is updated.
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

		// Store a clone so the module fully owns its state: a caller mutating its
		// own sigil after Add cannot change what Del later removes (Del reads
		// Params off the stored value). A sigil that cannot clone itself is rejected.
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

// Get returns an independent clone of a registered sigil by name, or nil if not
// found. Cloning keeps a caller from mutating the module's stored sigil state.
func (s *Obj) Get(name string) sigils.Interface {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sg, ok := s.sigils[name]
	if !ok {
		return nil
	}
	return sg.Clone()
}

// Del removes a sigil and deletes its keys from localNodeInfo.
func (s *Obj) Del(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sg, ok := s.sigils[name]
	if !ok {
		return fmt.Errorf("sigil[%s] not found", name)
	}

	delete(s.sigils, sg.GetName())

	// Remove exactly the keys the sigil wrote (its own Params); declared-but-unset
	// keys stay untouched so a user's base value under the same key survives.
	for key := range sg.Params() {
		delete(s.localNodeInfo, key)
	}

	s.localNodeInfo[target.Name] = CompileInfo(s.sigils)

	return nil
}
