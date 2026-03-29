package ninfo

import (
	"errors"
	"fmt"
	"regexp"
)

// // // // // // // // // //

const nameSigilInet = "inet"

var keysSigilInet = []string{nameSigilInet}

var reSigilInetAddr = regexp.MustCompile(`^[a-zA-Z0-9._:/-]{4,256}$`)

const maxInetAddrs = 32

// //

type ConfigSigilInetObj struct {
	Addrs []string
}

type SigilInetObj struct {
	conf *ConfigSigilInetObj
}

func NewSigilInet(conf ConfigSigilInetObj) (*SigilInetObj, error) {
	if len(conf.Addrs) == 0 {
		return nil, errors.New("empty addrs")
	}
	if len(conf.Addrs) > maxInetAddrs {
		return nil, fmt.Errorf("too many addrs: %d (max %d)", len(conf.Addrs), maxInetAddrs)
	}

	seen := make(map[string]bool, len(conf.Addrs))
	for i, addr := range conf.Addrs {
		if !reSigilInetAddr.MatchString(addr) {
			return nil, fmt.Errorf("invalid addr [%d]: %s", i, addr)
		}
		if seen[addr] {
			return nil, fmt.Errorf("duplicate addr [%d]: %s", i, addr)
		}
		seen[addr] = true
	}

	sg := new(SigilInetObj)
	sg.conf = &conf
	return sg, nil
}

// //

func (sg *SigilInetObj) GetName() string {
	return nameSigilInet
}

func (sg *SigilInetObj) GetParams() []string {
	return keysSigilInet
}

//

func (sg *SigilInetObj) ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[nameSigilInet]; ok {
		bufMap[nameSigilInet] = data
	}
	return bufMap
}

func (sg *SigilInetObj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+1)
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	if _, ok := bufMap[nameSigilInet]; ok {
		return nil, fmt.Errorf("conflict key: %s", nameSigilInet)
	}

	bufMap[nameSigilInet] = sg.conf.Addrs
	return bufMap, nil
}

func (sg *SigilInetObj) Match(NodeInfo map[string]any) bool {
	raw, ok := NodeInfo[nameSigilInet]
	if !ok {
		return false
	}

	arr, ok := raw.([]any)
	if !ok {
		return false
	}
	if len(arr) == 0 {
		return false
	}

	for _, item := range arr {
		if _, ok = item.(string); !ok {
			return false
		}
	}
	return true
}
