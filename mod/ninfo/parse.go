package ninfo

import (
	"encoding/json"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// ParsedObj holds the result of parsing foreign NodeInfo.
type ParsedObj struct {
	Info   *RatatoskrInfoObj
	Sigils map[string]sigils.Interface
	Extra  map[string]any
}

// NodeInfo reassembles the parsed data back into a map[string]any
// suitable for yggdrasil NodeInfo.
func (p *ParsedObj) NodeInfo() map[string]any {
	out := make(map[string]any, len(p.Extra)+len(p.Sigils)+1)
	for k, v := range p.Extra {
		out[k] = v
	}
	for _, sg := range p.Sigils {
		for k, v := range sg.Params() {
			out[k] = v
		}
	}
	if p.Info != nil {
		out[target.GlobalName] = p.Info.String()
	}
	return out
}

// String returns the parsed data as a JSON string.
func (p *ParsedObj) String() string {
	b, _ := json.Marshal(p.NodeInfo())
	return string(b)
}

// //

// Parse inspects arbitrary NodeInfo from yggdrasil.
// If the map contains a valid ratatoskr metadata key, sigils are extracted
// using built-in parsers from target.GlobalSigilParseMap merged with
// user-provided sg (user sigils override built-in on name collision).
// Parsed keys are removed from the remaining map returned in Extra.
func Parse(nodeInfo map[string]any, sg ...sigils.Interface) *ParsedObj {
	result := &ParsedObj{
		Extra: make(map[string]any, len(nodeInfo)),
	}

	for k, v := range nodeInfo {
		result.Extra[k] = v
	}

	raw, ok := result.Extra[target.GlobalName]
	if !ok {
		return result
	}
	str, ok := raw.(string)
	if !ok {
		return result
	}

	ri, err := ParseRatatoskrInfo(str)
	if err != nil {
		return result
	}

	result.Info = ri
	delete(result.Extra, target.GlobalName)

	// //

	parsers := make(map[string]func(map[string]any) (sigils.Interface, error), len(target.GlobalSigilParseMap))
	for name, fn := range target.GlobalSigilParseMap {
		parsers[name] = fn
	}

	userKeys := make(map[string]func() []string, len(sg))
	for _, s := range sg {
		name := s.GetName()
		parsers[name] = wrapUserSigil(s)
		userKeys[name] = s.GetParams
	}

	// //

	for _, name := range ri.Sigils {
		fn, ok := parsers[name]
		if !ok {
			continue
		}

		parsed, err := fn(nodeInfo)
		if err != nil || parsed == nil {
			continue
		}

		if result.Sigils == nil {
			result.Sigils = make(map[string]sigils.Interface, len(ri.Sigils))
		}
		result.Sigils[name] = parsed

		keys := parsed.GetParams
		if ukFn, ok := userKeys[name]; ok {
			keys = ukFn
		}
		for _, key := range keys() {
			delete(result.Extra, key)
		}
	}

	return result
}

// //

func wrapUserSigil(s sigils.Interface) func(map[string]any) (sigils.Interface, error) {
	return func(m map[string]any) (sigils.Interface, error) {
		if !s.Match(m) {
			return nil, nil
		}
		c := s.Clone()
		_, err := c.SetParams(c.ParseParams(m))
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}
