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
type KeyInterface = gsettings.KeyInterface
type IfInterface = gsettings.IfInterface
type NodeInterface = gsettings.NodeInterface
type PeersInterface = gsettings.PeersInterface
type ManagerInterface = gsettings.ManagerInterface
type MulticastInterface = gsettings.MulticastInterface
type SocksInterface = gsettings.SocksInterface
