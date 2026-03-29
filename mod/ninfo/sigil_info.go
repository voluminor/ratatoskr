package ninfo

import (
	"errors"
	"fmt"
	"regexp"
)

// // // // // // // // // //

const nameSigilInfo = "info"

var keysSigilInfo = []string{
	"name",
	"type",
	"location",
	"contact",
	"peering",
}

type ConfigSigilInfo struct {
	Name     string
	Type     string
	Location string
	Contacts map[string][]string
	Peerings string
}

var (
	reSigilInfoName     = regexp.MustCompile(`^[a-z0-9._-]{4,64}$`)
	reSigilInfoType     = regexp.MustCompile(`^[a-z0-9.-]{2,32}$`)
	reSigilInfoText     = regexp.MustCompile(`^\S[\S ]{0,512}\S$`)
	reSigilInfoContacts = regexp.MustCompile(`^\S[\S ]{1,256}\S$`)
)

// //

type SigilInfoObj struct {
	conf *ConfigSigilInfo
}

func NewSigilInfo(conf ConfigSigilInfo) (*SigilInfoObj, error) {
	if conf.Name == "" {
		return nil, errors.New("missing name")
	}
	if conf.Type == "" {
		return nil, errors.New("missing type")
	}

	if !reSigilInfoName.MatchString(conf.Name) {
		return nil, errors.New("invalid name")
	}
	if !reSigilInfoType.MatchString(conf.Type) {
		return nil, errors.New("invalid type")
	}
	if !reSigilInfoText.MatchString(conf.Peerings) {
		return nil, errors.New("invalid peering")
	}

	for group, contacts := range conf.Contacts {
		if !reSigilInfoType.MatchString(group) {
			return nil, fmt.Errorf("invalid contact name: %s", group)
		}
		for pos, contact := range contacts {
			if !reSigilInfoContacts.MatchString(contact) {
				return nil, fmt.Errorf("invalid contact (%s)[%d]", group, pos)
			}
		}
	}

	//

	sg := new(SigilInfoObj)
	sg.conf = &conf

	return sg, nil
}

// //

func (sg *SigilInfoObj) GetName() string {
	return nameSigilInfo
}

func (sg *SigilInfoObj) GetParams() []string {
	return keysSigilInfo
}

//

func (sg *SigilInfoObj) ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	for _, key := range keysSigilInfo {
		data, ok := NodeInfo[key]
		if ok {
			bufMap[key] = data
		}
	}
	return bufMap
}

func (sg *SigilInfoObj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+len(sg.GetParams()))
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	for _, key := range sg.GetParams() {
		if _, ok := bufMap[key]; ok {
			return nil, fmt.Errorf("conflict key: %s", key)
		}

		var data any
		switch key {
		case "name":
			data = sg.conf.Name
		case "type":
			data = sg.conf.Type
		case "location":
			data = sg.conf.Location
		case "contact":
			data = sg.conf.Contacts
		case "peering":
			data = sg.conf.Peerings
		default:
			return nil, fmt.Errorf("unknown param: %s", key)
		}
		bufMap[key] = data
	}

	return bufMap, nil
}

func (sg *SigilInfoObj) Match(NodeInfo map[string]any) bool {
	bufMap := sg.ParseParams(NodeInfo)
	if len(bufMap) < 2 {
		return false
	}
	if _, ok := bufMap["name"]; !ok {
		return false
	}
	if _, ok := bufMap["type"]; !ok {
		return false
	}

	for key, data := range bufMap {
		switch key {
		case "name", "type", "location", "peering":
			if _, ok := data.(string); !ok {
				return false
			}
		case "contact":
			if _, ok := data.(map[string][]string); !ok {
				return false
			}
		}
	}
	return true
}
