package services

import (
	"errors"
	"fmt"
	"math"

	"github.com/voluminor/ratatoskr/mod/sigils"
)

// // // // // // // // // //

func validateServices(services map[string]uint16) error {
	if len(services) == 0 {
		return errors.New("empty services")
	}
	if len(services) > maxServices {
		return fmt.Errorf("too many services: %d (max %d)", len(services), maxServices)
	}

	for name, port := range services {
		if !reServiceName.MatchString(name) {
			return fmt.Errorf("invalid service name: %s", name)
		}
		if port == 0 {
			return fmt.Errorf("invalid port for %s: 0", name)
		}
	}

	return nil
}

func cloneServices(src map[string]uint16) map[string]uint16 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]uint16, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func parsePort(v any) (uint16, bool) {
	switch port := v.(type) {
	case float64:
		if math.IsNaN(port) || math.IsInf(port, 0) || port <= 0 || port > 65535 || port != float64(int(port)) {
			return 0, false
		}
		return uint16(port), true
	case uint16:
		if port == 0 {
			return 0, false
		}
		return port, true
	default:
		return 0, false
	}
}

func parseServices(NodeInfo map[string]any) (map[string]uint16, bool) {
	raw, ok := ParseParams(NodeInfo)[sigName]
	if !ok {
		return nil, false
	}

	switch svc := raw.(type) {
	case map[string]any:
		if len(svc) > maxServices {
			return nil, false
		}
		out := make(map[string]uint16, len(svc))
		for name, v := range svc {
			port, ok := parsePort(v)
			if !ok {
				return nil, false
			}
			out[name] = port
		}
		return out, true
	case map[string]uint16:
		return cloneServices(svc), true
	default:
		return nil, false
	}
}

// Obj — ports open on this node inside Yggdrasil.
type Obj struct {
	services map[string]uint16
}

// New creates the "services" sigil. Max 256, ports 1–65535.
func New(services map[string]uint16) (*Obj, error) {
	if err := validateServices(services); err != nil {
		return nil, err
	}
	return &Obj{services: cloneServices(services)}, nil
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

	if svc, ok := parseServices(parsed); ok {
		if err := validateServices(svc); err == nil {
			o.services = cloneServices(svc)
		}
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Clone() sigils.Interface {
	return &Obj{services: cloneServices(o.services)}
}

func (o *Obj) Params() map[string]any {
	if len(o.services) == 0 {
		return map[string]any{}
	}
	// Deep-copy the nested map so the returned fragment cannot alias internal state.
	return map[string]any{sigName: cloneServices(o.services)}
}
