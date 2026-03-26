package core

import (
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// ConfigObj — Yggdrasil node creation parameters
type ConfigObj struct {
	// Yggdrasil configuration (keys, peers, listen); nil → random keys
	Config *config.NodeConfig

	// Logger; nil → logs are discarded
	Logger yggcore.Logger

	// core.Stop() timeout; 0 → unlimited wait
	CoreStopTimeout time.Duration

	// RST packet deferred queue size; 0 → 100
	RSTQueueSize int
}
