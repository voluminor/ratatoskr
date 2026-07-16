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

// Obj owns a validated list of public Internet addresses.
type Obj struct {
	addrs []string
}

// New creates an inet sigil with 1 to 32 unique addresses.
func New(addrs []string) (*Obj, error) {
	if err := validateAddrs(addrs); err != nil {
		return nil, err
	}
	return &Obj{addrs: cloneAddrs(addrs)}, nil
}

// //

// GetName returns Name.
func (o *Obj) GetName() string {
	return Name()
}

// GetParams returns Keys.
func (o *Obj) GetParams() []string {
	return Keys()
}

// SetParams merges the current fragment into a copy of NodeInfo.
func (o *Obj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	return sigils.MergeParams(NodeInfo, o.Params())
}

// ParseParams extracts the inet fragment and replaces current data when valid.
func (o *Obj) ParseParams(NodeInfo map[string]any) map[string]any {
	parsed := ParseParams(NodeInfo)

	if addrs, ok := parseAddrs(parsed); ok {
		if err := validateAddrs(addrs); err == nil {
			o.addrs = cloneAddrs(addrs)
		}
	}

	return parsed
}

// Match reports whether NodeInfo contains a valid inet fragment.
func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

// Clone returns an independent copy.
func (o *Obj) Clone() sigils.Interface {
	return &Obj{addrs: cloneAddrs(o.addrs)}
}

// Params returns an independent NodeInfo fragment.
func (o *Obj) Params() map[string]any {
	if len(o.addrs) == 0 {
		return map[string]any{}
	}
	return map[string]any{sigName: cloneAddrs(o.addrs)}
}
