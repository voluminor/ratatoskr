package inet

import (
	"errors"
	"fmt"

	"github.com/voluminor/ratatoskr/mod/sigils"
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

func cloneAddrs(addrs []string) []string {
	return append([]string(nil), addrs...)
}

func parseAddrs(NodeInfo map[string]any) ([]string, bool) {
	raw, ok := ParseParams(NodeInfo)[sigName]
	if !ok {
		return nil, false
	}

	switch arr := raw.(type) {
	case []any:
		if len(arr) > maxAddrs {
			return nil, false
		}
		addrs := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			addrs = append(addrs, s)
		}
		return addrs, true
	case []string:
		return cloneAddrs(arr), true
	default:
		return nil, false
	}
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
	return &Obj{addrs: cloneAddrs(addrs)}, nil
}

// //

func (o *Obj) GetName() string {
	return Name()
}

func (o *Obj) GetParams() []string {
	return Keys()
}

func (o *Obj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	return sigils.MergeParams(NodeInfo, o.Params())
}

func (o *Obj) ParseParams(NodeInfo map[string]any) map[string]any {
	parsed := ParseParams(NodeInfo)

	if addrs, ok := parseAddrs(parsed); ok {
		if err := validateAddrs(addrs); err == nil {
			o.addrs = cloneAddrs(addrs)
		}
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Clone() sigils.Interface {
	return &Obj{addrs: cloneAddrs(o.addrs)}
}

func (o *Obj) Params() map[string]any {
	if len(o.addrs) == 0 {
		return map[string]any{}
	}
	// Deep-copy the nested slice so the returned fragment cannot alias internal state.
	return map[string]any{sigName: cloneAddrs(o.addrs)}
}
