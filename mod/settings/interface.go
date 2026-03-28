package settings

import gsettings "github.com/voluminor/ratatoskr/target/settings"

// // // // // // // // // //

// Interface — top-level contract for a settings object
type Interface interface {
	GetConfig() string
	GetYggdrasil() gsettings.YggdrasilInterface
	Self() any
}

// //

type YggdrasilInterface = gsettings.YggdrasilInterface
type KeyInterface = gsettings.YggdrasilKeyInterface
type IfInterface = gsettings.YggdrasilIfInterface
type NodeInterface = gsettings.YggdrasilNodeInterface
type PeersInterface = gsettings.YggdrasilPeersInterface
type ManagerInterface = gsettings.YggdrasilPeersManagerInterface
type MulticastInterface = gsettings.YggdrasilMulticastInterface
type SocksInterface = gsettings.YggdrasilSocksInterface
