package gsettings

import (
	"testing"
	"time"
)

// // // // // // // // // //

func TestParseAskCommand(t *testing.T) {
	cfg, err := Parse([]string{
		"-go.ask.addr=200::1",
		"-go.ask.peer=tls://a:1,tcp://b:2",
		"-go.ask.peer=quic://c:3",
		"-go.ask.timeout=3s",
		"-go.ask.format=json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ask.Addr != "200::1" || cfg.Ask.Timeout != 3*time.Second || cfg.Ask.Format != GoAskFormatJson {
		t.Fatalf("Ask config = %+v", cfg.Ask)
	}
	if len(cfg.Ask.Peer) != 3 {
		t.Fatalf("peer count = %d, want 3", len(cfg.Ask.Peer))
	}
}

func TestParseRejectsInvalidEnum(t *testing.T) {
	if _, err := Parse([]string{"-go.forward.proto=sctp"}); err == nil {
		t.Fatal("invalid protocol was accepted")
	}
}
