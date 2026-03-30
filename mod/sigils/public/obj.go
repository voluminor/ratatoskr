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

// Obj — peering URIs grouped by network.
type Obj struct {
	peers map[string][]string
}

// New creates the "public" sigil. Max 8 groups, max 16 URIs per group.
func New(peers map[string][]string) (*Obj, error) {
	if err := validatePeers(peers); err != nil {
		return nil, err
	}
	return &Obj{peers: peers}, nil
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

	bufMap[sigName] = o.peers
	return bufMap, nil
}

func (o *Obj) ParseParams(NodeInfo map[string]any) map[string]any {
	parsed := ParseParams(NodeInfo)

	if raw, ok := parsed[sigName].(map[string]any); ok {
		peers := make(map[string][]string, len(raw))
		for group, v := range raw {
			arr, ok := v.([]any)
			if !ok {
				continue
			}
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					strs = append(strs, s)
				}
			}
			peers[group] = strs
		}
		o.peers = peers
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Clone() sigils.Interface {
	peers := make(map[string][]string, len(o.peers))
	for k, v := range o.peers {
		peers[k] = append([]string(nil), v...)
	}
	return &Obj{peers: peers}
}

func (o *Obj) Params() map[string]any {
	if len(o.peers) == 0 {
		return map[string]any{}
	}
	return map[string]any{sigName: o.peers}
}
