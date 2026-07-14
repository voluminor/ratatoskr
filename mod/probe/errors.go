package probe

import "errors"

// // // // // // // // // //

var (
	ErrSourceRequired          = errors.New("probe: source is required")
	ErrRemotePeersNotCaptured  = errors.New("probe: debug_remoteGetPeers was not captured")
	ErrMaxDepthRequired        = errors.New("probe: maxDepth must be > 0")
	ErrInvalidKeyLength        = errors.New("probe: invalid key length")
	ErrKeyNotInTree            = errors.New("probe: key not found in tree")
	ErrNoActivePath            = errors.New("probe: no active path to key")
	ErrRemotePeersDisabled     = errors.New("probe: debug_remoteGetPeers unavailable")
	ErrRemoteResponseTooLarge  = errors.New("probe: remote response too large")
	ErrRemoteCallTimedOut      = errors.New("probe: remote call timed out")
	ErrTreeEmpty               = errors.New("probe: tree entries are empty")
	ErrNoRoot                  = errors.New("probe: no self-rooted node in tree")
	ErrLookupTimedOut          = errors.New("probe: lookup timed out")
	ErrClosed                  = errors.New("probe: closed")
	ErrInvalidMaxTotalNodes    = errors.New("probe: MaxTotalNodes must be 0 or greater")
	ErrInvalidPollInterval     = errors.New("probe: PollInterval must be 0 or greater")
	ErrInvalidLookupRetryEvery = errors.New("probe: LookupRetryEvery must be 0 or greater")
	ErrProbeBusy               = errors.New("probe: too many distinct remote queries")
)
