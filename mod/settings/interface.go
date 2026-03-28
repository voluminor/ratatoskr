package settings

import gsettings "github.com/voluminor/ratatoskr/target/settings"

// // // // // // // // // //

// Interface is the top-level contract for reading settings.
type Interface interface {
	GetConfig() string
	GetYggdrasil() gsettings.YggdrasilInterface
	Self() any
}
