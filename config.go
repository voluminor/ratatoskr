package ratatoskr

import (
	"context"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/ninfo"
	"github.com/voluminor/ratatoskr/mod/peermgr"
	"github.com/voluminor/ratatoskr/mod/sigils"
	"github.com/voluminor/ratatoskr/mod/socks"
)

// // // // // // // // // //

// ConfigObj configures a node.
type ConfigObj struct {
	// Ctx closes the node when canceled. A nil context requires an explicit Close.
	Ctx context.Context

	// Config is the Yggdrasil configuration. Nil generates a configuration with
	// random keys and disables the admin listener.
	// Config.Peers must be empty if Peers is set.
	Config *config.NodeConfig

	// Logger receives module logs. Nil discards logs.
	Logger yggcore.Logger

	// CloseTimeout bounds the total Close wait. Zero uses 10 seconds; negative
	// values are invalid.
	// Once the budget expires, Close returns ErrCloseTimedOut while unfinished
	// component teardown continues best-effort in the background.
	CloseTimeout time.Duration

	// Peers enables managed peer selection. Nil uses Config.Peers directly. New
	// rejects a non-nil value when Config.Peers is not empty and always replaces
	// Peers.Node with this node's core.
	Peers *peermgr.ConfigObj

	// NodeInfo configures Ask and AskAddr. New always replaces NodeInfo.Source
	// with this node's core. Nil uses ninfo defaults.
	NodeInfo *ninfo.ConfigObj

	// Sigils assemble local NodeInfo and configure custom remote parsers. Existing
	// top-level Config.NodeInfo keys take precedence over sigil output. Invalid
	// sigils cause New to fail.
	Sigils []sigils.Interface
}

// //

// SOCKSConfigObj configures the root SOCKS5 service.
type SOCKSConfigObj struct {
	// Addr is a TCP address or a Unix socket path in a private directory.
	Addr string

	// Nameserver is an Yggdrasil DNS server in "[ipv6]:port" form. Empty permits
	// only .pk.ygg names and IP literals.
	Nameserver string

	// Verbose enables per-connection logging.
	Verbose bool

	// MaxConnections limits simultaneous connections. Zero uses the module
	// default; negative is unlimited.
	MaxConnections int

	// HandshakeTimeout bounds SOCKS handshakes. Zero uses the module default;
	// negative disables the timeout.
	HandshakeTimeout time.Duration

	// DialTimeout bounds outbound dials. Zero uses the module default; negative
	// disables the timeout.
	DialTimeout time.Duration

	// TunnelIdleTimeout bounds idle established tunnels. Zero uses the module
	// default; negative disables the timeout.
	TunnelIdleTimeout time.Duration

	// MaxAssociateTargetsPerSession limits UDP ASSOCIATE targets per session.
	// Zero uses the module default; negative disables this limit.
	MaxAssociateTargetsPerSession int

	// MaxAssociateTargetsPerPrincipal limits UDP ASSOCIATE targets per
	// authenticated user or source IP. Non-positive values are unlimited.
	MaxAssociateTargetsPerPrincipal int

	// MaxAssociateQueuedPacketsPerTarget limits queued packets per established
	// UDP ASSOCIATE target. Zero uses 64; negative is unlimited.
	MaxAssociateQueuedPacketsPerTarget int

	// MaxAssociateQueuedBytesPerTarget limits queued payload bytes per established
	// UDP ASSOCIATE target. Zero uses 64 KiB; negative is unlimited.
	MaxAssociateQueuedBytesPerTarget int

	// NameserverLookupTimeout bounds DNS lookups. Zero uses the resolver default;
	// negative disables the resolver-imposed deadline.
	NameserverLookupTimeout time.Duration

	// NameserverCacheTTL controls positive DNS caching. Zero uses the resolver
	// default; negative disables caching.
	NameserverCacheTTL time.Duration

	// NameserverCacheMaxEntries limits positive DNS cache entries. Zero uses the
	// resolver default; negative disables caching.
	NameserverCacheMaxEntries int

	// Credentials optionally enables SOCKS5 username/password authentication.
	Credentials socks.CredentialsInterface
}
