package info

import (
	"errors"
	"fmt"

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
	if conf.Description != "" && !reText.MatchString(conf.Description) {
		return errors.New("invalid description")
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
	return sigils.MergeParams(NodeInfo, o.Params())
}

func (o *Obj) ParseParams(NodeInfo map[string]any) map[string]any {
	parsed := ParseParams(NodeInfo)

	conf := ConfigObj{}
	if v, ok := parsed[keyName].(string); ok {
		conf.Name = v
	}
	if v, ok := parsed[keyType].(string); ok {
		conf.Type = v
	}
	if v, ok := parsed[keyLocation].(string); ok {
		conf.Location = v
	}
	if v, ok := parsed[keyDescription].(string); ok {
		conf.Description = v
	}
	if raw, ok := parsed[keyContact].(map[string]any); ok {
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

func (o *Obj) Clone() sigils.Interface {
	conf := *o.conf
	if o.conf.Contacts != nil {
		conf.Contacts = make(map[string][]string, len(o.conf.Contacts))
		for k, v := range o.conf.Contacts {
			conf.Contacts[k] = append([]string(nil), v...)
		}
	}
	return &Obj{conf: &conf}
}

func (o *Obj) Params() map[string]any {
	result := make(map[string]any)

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
		result[keyContact] = o.conf.Contacts
	}
	if o.conf.Description != "" {
		result[keyDescription] = o.conf.Description
	}

	return result
}
