package ninfo

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/voluminor/ratatoskr/mod/sigils"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

type noopLoggerObj struct{}

var _ yggcore.Logger = noopLoggerObj{}

func (noopLoggerObj) Printf(string, ...interface{}) {}
func (noopLoggerObj) Println(...interface{})        {}
func (noopLoggerObj) Infof(string, ...interface{})  {}
func (noopLoggerObj) Infoln(...interface{})         {}
func (noopLoggerObj) Warnf(string, ...interface{})  {}
func (noopLoggerObj) Warnln(...interface{})         {}
func (noopLoggerObj) Errorf(string, ...interface{}) {}
func (noopLoggerObj) Errorln(...interface{})        {}
func (noopLoggerObj) Debugf(string, ...interface{}) {}
func (noopLoggerObj) Debugln(...interface{})        {}
func (noopLoggerObj) Traceln(...interface{})        {}

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

// //

func newMockSigil(name string, keys ...string) *mockSigilObj {
	data := make(map[string]any, len(keys))
	for _, k := range keys {
		data[k] = "test"
	}
	return &mockSigilObj{name: name, params: keys, data: data}
}

// //

func genKey(t testing.TB) ed25519.PublicKey {
	t.Helper()
	pk, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}
