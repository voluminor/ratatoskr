package core

import (
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"
)

// // // // // // // // // //

// ConfigObj — параметры создания узла Yggdrasil
type ConfigObj struct {
	// Конфигурация Yggdrasil (ключи, пиры, listen); nil → случайные ключи
	Config *config.NodeConfig

	// Логгер; nil → логи отбрасываются
	Logger yggcore.Logger

	// Таймаут core.Stop(); 0 → ожидание без ограничений
	CoreStopTimeout time.Duration

	// Размер очереди отложенных RST-пакетов; 0 → 100
	RSTQueueSize int
}
