package core

import (
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// ConfigObj contains node construction parameters.
type ConfigObj struct {
	// Config is captured during construction. Treat it and nested values as
	// immutable for the node lifetime. Nil generates a default node config.
	Config *config.NodeConfig

	// Logger receives Yggdrasil logs. Nil discards them.
	Logger yggcore.Logger
}
