package probe

import "errors"

// // // // // // // // // //

var (
	ErrCoreRequired              = errors.New("probe: core is required")
	ErrInvalidConfig             = errors.New("probe: invalid config")
	ErrRemotePeersNotCaptured    = errors.New("probe: debug_remoteGetPeers was not captured")
	ErrMaxDepthRequired          = errors.New("probe: maxDepth must be > 0")
	ErrPeersPerNodeLimitExceeded = errors.New("probe: peers-per-node limit exceeded")
	ErrInvalidKeyLength          = errors.New("probe: invalid key length")
	ErrKeyNotInTree              = errors.New("probe: key not found in tree")
	ErrNoActivePath              = errors.New("probe: no active path to key")
	ErrNodeUnreachable           = errors.New("probe: node unreachable (cached)")
	ErrRemotePeersDisabled       = errors.New("probe: debug_remoteGetPeers unavailable")
	ErrRemoteResponseTooLarge    = errors.New("probe: remote response too large")
	ErrTreeEmpty                 = errors.New("probe: tree entries are empty")
	ErrNoRoot                    = errors.New("probe: no self-rooted node in tree")
	ErrLookupTimedOut            = errors.New("probe: lookup timed out")
	ErrClosed                    = errors.New("probe: closed")
)
