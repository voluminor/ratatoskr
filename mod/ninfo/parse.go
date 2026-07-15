package ninfo

import (
	"encoding/json"

	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/sigils/sigil_core"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

// ParsedObj holds the result of parsing foreign NodeInfo.
type ParsedObj struct {
	// Version is the parsed ratatoskr metadata version.
	Version string
	// Sigils contains metadata parsed by known sigils.
	Sigils map[string]sigils.Interface
	// SigilNames preserves valid metadata names that this build cannot parse.
	SigilNames []string
	// Extra contains unclaimed NodeInfo fields.
	Extra map[string]any
}

func (p *ParsedObj) sigilNames() []string {
	seen := make(map[string]struct{}, len(p.SigilNames)+len(p.Sigils))
	names := make([]string, 0, len(p.SigilNames)+len(p.Sigils))
	for _, name := range p.SigilNames {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for name := range p.Sigils {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// NodeInfo reassembles the parsed fields as Yggdrasil NodeInfo.
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
	if len(p.Version) > 0 {
		out[target.Name] = sigil_core.CompileInfoNames(p.sigilNames(), p.Version)
	}
	return out
}

// String returns the parsed data as a JSON string.
func (p *ParsedObj) String() string {
	b, _ := json.Marshal(p.NodeInfo())
	return string(b)
}

// //

// Parse separates known sigils from unclaimed Yggdrasil NodeInfo fields.
func Parse(nodeInfo map[string]any, sg ...sigils.Interface) *ParsedObj {
	result := &ParsedObj{
		Extra: make(map[string]any, len(nodeInfo)),
	}

	for k, v := range nodeInfo {
		result.Extra[k] = v
	}

	raw, ok := result.Extra[target.Name]
	if !ok {
		return result
	}
	str, ok := raw.(string)
	if !ok {
		return result
	}

	ver, sigilsArr, err := sigil_core.ParseInfo(str)
	if err != nil {
		return result
	}

	result.Version = ver
	result.SigilNames = append([]string(nil), sigilsArr...)
	delete(result.Extra, target.Name)

	userParsers := make(map[string]func(map[string]any) (sigils.Interface, error), len(sg))
	for _, s := range sg {
		if s == nil {
			continue
		}
		name := s.GetName()
		if reservedSigilName(name) {
			continue
		}
		userParsers[name] = wrapUserSigil(s)
	}

	for _, name := range sigilsArr {
		fn, ok := target.Parse(name)
		if !ok {
			fn, ok = userParsers[name]
		}
		if !ok {
			continue
		}

		parsed, err := fn(nodeInfo)
		if err != nil || parsed == nil {
			continue
		}

		if result.Sigils == nil {
			result.Sigils = make(map[string]sigils.Interface, len(sigilsArr))
		}
		result.Sigils[name] = parsed

		for _, key := range parsed.GetParams() {
			delete(result.Extra, key)
		}
	}

	return result
}

func wrapUserSigil(s sigils.Interface) func(map[string]any) (sigils.Interface, error) {
	return func(m map[string]any) (sigils.Interface, error) {
		c := s.Clone()
		if c == nil {
			return nil, nil
		}
		if !c.Match(m) {
			return nil, nil
		}
		c.ParseParams(m)
		return c, nil
	}
}
