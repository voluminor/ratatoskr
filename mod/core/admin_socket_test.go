package core

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/admin"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
)

// // // // // // // // // //

func TestAdminSocketPassThrough(t *testing.T) {
	cfg := config.GenerateConfig()
	cfg.AdminListen = "none"
	node, err := New(ConfigObj{Config: cfg, Logger: noopLoggerObj{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = node.Close() })

	socketPath := filepath.Join(t.TempDir(), "admin.sock")
	if err := node.EnableAdmin("unix://" + socketPath); err != nil {
		t.Fatalf("EnableAdmin: %v", err)
	}
	_, active := node.adminSocket.get()
	if !active {
		t.Fatal("admin socket is not active")
	}

	connection, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial admin: %v", err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set admin test deadline: %v", err)
	}
	if err := json.NewEncoder(connection).Encode(admin.AdminSocketRequest{Name: "list"}); err != nil {
		t.Fatalf("encode request: %v", err)
	}
	var response admin.AdminSocketResponse
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "success" {
		t.Fatalf("admin status = %q, error = %q", response.Status, response.Error)
	}
	var list admin.ListResponse
	if err := json.Unmarshal(response.Response, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	want := map[string]bool{
		"getself":             false,
		"getnodeinfo":         false,
		"debug_remotegettree": false,
	}
	for _, entry := range list.List {
		if _, exists := want[entry.Command]; exists {
			want[entry.Command] = true
		}
	}
	for command, found := range want {
		if !found {
			t.Errorf("list is missing %q", command)
		}
	}
	if err := node.DisableAdmin(); err != nil {
		t.Fatalf("DisableAdmin: %v", err)
	}
}
