package sigil_core

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/voluminor/ratatoskr/internal/common"
	"github.com/voluminor/ratatoskr/mod/sigils/info"
	"github.com/voluminor/ratatoskr/target"
)

// // // // // // // // // //

func TestNewOwnsInputsAndReturnsPartialResult(t *testing.T) {
	base := map[string]any{"custom": map[string]any{"value": "original"}}
	valid := newMockSigil("valid", "owned")
	obj, errs := New(base, valid, nil, newMockSigil("AB"))
	if len(errs) != 2 {
		t.Fatalf("New errors = %v, want nil and invalid-name errors", errs)
	}
	base["custom"].(map[string]any)["value"] = "changed"
	valid.data["owned"] = "changed"
	nodeInfo := obj.NodeInfo()
	if got := nodeInfo["custom"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("base NodeInfo alias changed result: %v", got)
	}
	if got := nodeInfo["owned"]; got != "test" {
		t.Fatalf("sigil alias changed result: %v", got)
	}
	if obj.Get("valid") == nil || obj.Get("AB") != nil || obj.LenSigils() != 1 {
		t.Fatalf("partial registry = %#v", obj.Sigils())
	}
	nodeInfo["custom"].(map[string]any)["value"] = "tampered"
	delete(nodeInfo, target.Name)
	if got := obj.NodeInfo()["custom"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("NodeInfo result retained alias: %v", got)
	}
	if _, exists := obj.NodeInfo()[target.Name]; !exists {
		t.Fatal("NodeInfo result exposed internal map")
	}
	if _, exists := base[target.Name]; exists {
		t.Fatal("New mutated the base map")
	}
}

func TestNewRejectsCyclicData(t *testing.T) {
	base := make(map[string]any)
	base["self"] = base
	cyclicParams := make(map[string]any)
	cyclicParams["loop"] = cyclicParams
	obj, errs := New(base, &mockSigilObj{name: "cyclic", params: []string{"loop"}, data: cyclicParams})
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want base and sigil cycle errors", errs)
	}
	for _, err := range errs {
		if !errors.Is(err, common.ErrNodeInfoCycle) {
			t.Fatalf("error = %v, want ErrNodeInfoCycle", err)
		}
	}
	if obj.Get("cyclic") != nil {
		t.Fatal("cyclic sigil was stored")
	}
}

func TestRegistryLifecycleAndMetadata(t *testing.T) {
	obj, errs := New(map[string]any{"base": "value"})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if errs := obj.Add(newMockSigil("bbb", "key-b"), newMockSigil("aaa", "key-a")); len(errs) != 0 {
		t.Fatalf("Add errors: %v", errs)
	}
	if errs := obj.Add(newMockSigil("aaa")); len(errs) != 1 {
		t.Fatalf("duplicate errors = %v", errs)
	}
	if got := obj.NodeInfo()[target.Name]; got != "[aaa,bbb] "+target.Version {
		t.Fatalf("metadata = %q", got)
	}
	if obj.LenSigils() != 2 || obj.LenLocal() != 4 || obj.Len() != 6 {
		t.Fatalf("lengths = sigils:%d local:%d total:%d", obj.LenSigils(), obj.LenLocal(), obj.Len())
	}
	if obj.String() != target.Name+" [aaa,bbb] "+target.Version {
		t.Fatalf("String() = %q", obj.String())
	}

	registry := obj.Sigils()
	delete(registry, "aaa")
	got := obj.Get("aaa").(*mockSigilObj)
	got.data["key-a"] = "changed"
	if obj.Get("aaa").Params()["key-a"] != "test" {
		t.Fatal("Get or Sigils exposed registry state")
	}
	if obj.Get("missing") != nil {
		t.Fatal("Get returned a missing sigil")
	}

	if err := obj.Del("aaa"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if obj.Get("aaa") != nil {
		t.Fatal("deleted sigil remains registered")
	}
	if _, exists := obj.NodeInfo()["key-a"]; exists {
		t.Fatal("deleted sigil key remains in NodeInfo")
	}
	if got := obj.NodeInfo()[target.Name]; got != "[bbb] "+target.Version {
		t.Fatalf("metadata after Del = %q", got)
	}
	if err := obj.Del("missing"); err == nil {
		t.Fatal("Del accepted a missing sigil")
	}
}

func TestDelPreservesUnpopulatedBaseKey(t *testing.T) {
	sigil, err := info.New(info.ConfigObj{Name: "test.node", Type: "router"})
	if err != nil {
		t.Fatal(err)
	}
	obj, errs := New(map[string]any{"location": "user-value"}, sigil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := obj.Del("info"); err != nil {
		t.Fatal(err)
	}
	if got := obj.NodeInfo()["location"]; got != "user-value" {
		t.Fatalf("unpopulated base key was removed: %v", got)
	}
	if _, exists := obj.NodeInfo()["name"]; exists {
		t.Fatal("populated sigil key remains after Del")
	}
}

func TestObjConcurrentRegistryAccess(t *testing.T) {
	obj, _ := New(nil)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			name := fmt.Sprintf("sigil-%02d", i)
			if errs := obj.Add(newMockSigil(name, "key-"+name)); len(errs) != 0 {
				t.Errorf("Add(%s): %v", name, errs)
			}
			_ = obj.NodeInfo()
			_ = obj.Sigils()
		}()
	}
	close(start)
	workers.Wait()
	if obj.LenSigils() != 16 {
		t.Fatalf("registered sigils = %d, want 16", obj.LenSigils())
	}
}
