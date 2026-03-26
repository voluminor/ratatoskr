package ratatoskr

import (
	"context"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	yggcore "github.com/yggdrasil-network/yggdrasil-go/src/core"

	"github.com/voluminor/ratatoskr/mod/peermgr"
)

// // // // // // // // // //

// ConfigObj — параметры создания узла для встраивания
type ConfigObj struct {
	// Родительский контекст; при отмене узел завершает работу.
	// nil → Close() нужно вызвать вручную
	Ctx context.Context

	// Конфигурация Yggdrasil; nil → случайные ключи.
	// Config.Peers должен быть пустым если задан Peers.
	Config *config.NodeConfig

	// Логгер; nil → логи отбрасываются
	Logger yggcore.Logger

	// Таймаут core.Stop(); 0 → без ограничения
	CoreStopTimeout time.Duration

	// Peers запускает менеджер пиров вместо стандартного механизма Yggdrasil.
	// nil → пиры берутся из Config.Peers как обычно.
	// Не nil + Config.Peers непустой → ошибка при New().
	Peers *peermgr.ConfigObj
}

// //

// SOCKSConfigObj — параметры запуска SOCKS5-прокси
type SOCKSConfigObj struct {
	// Адрес: TCP "127.0.0.1:1080" или Unix "/tmp/ygg.sock"
	Addr string

	// DNS-сервер в сети Yggdrasil для .ygg доменов.
	// Формат: "[ipv6]:port". Пустая строка → только .pk.ygg и литералы
	Nameserver string

	// Подробное логирование SOCKS-соединений
	Verbose bool

	// Максимум одновременных соединений; 0 → без ограничений
	MaxConnections int
}
