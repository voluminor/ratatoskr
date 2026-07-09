package info

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/voluminor/ratatoskr/mod/sigils"
)

// // // // // // // // // //

// ConfigObj holds the node's public identity card.
type ConfigObj struct {
	Name        string              // FQDN or friendly name, required, 4–64 chars
	Type        string              // device/role label, required, 2–32 chars
	Location    string              // free-text physical location, optional
	Contacts    map[string][]string // grouped contact addresses, optional, max 8×8
	Description string              // free-text description, optional, e.g. peering policy
}

func validHumanText(s string, minRunes, maxRunes int) bool {
	n := utf8.RuneCountInString(s)
	if n < minRunes || n > maxRunes {
		return false
	}
	first := true
	var last rune
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
		if unicode.IsSpace(r) && r != ' ' {
			return false
		}
		if first && r == ' ' {
			return false
		}
		first = false
		last = r
	}
	return last != ' '
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
	if conf.Description != "" && !validHumanText(conf.Description, 2, 514) {
		return errors.New("invalid description")
	}
	if conf.Location != "" && !validHumanText(conf.Location, 2, 514) {
		return errors.New("invalid location")
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
			if !validHumanText(contact, 3, 258) {
				return fmt.Errorf("invalid contact (%s)[%d]", group, pos)
			}
		}
	}

	return nil
}

func cloneContacts(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for k, v := range src {
		dst[k] = append([]string(nil), v...)
	}
	return dst
}

func cloneConfig(conf *ConfigObj) ConfigObj {
	if conf == nil {
		return ConfigObj{}
	}
	out := *conf
	out.Contacts = cloneContacts(conf.Contacts)
	return out
}

func parseContactMap(raw any) (map[string][]string, bool) {
	switch m := raw.(type) {
	case map[string]any:
		if len(m) > maxContactGroups {
			return nil, false
		}
		contacts := make(map[string][]string, len(m))
		for group, v := range m {
			arr, ok := v.([]any)
			if !ok {
				return nil, false
			}
			if len(arr) > maxContactsPerGroup {
				return nil, false
			}
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok {
					return nil, false
				}
				strs = append(strs, s)
			}
			contacts[group] = strs
		}
		return contacts, true
	case map[string][]string:
		return cloneContacts(m), true
	default:
		return nil, false
	}
}

func parseConfig(NodeInfo map[string]any) (ConfigObj, bool) {
	parsed := ParseParams(NodeInfo)

	conf := ConfigObj{}
	name, ok := parsed[keyName].(string)
	if !ok {
		return ConfigObj{}, false
	}
	conf.Name = name
	typ, ok := parsed[keyType].(string)
	if !ok {
		return ConfigObj{}, false
	}
	conf.Type = typ

	if v, ok := parsed[keyLocation]; ok {
		s, ok := v.(string)
		if !ok {
			return ConfigObj{}, false
		}
		conf.Location = s
	}
	if v, ok := parsed[keyDescription]; ok {
		s, ok := v.(string)
		if !ok {
			return ConfigObj{}, false
		}
		conf.Description = s
	}
	if raw, ok := parsed[keyContact]; ok {
		contacts, ok := parseContactMap(raw)
		if !ok {
			return ConfigObj{}, false
		}
		conf.Contacts = contacts
	}

	return conf, true
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
	cloned := cloneConfig(&conf)
	return &Obj{conf: &cloned}, nil
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

	if conf, ok := parseConfig(parsed); ok {
		if obj, err := New(conf); err == nil {
			o.conf = obj.conf
		}
	}

	return parsed
}

func (o *Obj) Match(NodeInfo map[string]any) bool {
	return Match(NodeInfo)
}

func (o *Obj) Clone() sigils.Interface {
	conf := cloneConfig(o.conf)
	return &Obj{conf: &conf}
}

func (o *Obj) Params() map[string]any {
	result := make(map[string]any)
	if o.conf == nil {
		return result
	}

	if o.conf.Name != "" {
		result[keyName] = o.conf.Name
	}
	if o.conf.Type != "" {
		result[keyType] = o.conf.Type
	}
	if o.conf.Location != "" {
		result[keyLocation] = o.conf.Location
	}
	if len(o.conf.Contacts) > 0 {
		// Deep-copy the nested map/slices so the returned fragment cannot alias internal state.
		result[keyContact] = cloneContacts(o.conf.Contacts)
	}
	if o.conf.Description != "" {
		result[keyDescription] = o.conf.Description
	}

	return result
}
