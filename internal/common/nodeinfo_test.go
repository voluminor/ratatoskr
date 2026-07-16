package common

import (
	"errors"
	"testing"
)

// // // // // // // // // //

func TestCloneNodeInfoDeepCopiesTypedContainers(t *testing.T) {
	src := map[string]any{
		"groups": map[string][]string{"a": {"one", "two"}},
		"nested": []any{map[string]any{"value": "before"}},
		"nil":    nil,
	}
	clone, err := CloneNodeInfo(src)
	if err != nil {
		t.Fatalf("CloneNodeInfo: %v", err)
	}
	src["groups"].(map[string][]string)["a"][0] = "changed"
	src["nested"].([]any)[0].(map[string]any)["value"] = "changed"
	if got := clone["groups"].(map[string][]string)["a"][0]; got != "one" {
		t.Fatalf("typed nested slice leaked: %q", got)
	}
	if got := clone["nested"].([]any)[0].(map[string]any)["value"]; got != "before" {
		t.Fatalf("nested map leaked: %q", got)
	}
	if clone["nil"] != nil {
		t.Fatal("nil value changed")
	}
}

func TestCloneNodeInfoRejectsCycles(t *testing.T) {
	mapCycle := make(map[string]any)
	mapCycle["self"] = mapCycle
	sliceCycle := make([]any, 1)
	sliceCycle[0] = sliceCycle
	for name, src := range map[string]map[string]any{
		"map":   mapCycle,
		"slice": {"self": sliceCycle},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CloneNodeInfo(src); !errors.Is(err, ErrNodeInfoCycle) {
				t.Fatalf("CloneNodeInfo error = %v, want ErrNodeInfoCycle", err)
			}
		})
	}
}

func TestCloneNodeInfoRejectsExcessiveDepth(t *testing.T) {
	src := map[string]any{}
	cursor := src
	for i := 0; i <= maxNodeInfoDepth; i++ {
		next := map[string]any{}
		cursor["next"] = next
		cursor = next
	}
	if _, err := CloneNodeInfo(src); !errors.Is(err, ErrNodeInfoTooDeep) {
		t.Fatalf("CloneNodeInfo error = %v, want ErrNodeInfoTooDeep", err)
	}
}

func TestCloneNodeInfoAllowsSharedAcyclicValues(t *testing.T) {
	shared := map[string]any{"value": "before"}
	clone, err := CloneNodeInfo(map[string]any{"a": shared, "b": shared})
	if err != nil {
		t.Fatalf("CloneNodeInfo: %v", err)
	}
	clone["a"].(map[string]any)["value"] = "changed"
	if got := clone["b"].(map[string]any)["value"]; got != "before" {
		t.Fatalf("shared DAG branches alias after clone: %q", got)
	}
}
