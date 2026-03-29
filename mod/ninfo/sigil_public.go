package ninfo

import (
	"errors"
	"fmt"
	"regexp"
)

// // // // // // // // // //

const nameSigilPublic = "public"

var keysSigilPublic = []string{nameSigilPublic}

var (
	reSigilPublicGroup = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
	reSigilPublicURI   = regexp.MustCompile(`^[a-zA-Z0-9+._/:@\[\]-]{8,256}$`)
)

const maxPublicGroups = 8
const maxPublicURIsPerGroup = 16

// //

type ConfigSigilPublicObj struct {
	Peers map[string][]string
}

type SigilPublicObj struct {
	conf *ConfigSigilPublicObj
}

func NewSigilPublic(conf ConfigSigilPublicObj) (*SigilPublicObj, error) {
	if len(conf.Peers) == 0 {
		return nil, errors.New("empty peers")
	}
	if len(conf.Peers) > maxPublicGroups {
		return nil, fmt.Errorf("too many groups: %d (max %d)", len(conf.Peers), maxPublicGroups)
	}

	for group, uris := range conf.Peers {
		if !reSigilPublicGroup.MatchString(group) {
			return nil, fmt.Errorf("invalid group name: %s", group)
		}
		if len(uris) == 0 {
			return nil, fmt.Errorf("empty group: %s", group)
		}
		if len(uris) > maxPublicURIsPerGroup {
			return nil, fmt.Errorf("too many URIs in group %s: %d (max %d)", group, len(uris), maxPublicURIsPerGroup)
		}
		for i, uri := range uris {
			if !reSigilPublicURI.MatchString(uri) {
				return nil, fmt.Errorf("invalid URI (%s)[%d]", group, i)
			}
		}
	}

	sg := new(SigilPublicObj)
	sg.conf = &conf
	return sg, nil
}

// //

func (sg *SigilPublicObj) GetName() string {
	return nameSigilPublic
}

func (sg *SigilPublicObj) GetParams() []string {
	return keysSigilPublic
}

//

func (sg *SigilPublicObj) ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	if data, ok := NodeInfo[nameSigilPublic]; ok {
		bufMap[nameSigilPublic] = data
	}
	return bufMap
}

func (sg *SigilPublicObj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+1)
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	if _, ok := bufMap[nameSigilPublic]; ok {
		return nil, fmt.Errorf("conflict key: %s", nameSigilPublic)
	}

	bufMap[nameSigilPublic] = sg.conf.Peers
	return bufMap, nil
}

func (sg *SigilPublicObj) Match(NodeInfo map[string]any) bool {
	raw, ok := NodeInfo[nameSigilPublic]
	if !ok {
		return false
	}

	peers, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	if len(peers) == 0 {
		return false
	}

	for group, v := range peers {
		if !reSigilPublicGroup.MatchString(group) {
			return false
		}
		arr, ok := v.([]any)
		if !ok {
			return false
		}
		for _, item := range arr {
			if _, ok := item.(string); !ok {
				return false
			}
		}
	}
	return true
}
