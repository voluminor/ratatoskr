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

const (
	maxContactGroups    = 8
	maxContactsPerGroup = 8
)

var (
	reSigilInfoName     = regexp.MustCompile(`^[a-z0-9._-]{4,64}$`)
	reSigilInfoType     = regexp.MustCompile(`^[a-z0-9.-]{2,32}$`)
	reSigilInfoText     = regexp.MustCompile(`^\S[\S ]{0,512}\S$`)
	reSigilInfoContacts = regexp.MustCompile(`^\S[\S ]{1,256}\S$`)
)

// //

// ConfigSigilInfoObj holds the node's public identity card.
type ConfigSigilInfoObj struct {
	// Name is the FQDN or friendly name of the node (e.g. "home.y.example.net").
	// Required. Lowercase, 4–64 chars.
	Name string
	// Type is the device/role label (e.g. "server", "laptop", "router").
	// Required. Lowercase, 2–32 chars.
	Type string
	// Location is a free-text physical location (e.g. "Gravelines, France").
	// Optional. 2–514 chars if set.
	Location string
	// Contacts groups contact addresses by category.
	// Key is the group name (e.g. "email", "xmpp"), value is a list of addresses.
	// Optional. Max 8 groups, max 8 entries per group.
	Contacts map[string][]string
	// Peerings is a free-text peering policy (e.g. "open", "ask me").
	// Optional. 2–514 chars if set.
	Peerings string
}

type SigilInfoObj struct {
	conf *ConfigSigilInfoObj
}

// NewSigilInfo creates the "info" sigil — node identity card.
// Name and Type are required; Location, Contacts and Peerings are optional.
func NewSigilInfo(conf ConfigSigilInfoObj) (*SigilInfoObj, error) {
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
	if conf.Peerings != "" && !reSigilInfoText.MatchString(conf.Peerings) {
		return nil, errors.New("invalid peering")
	}

	if len(conf.Contacts) > maxContactGroups {
		return nil, fmt.Errorf("too many contact groups: %d (max %d)", len(conf.Contacts), maxContactGroups)
	}

	for group, contacts := range conf.Contacts {
		if !reSigilInfoType.MatchString(group) {
			return nil, fmt.Errorf("invalid contact name: %s", group)
		}
		if len(contacts) == 0 {
			return nil, fmt.Errorf("empty contact group: %s", group)
		}
		if len(contacts) > maxContactsPerGroup {
			return nil, fmt.Errorf("too many contacts in group %s: %d (max %d)", group, len(contacts), maxContactsPerGroup)
		}
		for pos, contact := range contacts {
			if !reSigilInfoContacts.MatchString(contact) {
				return nil, fmt.Errorf("invalid contact (%s)[%d]", group, pos)
			}
		}
	}

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

// //

func (sg *SigilInfoObj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+len(keysSigilInfo))
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	pairs := []struct {
		key string
		val any
	}{
		{"name", sg.conf.Name},
		{"type", sg.conf.Type},
		{"location", sg.conf.Location},
		{"contact", sg.conf.Contacts},
		{"peering", sg.conf.Peerings},
	}

	for _, p := range pairs {
		switch v := p.val.(type) {
		case string:
			if v == "" {
				continue
			}
		case map[string][]string:
			if len(v) == 0 {
				continue
			}
		}

		if _, ok := bufMap[p.key]; ok {
			return nil, fmt.Errorf("conflict key: %s", p.key)
		}
		bufMap[p.key] = p.val
	}

	return bufMap, nil
}

func (sg *SigilInfoObj) ParseParams(NodeInfo map[string]any) map[string]any {
	bufMap := make(map[string]any)
	for _, key := range keysSigilInfo {
		if data, ok := NodeInfo[key]; ok {
			bufMap[key] = data
		}
	}
	return bufMap
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
			m, ok := data.(map[string]any)
			if !ok {
				return false
			}
			for _, v := range m {
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
		}
	}
	return true
}
