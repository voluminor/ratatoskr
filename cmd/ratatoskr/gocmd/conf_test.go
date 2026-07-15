package gocmd

import (
	"bytes"
	"testing"

	yggconfig "github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// // // // // // // // // //

func TestRuntimeNodeConfigRoundTrip(t *testing.T) {
	want := yggconfig.GenerateConfig()
	want.AdminListen = "none"
	want.Peers = []string{"tls://example:1234"}
	want.IfName = "none"
	want.IfMTU = 4096
	runtimeCfg := runtimeConfigFromNode(want)
	got, err := nodeConfigFromRuntime(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.PrivateKey) != string(want.PrivateKey) || got.AdminListen != want.AdminListen || got.IfMTU != want.IfMTU {
		t.Fatalf("round trip mismatch: got=%+v want=%+v", got, want)
	}
	if len(got.Peers) != 1 || got.Peers[0] != want.Peers[0] {
		t.Fatalf("round trip peers = %v", got.Peers)
	}
}

func TestMarshalNodeConfigYAMLIsYAML(t *testing.T) {
	cfg := yggconfig.GenerateConfig()
	cfg.AdminListen = "none"
	data, err := marshalNodeConfig(cfg, ".yml")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		t.Fatalf("YAML export contains JSON: %.32q", data)
	}
	var decoded map[string]any
	if err = yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}
	if len(decoded) == 0 {
		t.Fatal("YAML export is empty")
	}
	privateKey, ok := decoded["PrivateKey"].(string)
	if !ok || len(privateKey) != 128 {
		t.Fatalf("YAML private key = %T(%v), want 128-character hex string", decoded["PrivateKey"], decoded["PrivateKey"])
	}
}
