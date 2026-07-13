package ratatoskr

import (
	"context"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

// ConfigObj — node creation parameters for embedding
type ConfigObj struct {
	// Parent context; cancellation shuts down the node.
	// nil → Close() must be called manually
	Ctx context.Context

	// Yggdrasil configuration; nil → random keys.
	// Config.Peers must be empty if Peers is set.
	Config *config.NodeConfig

	// Logger; nil → logs are discarded
	Logger yggcore.Logger

	// Total budget for Close(); 0 → 10s default, <0 → invalid.
	// Once the budget expires, Close returns ErrCloseTimedOut while unfinished
	// component teardown continues best-effort in the background.
	CloseTimeout time.Duration

	// RST packet deferred queue size; 0 → core default.
	RSTQueueSize int

	// Peers enables the peer manager instead of the standard Yggdrasil mechanism.
	// nil → peers are taken from Config.Peers as usual.
	// Not nil + Config.Peers non-empty → error in New().
	Peers *peermgr.ConfigObj

	// Sigils for NodeInfo assembly; nil → not used.
	// When set, sigils write their data into Config.NodeInfo.
	// Config.NodeInfo serves as the base (has priority); sigil data is added on top.
	// Can be combined with Config.NodeInfo or used standalone.
	Sigils []sigils.Interface
}

// //

// SOCKSConfigObj — SOCKS5 proxy startup parameters
type SOCKSConfigObj struct {
	// Address: TCP "127.0.0.1:1080" or a Unix socket in a private directory.
	Addr string

	// DNS server in the Yggdrasil network for .ygg domains.
	// Format: "[ipv6]:port". Empty string → only .pk.ygg and literals
	Nameserver string

	// Verbose logging of SOCKS connections
	Verbose bool

	// Maximum simultaneous connections; 0 → safe default, <0 → unlimited
	MaxConnections int

	// SOCKS handshake timeout; 0 → safe default, <0 → disabled
	HandshakeTimeout time.Duration

	// SOCKS outbound dial timeout; 0 -> safe default, <0 -> disabled
	DialTimeout time.Duration

	// SOCKS established tunnel idle timeout; 0 -> safe default, <0 -> disabled
	TunnelIdleTimeout time.Duration

	// Max UDP ASSOCIATE targets per session; 0 -> safe default,
	// <0 -> no per-session cap. The per-server safety cap still applies.
	MaxAssociateTargetsPerSession int

	// DNS lookup timeout for Nameserver; 0 -> safe default, <0 -> no resolver-imposed
	// deadline (each query is still bounded by the Go DNS client's own ~5s timeout)
	NameserverLookupTimeout time.Duration

	// Positive DNS cache TTL for Nameserver; 0 -> safe default, <0 -> disabled
	NameserverCacheTTL time.Duration

	// Positive DNS cache cap for Nameserver; 0 -> safe default, <0 -> disabled
	NameserverCacheMaxEntries int

	// Optional SOCKS5 username/password credentials
	Credentials socks.CredentialsInterface
}
