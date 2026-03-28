package settings_test

import (
	"encoding/hex"
	"testing"

	msettings "github.com/voluminor/ratatoskr/mod/settings"
	gsettings "github.com/voluminor/ratatoskr/target/settings"
)

// // // // // // // // // //

func populatedObj() *gsettings.Obj {
	obj := gsettings.NewDefault()
	y := &obj.Yggdrasil

	y.Key.Text = hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	y.Key.Path = "/tmp/key.pem"
	y.Listen = []string{"tls://0.0.0.0:0"}
	y.Peers.Url = []string{"tcp://1.2.3.4:5678"}
	y.Peers.Interface = map[string][]string{"eth0": {"tcp://10.0.0.1:9001"}}
	y.Peers.Manager.Enable = false
	y.AllowedPublicKeys = []string{"aabbccdd"}
	y.AdminListen = "unix:///tmp/ygg.sock"
	y.If.Name = "ygg0"
	y.If.Mtu = 1500
	y.Node.Info = map[string]any{"name": "test-node"}
	y.Node.Privacy = true
	y.LogLookups = true

	y.Multicast.Regex = "eth.*"
	y.Multicast.Beacon = true
	y.Multicast.Listen = false
	y.Multicast.Port = 9001
	y.Multicast.Priority = 5
	y.Multicast.Password = "secret"

	return obj
}

func ygg(obj *gsettings.Obj) msettings.YggdrasilInterface {
	return &obj.Yggdrasil
}

// //

func TestNodeConfig_BasicFields(t *testing.T) {
	obj := populatedObj()
	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}

	if cfg.PrivateKeyPath != "/tmp/key.pem" {
		t.Fatalf("PrivateKeyPath: got %q", cfg.PrivateKeyPath)
	}
	if len(cfg.Listen) != 1 || cfg.Listen[0] != "tls://0.0.0.0:0" {
		t.Fatalf("Listen: got %v", cfg.Listen)
	}
	if cfg.AdminListen != "unix:///tmp/ygg.sock" {
		t.Fatalf("AdminListen: got %q", cfg.AdminListen)
	}
	if cfg.IfName != "ygg0" {
		t.Fatalf("IfName: got %q", cfg.IfName)
	}
	if cfg.IfMTU != 1500 {
		t.Fatalf("IfMTU: got %d", cfg.IfMTU)
	}
	if !cfg.NodeInfoPrivacy {
		t.Fatal("NodeInfoPrivacy should be true")
	}
	if !cfg.LogLookups {
		t.Fatal("LogLookups should be true")
	}
}

func TestNodeConfig_PrivateKeyHexDecode(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Key.Text = hex.EncodeToString([]byte("testkey1"))

	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.PrivateKey) != "testkey1" {
		t.Fatalf("PrivateKey: got %q", cfg.PrivateKey)
	}
}

func TestNodeConfig_InvalidHex(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Key.Text = "not-valid-hex!"

	_, err := msettings.NodeConfig(ygg(obj))
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
}

func TestNodeConfig_EmptyKey(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Key.Text = ""

	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrivateKey != nil {
		t.Fatalf("PrivateKey should be nil for empty key.text, got %v", cfg.PrivateKey)
	}
}

// //

func TestNodeConfig_PeerManagerEnabled(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Peers.Url = []string{"tcp://1.2.3.4:5678"}
	obj.Yggdrasil.Peers.Manager.Enable = true

	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Peers != nil {
		t.Fatalf("Peers should be nil when manager is enabled, got %v", cfg.Peers)
	}
}

func TestNodeConfig_PeerManagerDisabled(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Peers.Url = []string{"tcp://1.2.3.4:5678"}
	obj.Yggdrasil.Peers.Manager.Enable = false

	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Peers) != 1 || cfg.Peers[0] != "tcp://1.2.3.4:5678" {
		t.Fatalf("Peers should pass through when manager disabled, got %v", cfg.Peers)
	}
}

// //

func TestNodeConfig_Multicast(t *testing.T) {
	obj := populatedObj()
	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.MulticastInterfaces) != 1 {
		t.Fatalf("expected 1 multicast interface, got %d", len(cfg.MulticastInterfaces))
	}
	mc := cfg.MulticastInterfaces[0]
	if mc.Regex != "eth.*" {
		t.Fatalf("Regex: got %q", mc.Regex)
	}
	if !mc.Beacon {
		t.Fatal("Beacon should be true")
	}
	if mc.Listen {
		t.Fatal("Listen should be false")
	}
	if mc.Port != 9001 {
		t.Fatalf("Port: got %d", mc.Port)
	}
	if mc.Priority != 5 {
		t.Fatalf("Priority: got %d", mc.Priority)
	}
	if mc.Password != "secret" {
		t.Fatalf("Password: got %q", mc.Password)
	}
}

func TestNodeConfig_NoMulticast(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Multicast.Regex = ""

	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MulticastInterfaces) != 0 {
		t.Fatalf("expected no multicast interfaces, got %d", len(cfg.MulticastInterfaces))
	}
}

// //

func TestNodeConfig_NilInterfacePeers(t *testing.T) {
	obj := gsettings.NewDefault()
	obj.Yggdrasil.Peers.Interface = nil

	cfg, err := msettings.NodeConfig(ygg(obj))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InterfacePeers == nil {
		t.Fatal("InterfacePeers should be initialized to empty map, not nil")
	}
}

// //

func TestFromNodeConfig_RoundTrip(t *testing.T) {
	src := populatedObj()
	cfg, err := msettings.NodeConfig(ygg(src))
	if err != nil {
		t.Fatal(err)
	}

	dst := msettings.FromNodeConfig(cfg, gsettings.NewDefault())
	dstObj := msettings.Obj(dst)
	y := &dstObj.Yggdrasil

	if y.Key.Text != src.Yggdrasil.Key.Text {
		t.Fatalf("Key.Text: got %q, want %q", y.Key.Text, src.Yggdrasil.Key.Text)
	}
	if y.Key.Path != src.Yggdrasil.Key.Path {
		t.Fatalf("Key.Path: got %q, want %q", y.Key.Path, src.Yggdrasil.Key.Path)
	}
	if y.AdminListen != src.Yggdrasil.AdminListen {
		t.Fatalf("AdminListen: got %q, want %q", y.AdminListen, src.Yggdrasil.AdminListen)
	}
	if y.If.Name != src.Yggdrasil.If.Name {
		t.Fatalf("If.Name: got %q, want %q", y.If.Name, src.Yggdrasil.If.Name)
	}
	if y.If.Mtu != src.Yggdrasil.If.Mtu {
		t.Fatalf("If.Mtu: got %d, want %d", y.If.Mtu, src.Yggdrasil.If.Mtu)
	}
	if y.Node.Privacy != src.Yggdrasil.Node.Privacy {
		t.Fatalf("Node.Privacy: got %v, want %v", y.Node.Privacy, src.Yggdrasil.Node.Privacy)
	}
	if y.LogLookups != src.Yggdrasil.LogLookups {
		t.Fatalf("LogLookups: got %v, want %v", y.LogLookups, src.Yggdrasil.LogLookups)
	}
	if y.Multicast.Regex != src.Yggdrasil.Multicast.Regex {
		t.Fatalf("Multicast.Regex: got %q, want %q", y.Multicast.Regex, src.Yggdrasil.Multicast.Regex)
	}
	if y.Multicast.Port != src.Yggdrasil.Multicast.Port {
		t.Fatalf("Multicast.Port: got %d, want %d", y.Multicast.Port, src.Yggdrasil.Multicast.Port)
	}
}

func TestFromNodeConfig_DoesNotMutateBase(t *testing.T) {
	base := gsettings.NewDefault()
	base.Log.MaxSize = 99

	cfg, _ := msettings.NodeConfig(ygg(populatedObj()))
	_ = msettings.FromNodeConfig(cfg, base)

	if base.Yggdrasil.AdminListen != gsettings.NewDefault().Yggdrasil.AdminListen {
		t.Fatal("FromNodeConfig mutated the base object")
	}
	if base.Log.MaxSize != 99 {
		t.Fatal("FromNodeConfig mutated non-yggdrasil fields on base")
	}
}

// //

func TestObj_CastFromInterface(t *testing.T) {
	src := gsettings.NewDefault()
	src.Config = "test.json"

	obj := msettings.Obj(src)
	if obj.Config != "test.json" {
		t.Fatalf("Obj() cast failed: got Config=%q", obj.Config)
	}
}
