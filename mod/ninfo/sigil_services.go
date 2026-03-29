package ninfo

import (
	"errors"
	"fmt"
	"regexp"
)

// // // // // // // // // //

const nameSigilServices = "services"

var keysSigilServices = []string{nameSigilServices}

const maxServices = 32

var reSigilServiceName = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)

// //

type SigilServicesObj struct {
	services map[string]uint16
}

// NewSigilServices creates the "services" sigil — ports open on this node inside Yggdrasil.
// services maps a service name (e.g. "http", "ssh") to its port number (1–65535).
// Max 32 services.
func NewSigilServices(services map[string]uint16) (*SigilServicesObj, error) {
	if len(services) == 0 {
		return nil, errors.New("empty services")
	}
	if len(services) > maxServices {
		return nil, fmt.Errorf("too many services: %d (max %d)", len(services), maxServices)
	}

	for name, port := range services {
		if !reSigilServiceName.MatchString(name) {
			return nil, fmt.Errorf("invalid service name: %s", name)
		}
		if port == 0 {
			return nil, fmt.Errorf("invalid port for %s: 0", name)
		}
	}

	sg := new(SigilServicesObj)
	sg.services = services
	return sg, nil
}

// //

func (sg *SigilServicesObj) GetName() string {
	return nameSigilServices
}

func (sg *SigilServicesObj) GetParams() []string {
	return keysSigilServices
}

// //

func (sg *SigilServicesObj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+1)
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	if _, ok := bufMap[nameSigilServices]; ok {
		return nil, fmt.Errorf("conflict key: %s", nameSigilServices)
	}

	bufMap[nameSigilServices] = sg.services
	return bufMap, nil
}

func (sg *SigilServicesObj) ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[nameSigilServices]; ok {
		bufMap[nameSigilServices] = data
	}
	return bufMap
}

func (sg *SigilServicesObj) Match(NodeInfo map[string]any) bool {
	raw, ok := NodeInfo[nameSigilServices]
	if !ok {
		return false
	}

	svc, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if len(svc) == 0 {
		return false
	}

	for name, v := range svc {
		if !reSigilServiceName.MatchString(name) {
			return false
		}
		port, ok := v.(float64)
		if !ok || port <= 0 || port > 65535 || port != float64(int(port)) {
			return false
		}
	}
	return true
}
