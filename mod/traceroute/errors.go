package traceroute

import "errors"

// // // // // // // // // //

var (
	ErrCoreRequired          = errors.New("traceroute: core is required")
	ErrLoggerRequired        = errors.New("traceroute: logger is required")
	ErrRemotePeersNotCapture = errors.New("traceroute: debug_remoteGetPeers was not captured")
	ErrInvalidCacheTTL       = errors.New("traceroute: CacheTTL must be >= 1s")
	ErrMaxDepthRequired      = errors.New("traceroute: maxDepth must be > 0")
	ErrInvalidKeyLength      = errors.New("traceroute: invalid key length")
	ErrKeyNotInTree          = errors.New("traceroute: key not found in tree")
	ErrNoActivePath          = errors.New("traceroute: no active path to key")
	ErrNodeUnreachable       = errors.New("traceroute: node unreachable (cached)")
	ErrRemotePeersDisabled   = errors.New("traceroute: debug_remoteGetPeers unavailable")
	ErrTreeEmpty             = errors.New("traceroute: tree entries are empty")
	ErrNoRoot                = errors.New("traceroute: no self-rooted node in tree")
	ErrLookupTimedOut        = errors.New("traceroute: lookup timed out")
)
