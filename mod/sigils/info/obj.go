package info

import (
	"errors"
	"fmt"
)

// // // // // // // // // //

// ConfigObj holds the node's public identity card.
type ConfigObj struct {
	Name     string              // FQDN or friendly name, required, 4–64 chars
	Type     string              // device/role label, required, 2–32 chars
	Location string              // free-text physical location, optional
	Contacts map[string][]string // grouped contact addresses, optional, max 8×8
	Peerings string              // free-text peering policy, optional
}

func validateConfig(conf *ConfigObj) error {
	if conf.Name == "" {
		return errors.New("missing name")
	}
	if conf.Type == "" {
		return errors.New("missing type")
	}

	if !reName.MatchString(conf.Name) {
		return errors.New("invalid name")
	}
	if !reType.MatchString(conf.Type) {
		return errors.New("invalid type")
	}
	if conf.Peerings != "" && !reText.MatchString(conf.Peerings) {
		return errors.New("invalid peering")
	}

	if len(conf.Contacts) > maxContactGroups {
		return fmt.Errorf("too many contact groups: %d (max %d)", len(conf.Contacts), maxContactGroups)
	}

	for group, contacts := range conf.Contacts {
		if !reType.MatchString(group) {
			return fmt.Errorf("invalid contact name: %s", group)
		}
		if len(contacts) == 0 {
			return fmt.Errorf("empty contact group: %s", group)
		}
		if len(contacts) > maxContactsPerGroup {
			return fmt.Errorf("too many contacts in group %s: %d (max %d)", group, len(contacts), maxContactsPerGroup)
		}
		for pos, contact := range contacts {
			if !reContacts.MatchString(contact) {
				return fmt.Errorf("invalid contact (%s)[%d]", group, pos)
			}
		}
	}

	return nil
}

// //

type Obj struct {
	conf *ConfigObj
}

// New creates the "info" sigil — node identity card.
func New(conf ConfigObj) (*Obj, error) {
	if err := validateConfig(&conf); err != nil {
		return nil, err
	}
	return &Obj{conf: &conf}, nil
}

// //

func (o *Obj) GetName() string {
	return Name()
}

func (o *Obj) GetParams() []string {
	return Keys()
}

func (o *Obj) SetParams(NodeInfo map[string]any) (map[string]any, error) {
	bufMap := make(map[string]any, len(NodeInfo)+len(sigKeys))
	for k, v := range NodeInfo {
		bufMap[k] = v
	}

	pairs := []struct {
		key string
		val any
	}{
		{"name", o.conf.Name},
		{"type", o.conf.Type},
		{"location", o.conf.Location},
		{"contact", o.conf.Contacts},
		{"peering", o.conf.Peerings},
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

func (o *Obj) ParseParams(NodeInfo map[string]any) map[string]any {
	parsed := ParseParams(NodeInfo)

	conf := ConfigObj{}
	if v, ok := parsed["name"].(string); ok {
		conf.Name = v
	}
	if v, ok := parsed["type"].(string); ok {
		conf.Type = v
	}
	if v, ok := parsed["location"].(string); ok {
		conf.Location = v
	}
	if v, ok := parsed["peering"].(string); ok {
		conf.Peerings = v
	}
	if raw, ok := parsed["contact"].(map[string]any); ok {
		conf.Contacts = make(map[string][]string, len(raw))
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
			conf.Contacts[group] = strs
		}
	}
	o.conf = &conf

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Params() map[string]any {
	result := make(map[string]any)

	if o.conf.Name != "" {
		result["name"] = o.conf.Name
	}
	if o.conf.Type != "" {
		result["type"] = o.conf.Type
	}
	if o.conf.Location != "" {
		result["location"] = o.conf.Location
	}
	if len(o.conf.Contacts) > 0 {
		result["contact"] = o.conf.Contacts
	}
	if o.conf.Peerings != "" {
		result["peering"] = o.conf.Peerings
	}

	return result
}
