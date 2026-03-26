package mobile

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"time"

	"github.com/quic-go/quic-go"
)

// // // // // // // // // //

const quicDialTimeout = 10 * time.Second

// CheckQuicRTT измеряет RTT QUIC-рукопожатия до пира в миллисекундах.
// peerURI — URI формата quic://, например "quic://1.2.3.4:12345".
// Возвращает длительность рукопожатия в мс или -1 при ошибке.
func CheckQuicRTT(peerURI string) (int64, error) {
	u, err := url.Parse(peerURI)
	if err != nil {
		return -1, fmt.Errorf("url.Parse: %w", err)
	}
	if u.Scheme != "quic" {
		return -1, fmt.Errorf("expected quic:// URI, got scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return -1, fmt.Errorf("missing host in URI %q", peerURI)
	}

	ctx, cancel := context.WithTimeout(context.Background(), quicDialTimeout)
	defer cancel()

	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec — peer cert is self-signed by design
		NextProtos:         []string{"yggdrasil"},
	}

	start := time.Now()
	conn, err := quic.DialAddr(ctx, u.Host, tlsCfg, nil)
	if err != nil {
		return -1, fmt.Errorf("QUIC dial %s: %w", u.Host, err)
	}
	rtt := time.Since(start).Milliseconds()
	_ = conn.CloseWithError(0, "")
	return rtt, nil
}
