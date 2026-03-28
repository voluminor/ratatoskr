package settings

import gsettings "github.com/voluminor/ratatoskr/target/settings"

// // // // // // // // // //

// Interface is the top-level contract for reading settings.
type Interface interface {
	GetConfig() string
	GetYggdrasil() gsettings.YggdrasilInterface
	Self() any
}

// //

// Re-exported sub-setting interfaces for external consumers.
type YggdrasilInterface = gsettings.YggdrasilInterface
type KeyInterface = gsettings.YggdrasilKeyInterface
type IfInterface = gsettings.YggdrasilIfInterface
type NodeInterface = gsettings.YggdrasilNodeInterface
type PeersInterface = gsettings.YggdrasilPeersInterface
type ManagerInterface = gsettings.YggdrasilPeersManagerInterface
type MulticastInterface = gsettings.YggdrasilMulticastInterface
type SocksInterface = gsettings.YggdrasilSocksInterface
