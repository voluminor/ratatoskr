package settings

import "time"

// // // // // // // // // //

// Interface — top-level contract for a settings object
type Interface interface {
	GetConfig() string
	Self() any
}

// //

// MulticastInterface — multicast peer discovery settings
type MulticastInterface interface {
	GetBeacon() bool
	GetListen() bool
	GetPassword() string
	GetPort() uint16
	GetPriority() uint16
	GetRegex() string
}

// PeerManagerInterface — smart peer manager settings
type PeerManagerInterface interface {
	GetBatchSize() int
	GetMaxPerProto() int
	GetProbeTimeout() time.Duration
	GetRefreshInterval() time.Duration
}

// SocksInterface — SOCKS5 proxy settings
type SocksInterface interface {
	GetAddr() string
	GetMaxConnections() int
	GetNameserver() string
	GetVerbose() bool
}

// YggdrasilInterface — full typed accessor for yggdrasil settings
type YggdrasilInterface interface {
	GetAdminListen() string
	GetAllowedPublicKeys() []string
	GetCoreStopTimeout() time.Duration
	GetIfMtu() uint64
	GetIfName() string
	GetInterfacePeers() map[string][]string
	GetListen() []string
	GetLogLookups() bool
	GetMulticast() MulticastInterface
	GetNodeInfo() map[string]any
	GetNodeInfoPrivacy() bool
	GetPeerManager() PeerManagerInterface
	GetPeers() []string
	GetPrivateKey() string
	GetPrivateKeyPath() string
	GetRstQueueSize() int
	GetSocks() SocksInterface
}
