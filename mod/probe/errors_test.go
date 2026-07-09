package probe

import (
	"testing"
)

// // // // // // // // // //

func TestErrors_distinct(t *testing.T) {
	errs := []error{
		ErrCoreRequired, ErrRemotePeersNotCaptured,
		ErrMaxDepthRequired, ErrInvalidKeyLength,
		ErrKeyNotInTree, ErrNoActivePath, ErrNodeUnreachable,
		ErrRemotePeersDisabled, ErrTreeEmpty, ErrNoRoot, ErrLookupTimedOut,
	}
	seen := make(map[string]bool, len(errs))
	for _, e := range errs {
		msg := e.Error()
		if seen[msg] {
			t.Fatalf("duplicate error message: %q", msg)
		}
		seen[msg] = true
	}
}
