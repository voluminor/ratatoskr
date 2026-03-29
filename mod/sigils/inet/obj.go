package inet

import (
	"errors"
	"fmt"
)

// // // // // // // // // //

func validateAddrs(addrs []string) error {
	if len(addrs) == 0 {
		return errors.New("empty addrs")
	}
	if len(addrs) > maxAddrs {
		return fmt.Errorf("too many addrs: %d (max %d)", len(addrs), maxAddrs)
	}

	seen := make(map[string]bool, len(addrs))
	for i, addr := range addrs {
		if !reAddr.MatchString(addr) {
			return fmt.Errorf("invalid addr [%d]: %s", i, addr)
		}
		if seen[addr] {
			return fmt.Errorf("duplicate addr [%d]: %s", i, addr)
		}
		seen[addr] = true
	}

	return nil
}

// Obj — real internet addresses of the node.
type Obj struct {
	addrs []string
}

// New creates the "inet" sigil. Max 32 addresses, no duplicates.
func New(addrs []string) (*Obj, error) {
	if err := validateAddrs(addrs); err != nil {
		return nil, err
	}
	return &Obj{addrs: addrs}, nil
}

// //

func (o *Obj) GetName() string {
	return Name()
}

func (o *Obj) GetParams() []string {
	return Keys()
}

func (o *Obj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+1)
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	if _, ok := bufMap[sigName]; ok {
		return nil, fmt.Errorf("conflict key: %s", sigName)
	}

	bufMap[sigName] = o.addrs
	return bufMap, nil
}

func (o *Obj) ParseParams(NodeInfo map[string]any) map[string]any {
	parsed := ParseParams(NodeInfo)

	if raw, ok := parsed[sigName].([]any); ok {
		addrs := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				addrs = append(addrs, s)
			}
		}
		o.addrs = addrs
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Params() map[string]any {
	if len(o.addrs) == 0 {
		return map[string]any{}
	}
	return map[string]any{sigName: o.addrs}
}
