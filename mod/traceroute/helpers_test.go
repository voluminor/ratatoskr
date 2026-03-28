package traceroute

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

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

// //

func genKey(t testing.TB) ed25519.PublicKey {
	t.Helper()
	pk, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func genKeyN(t testing.TB, n int) []ed25519.PublicKey {
	t.Helper()
	keys := make([]ed25519.PublicKey, n)
	for i := range keys {
		keys[i] = genKey(t)
	}
	return keys
}

// buildTestTree creates:
//
//	root(0) -> c1(1) -> gc1(3), gc2(4)
//	        -> c2(2)
func buildTestTree(t testing.TB) (*NodeObj, []ed25519.PublicKey) {
	t.Helper()
	keys := genKeyN(t, 5)
	root := &NodeObj{Key: keys[0], Depth: 0}
	c1 := &NodeObj{Key: keys[1], Parent: keys[0], Depth: 1}
	c2 := &NodeObj{Key: keys[2], Parent: keys[0], Depth: 1}
	gc1 := &NodeObj{Key: keys[3], Parent: keys[1], Depth: 2}
	gc2 := &NodeObj{Key: keys[4], Parent: keys[1], Depth: 2}
	root.Children = []*NodeObj{c1, c2}
	c1.Children = []*NodeObj{gc1, gc2}
	return root, keys
}
