package sigil_core

import (
	"github.com/voluminor/ratatoskr/mod/sigils"
)

// // // // // // // // // //

type mockSigilObj struct {
	name   string
	params []string
	data   map[string]any
}

var _ sigils.Interface = &mockSigilObj{}

func (m *mockSigilObj) GetName() string        { return m.name }
func (m *mockSigilObj) GetParams() []string    { return m.params }
func (m *mockSigilObj) Params() map[string]any { return m.data }
func (m *mockSigilObj) Match(mp map[string]any) bool {
	for _, k := range m.params {
		if _, ok := mp[k]; !ok {
			return false
		}
	}
	return true
}

func (m *mockSigilObj) SetParams(mp map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(mp)+len(m.data))
	for k, v := range mp {
		out[k] = v
	}
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

func (m *mockSigilObj) ParseParams(mp map[string]any) map[string]any {
	out := make(map[string]any)
	for _, k := range m.params {
		if v, ok := mp[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (m *mockSigilObj) Clone() sigils.Interface {
	data := make(map[string]any, len(m.data))
	for k, v := range m.data {
		data[k] = v
	}
	return &mockSigilObj{name: m.name, params: append([]string(nil), m.params...), data: data}
}

// //

func newMockSigil(name string, keys ...string) *mockSigilObj {
	data := make(map[string]any, len(keys))
	for _, k := range keys {
		data[k] = "test"
	}
	return &mockSigilObj{name: name, params: keys, data: data}
}
