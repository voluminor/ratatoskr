package public

import (
	"errors"
	"fmt"

	"github.com/voluminor/ratatoskr/mod/sigils"
)

// // // // // // // // // //

func validatePeers(peers map[string][]string) error {
	if len(peers) == 0 {
		return errors.New("empty peers")
	}
	if len(peers) > maxGroups {
		return fmt.Errorf("too many groups: %d (max %d)", len(peers), maxGroups)
	}

	for group, uris := range peers {
		if !reGroup.MatchString(group) {
			return fmt.Errorf("invalid group name: %s", group)
		}
		if len(uris) == 0 {
			return fmt.Errorf("empty group: %s", group)
		}
		if len(uris) > maxURIsPerGroup {
			return fmt.Errorf("too many URIs in group %s: %d (max %d)", group, len(uris), maxURIsPerGroup)
		}
		for i, uri := range uris {
			if !reURI.MatchString(uri) {
				return fmt.Errorf("invalid URI (%s)[%d]", group, i)
			}
		}
	}

	return nil
}

func clonePeers(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for k, v := range src {
		dst[k] = append([]string(nil), v...)
	}
	return dst
}

func parsePeers(NodeInfo map[string]any) (map[string][]string, bool) {
	raw, ok := ParseParams(NodeInfo)[sigName]
	if !ok {
		return nil, false
	}

	switch peers := raw.(type) {
	case map[string]any:
		if len(peers) > maxGroups {
			return nil, false
		}
		out := make(map[string][]string, len(peers))
		for group, v := range peers {
			arr, ok := v.([]any)
			if !ok {
				return nil, false
			}
			if len(arr) > maxURIsPerGroup {
				return nil, false
			}
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok {
					return nil, false
				}
				strs = append(strs, s)
			}
			out[group] = strs
		}
		return out, true
	case map[string][]string:
		return clonePeers(peers), true
	default:
		return nil, false
	}
}

// Obj — peering URIs grouped by network.
type Obj struct {
	peers map[string][]string
}

// New creates the "public" sigil. Max 8 groups, max 16 URIs per group.
func New(peers map[string][]string) (*Obj, error) {
	if err := validatePeers(peers); err != nil {
		return nil, err
	}
	return &Obj{peers: clonePeers(peers)}, nil
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

	if peers, ok := parsePeers(parsed); ok {
		if err := validatePeers(peers); err == nil {
			o.peers = clonePeers(peers)
		}
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Clone() sigils.Interface {
	return &Obj{peers: clonePeers(o.peers)}
}

func (o *Obj) Params() map[string]any {
	if len(o.peers) == 0 {
		return map[string]any{}
	}
	// Deep-copy the nested map/slices so the returned fragment cannot alias internal state.
	return map[string]any{sigName: clonePeers(o.peers)}
}
