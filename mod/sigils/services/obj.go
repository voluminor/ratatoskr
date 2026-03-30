package services

import (
	"errors"
	"fmt"

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

// Obj — ports open on this node inside Yggdrasil.
type Obj struct {
	services map[string]uint16
}

// New creates the "services" sigil. Max 32, ports 1–65535.
func New(services map[string]uint16) (*Obj, error) {
	if err := validateServices(services); err != nil {
		return nil, err
	}
	return &Obj{services: services}, nil
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

	if raw, ok := parsed[sigName].(map[string]any); ok {
		svc := make(map[string]uint16, len(raw))
		for name, v := range raw {
			if port, ok := v.(float64); ok && port > 0 && port <= 65535 && port == float64(int(port)) {
				svc[name] = uint16(port)
			}
		}
		o.services = svc
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Clone() sigils.Interface {
	svc := make(map[string]uint16, len(o.services))
	for k, v := range o.services {
		svc[k] = v
	}
	return &Obj{services: svc}
}

func (o *Obj) Params() map[string]any {
	if len(o.services) == 0 {
		return map[string]any{}
	}
	return map[string]any{sigName: o.services}
}
