package probe

import "errors"

// // // // // // // // // //

var (
	// ErrSourceRequired indicates a nil source in ConfigObj.
	ErrSourceRequired = errors.New("probe: source is required")
	// ErrRemotePeersNotCaptured indicates a missing upstream debug handler.
	ErrRemotePeersNotCaptured = errors.New("probe: debug_remoteGetPeers was not captured")
	// ErrMaxDepthRequired indicates a zero Tree depth.
	ErrMaxDepthRequired = errors.New("probe: maxDepth must be > 0")
	// ErrInvalidKeyLength indicates a public key that is not 32 bytes.
	ErrInvalidKeyLength = errors.New("probe: invalid key length")
	// ErrKeyNotInTree indicates a missing spanning-tree target.
	ErrKeyNotInTree = errors.New("probe: key not found in tree")
	// ErrNoActivePath indicates a missing pathfinder route.
	ErrNoActivePath = errors.New("probe: no active path to key")
	// ErrRemotePeersDisabled indicates an unavailable captured handler.
	ErrRemotePeersDisabled = errors.New("probe: debug_remoteGetPeers unavailable")
	// ErrRemoteResponseTooLarge indicates a response larger than 1 MiB.
	ErrRemoteResponseTooLarge = errors.New("probe: remote response too large")
	// ErrRemoteCallTimedOut indicates that RemoteTimeout elapsed.
	ErrRemoteCallTimedOut = errors.New("probe: remote call timed out")
	// ErrTreeEmpty indicates an empty local spanning tree.
	ErrTreeEmpty = errors.New("probe: tree entries are empty")
	// ErrNoRoot indicates malformed tree data without a self-parented root.
	ErrNoRoot = errors.New("probe: no self-rooted node in tree")
	// ErrLookupTimedOut indicates that route polling reached its deadline.
	ErrLookupTimedOut = errors.New("probe: lookup timed out")
	// ErrClosed indicates an operation attempted after shutdown.
	ErrClosed = errors.New("probe: closed")
	// ErrInvalidMaxTotalNodes indicates a negative node limit.
	ErrInvalidMaxTotalNodes = errors.New("probe: MaxTotalNodes must be 0 or greater")
	// ErrInvalidPollInterval indicates a negative poll interval.
	ErrInvalidPollInterval = errors.New("probe: PollInterval must be 0 or greater")
	// ErrInvalidLookupRetryEvery indicates a negative lookup retry interval.
	ErrInvalidLookupRetryEvery = errors.New("probe: LookupRetryEvery must be 0 or greater")
	// ErrProbeBusy indicates that 256 distinct remote flights are active.
	ErrProbeBusy = errors.New("probe: too many distinct remote queries")
)
