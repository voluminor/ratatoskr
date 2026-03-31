package probe

import "errors"

// // // // // // // // // //

var (
	ErrCoreRequired           = errors.New("probe: core is required")
	ErrLoggerRequired         = errors.New("probe: logger is required")
	ErrRemotePeersNotCaptured = errors.New("probe: debug_remoteGetPeers was not captured")
	ErrInvalidCacheTTL        = errors.New("probe: CacheTTL must be >= 1s")
	ErrMaxDepthRequired       = errors.New("probe: maxDepth must be > 0")
	ErrInvalidKeyLength       = errors.New("probe: invalid key length")
	ErrKeyNotInTree           = errors.New("probe: key not found in tree")
	ErrNoActivePath           = errors.New("probe: no active path to key")
	ErrNodeUnreachable        = errors.New("probe: node unreachable (cached)")
	ErrRemotePeersDisabled    = errors.New("probe: debug_remoteGetPeers unavailable")
	ErrTreeEmpty              = errors.New("probe: tree entries are empty")
	ErrNoRoot                 = errors.New("probe: no self-rooted node in tree")
	ErrLookupTimedOut         = errors.New("probe: lookup timed out")
)
