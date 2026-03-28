package peermgr

import (
	"errors"
	"testing"
)

// // // // // // // // // //

func TestValidatePeers_nil(t *testing.T) {
	res, errs := ValidatePeers(nil)
	if len(res) != 0 || len(errs) != 0 {
		t.Fatalf("expected empty; got res=%v errs=%v", res, errs)
	}
}

func TestValidatePeers_allWhitespace(t *testing.T) {
	res, errs := ValidatePeers([]string{"", "  ", "\t"})
	if len(res) != 0 || len(errs) != 0 {
		t.Fatalf("expected empty; got res=%v errs=%v", res, errs)
	}
}

func TestValidatePeers_allSchemes(t *testing.T) {
	for _, scheme := range AllowedSchemes {
		uri := scheme + "://host.example.com:1234"
		res, errs := ValidatePeers([]string{uri})
		if len(errs) != 0 {
			t.Errorf("scheme %q: unexpected errors: %v", scheme, errs)
		}
		if len(res) != 1 || res[0].Scheme != scheme {
			t.Errorf("scheme %q: unexpected result: %v", scheme, res)
		}
	}
}

func TestValidatePeers_orderPreserved(t *testing.T) {
	uris := []string{
		"tls://a.example.com:4443",
		"tcp://b.example.com:1234",
		"quic://c.example.com:9000",
	}
	res, errs := ValidatePeers(uris)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	expected := []string{"tls", "tcp", "quic"}
	for i, p := range res {
		if p.Scheme != expected[i] {
			t.Errorf("[%d] scheme order: got %q, want %q", i, p.Scheme, expected[i])
		}
	}
}

func TestValidatePeers_duplicate(t *testing.T) {
	res, errs := ValidatePeers([]string{"tcp://h:1", "tcp://h:1"})
	if len(errs) == 0 {
		t.Fatal("expected duplicate error")
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 valid entry; got %d", len(res))
	}
	if !errors.Is(errs[0], ErrDuplicatePeer) {
		t.Errorf("expected ErrDuplicatePeer, got: %v", errs[0])
	}
}

func TestValidatePeers_missingHost(t *testing.T) {
	// "tcp://" has no host or port
	_, errs := ValidatePeers([]string{"tcp://"})
	if len(errs) == 0 {
		t.Fatal("expected missing-host error")
	}
}

func TestValidatePeers_unsupportedScheme(t *testing.T) {
	_, errs := ValidatePeers([]string{"ftp://host:21"})
	if len(errs) == 0 {
		t.Fatal("expected unsupported-scheme error")
	}
	if !errors.Is(errs[0], ErrUnsupportedScheme) {
		t.Errorf("expected ErrUnsupportedScheme, got: %v", errs[0])
	}
}

func TestValidatePeers_mixedErrors(t *testing.T) {
	peers := []string{
		"tls://good:1",
		"ftp://bad:21",
		"tls://good:1", // duplicate
		"tls://good2:2",
	}
	res, errs := ValidatePeers(peers)
	if len(res) != 2 {
		t.Errorf("expected 2 valid, got %d", len(res))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidatePeers_trimSpace(t *testing.T) {
	res, errs := ValidatePeers([]string{"  tls://h:1  "})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1, got %d", len(res))
	}
}

func TestValidatePeers_uriPreserved(t *testing.T) {
	res, _ := ValidatePeers([]string{"tls://host.example.com:4443"})
	if len(res) == 0 || res[0].URI == "" {
		t.Error("expected non-empty URI in result")
	}
}

// //

func BenchmarkValidatePeers_small(b *testing.B) {
	peers := []string{
		"tls://peer1.example.com:4443",
		"tcp://peer2.example.com:1234",
		"quic://peer3.example.com:9000",
	}
	for b.Loop() {
		ValidatePeers(peers)
	}
}

func BenchmarkValidatePeers_large(b *testing.B) {
	peers := make([]string, 100)
	for i := range peers {
		peers[i] = "tls://host" + string(rune('a'+i%26)) + ":1234"
	}
	for b.Loop() {
		ValidatePeers(peers)
	}
}
