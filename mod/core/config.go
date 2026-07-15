package core

import (
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// ConfigObj contains node construction parameters.
type ConfigObj struct {
	// Config is copied during construction. Nil generates a default node config.
	Config *config.NodeConfig

	// Logger receives Yggdrasil logs. Nil discards them.
	Logger yggcore.Logger
}
