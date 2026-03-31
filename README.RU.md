[![Go Report Card](https://goreportcard.com/badge/github.com/voluminor/ratatoskr)](https://goreportcard.com/report/github.com/voluminor/ratatoskr)

![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/voluminor/ratatoskr?color=orange)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/voluminor/ratatoskr?color=green)
![GitHub repo size](https://img.shields.io/github/repo-size/voluminor/ratatoskr)

# ratatoskr

> **[English version](README.md)**

Go-библиотека для встраивания узла Yggdrasil в приложение. Сетевой стек работает в userspace
на базе gVisor netstack — не требуется TUN-интерфейс, root-доступ или внешние зависимости.

- **Userspace-стек.** TCP/UDP поверх gVisor netstack, без привилегий ОС.
- **Стандартные Go-интерфейсы.** `DialContext`, `Listen`, `ListenPacket` — совместимы с `net.Conn`,
  `net.Listener`, `http.Transport` и т. д.
- **`core.Interface` как контракт.** Пакеты `socks`, `peermgr`, корневой `ratatoskr` зависят от
  интерфейса, а не от реализации `core.Obj`. Можно подставить собственную реализацию для тестов
  или нестандартных транспортов.

### ratatoskr vs yggstack

[yggstack](https://github.com/yggdrasil-network/yggstack) — готовый бинарник для конечного
пользователя (SOCKS-прокси, TCP/UDP-форвардинг через CLI-флаги). `ratatoskr` — библиотека для
разработчика: узел создаётся вызовом `ratatoskr.New()`, всё управление — через Go API.

---

## Содержание

- [Установка](#установка)
- [Быстрый старт](#быстрый-старт)
- [Архитектура](#архитектура)
- [API корневого пакета](#api-корневого-пакета)
  - [New](#new)
  - [SOCKS5-прокси](#socks5-прокси)
  - [Менеджер пиров](#менеджер-пиров)
  - [RetryPeers](#retrypeers)
  - [Ask / AskAddr](#ask--askaddr)
  - [Snapshot](#snapshot)
  - [Close](#close)
- [Конфигурация](#конфигурация)
  - [ConfigObj](#configobj)
  - [SOCKSConfigObj](#socksconfigobj)
- [Типы снимков](#типы-снимков)
- [Ошибки](#ошибки)
- [Потокобезопасность](#потокобезопасность)
- [Жизненный цикл](#жизненный-цикл)
- [Примеры использования](#примеры-использования)
- [Модули](#модули)
- [Примеры приложений](#примеры-приложений)
- [Поддерживаемые платформы](#поддерживаемые-платформы)

---

## Установка

```bash
go get github.com/voluminor/ratatoskr
```

Минимальная версия Go: **1.24**.

---

## Быстрый старт

Создать узел, подключиться к сети и сделать HTTP-запрос:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/voluminor/ratatoskr"
	"github.com/voluminor/ratatoskr/mod/peermgr"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	node, err := ratatoskr.New(ratatoskr.ConfigObj{
		Ctx: ctx,
		Peers: &peermgr.ConfigObj{
			Peers: []string{
				"tls://peer1.example.com:17117",
				"tls://peer2.example.com:17117",
			},
          MaxPerProto: 1,
		},
	})
	if err != nil {
		panic(err)
	}
	defer node.Close()

	fmt.Println("Адрес в сети:", node.Address())

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: node.DialContext,
		},
	}

	resp, err := client.Get("http://[200:abcd::1]:8080/api")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
}
```

---

## Архитектура

```mermaid
flowchart TB
    App[Приложение]

  subgraph ratatoskr["ratatoskr (корневой пакет)"]
    Obj["Obj — фасад"]
    SOCKS["SOCKS5-прокси"]
    PeerMgr["Peer Manager"]
    NInfo["ninfo — Ask / AskAddr"]
  end

  subgraph core["mod/core"]
    CoreObj["core.Obj"]
    Netstack["netstack — userspace TCP/UDP"]
  end

  subgraph sigils["mod/sigils"]
    SigilCore["sigil_core — сборка NodeInfo"]
    Sigils["inet, info, public, services"]
  end

  subgraph external["Внешние зависимости"]
    YggCore["yggdrasil-go/core"]
    gVisor["gVisor netstack"]
  end

  App --> Obj
  Obj -->|" core.Interface "| CoreObj
  Obj --> SOCKS
  Obj --> PeerMgr
  Obj --> NInfo
  NInfo -->|" getNodeInfo "| YggCore
  SOCKS -->|" DialContext "| CoreObj
  PeerMgr -->|" AddPeer / RemovePeer "| CoreObj
  SigilCore --> CoreObj
  Sigils --> SigilCore
  CoreObj --> Netstack
  Netstack --> gVisor
  Netstack -->|" IPv6-пакеты "| YggCore
```

`ratatoskr.Obj` встраивает `core.Interface` — все сетевые методы (`DialContext`, `Listen`,
`ListenPacket`, `Address`, `Subnet`, `PublicKey` и т.д.) доступны напрямую. SOCKS5-прокси,
менеджер пиров и ninfo — опциональные компоненты, управляемые через методы `Obj`.

---

## API корневого пакета

### New

```go
func New(cfg ConfigObj) (*Obj, error)
```

Создаёт и запускает узел Yggdrasil. Возвращает `*Obj` — фасад с полным набором возможностей.

```mermaid
flowchart LR
  New["New(cfg)"] --> SG{Sigils задан?}
  SG -->|Да| SC["sigil_core.New() → NodeInfo"]
  SG -->|Нет| Core
  SC --> Core["Запуск core"]
  Core --> NI["ninfo.New()"]
  NI --> PM{Peers задан?}
  PM -->|Да| Start["peermgr.Start()"]
  PM -->|Нет| Ready["Obj готов"]
  Start --> Ready
  Ready -->|" Ctx != nil "| Watch["горутина: <-Ctx.Done() → Close()"]
```

- Если `cfg.Config == nil` — генерируются случайные ключи
- Если `cfg.Logger == nil` — логи отбрасываются (noop-логгер)
- Если `cfg.Sigils != nil` — собирается NodeInfo из сигилов; `Config.NodeInfo` используется как база
- Если `cfg.Peers != nil` — запускается менеджер пиров; `cfg.Config.Peers` должен быть пустым
- Если `cfg.Ctx != nil` — при отмене контекста узел автоматически завершится

После успешного вызова `New` все сетевые методы `core.Interface` доступны через `Obj`:

| Метод                         | Описание                                     |
|-------------------------------|----------------------------------------------|
| `DialContext(ctx, net, addr)` | Исходящее TCP/UDP-соединение через Yggdrasil |
| `Listen(net, addr)`           | TCP-listener в сети Yggdrasil                |
| `ListenPacket(net, addr)`     | UDP-listener                                 |
| `Address()`                   | IPv6-адрес узла (200::/7)                    |
| `Subnet()`                    | Подсеть /64                                  |
| `PublicKey()`                 | Публичный ключ ed25519                       |
| `MTU()`                       | MTU стека                                    |
| `GetPeers()`                  | Список пиров с метриками                     |
| `AddPeer(uri)`                | Добавить пир                                 |
| `RemovePeer(uri)`             | Удалить пир                                  |
| `EnableMulticast(logger)`     | mDNS-обнаружение в локальной сети            |
| `DisableMulticast()`          | Остановить multicast                         |
| `EnableAdmin(addr)`           | Admin-сокет (unix/tcp)                       |
| `DisableAdmin()`              | Остановить admin-сокет                       |

Подробнее о сетевых операциях, компонентах и NIC — в [mod/core/README.md](mod/core/README.md).

### SOCKS5-прокси

```go
func (o *Obj) EnableSOCKS(cfg SOCKSConfigObj) error
func (o *Obj) DisableSOCKS() error
```

`EnableSOCKS` запускает SOCKS5-прокси. Резолвер создаётся автоматически на основе `cfg.Nameserver`.
`DisableSOCKS` останавливает прокси; идемпотентный.

```mermaid
stateDiagram-v2
  [*] --> Создан: New()
  Создан --> Активен: EnableSOCKS()
  Активен --> Создан: DisableSOCKS()
  Активен --> Активен: EnableSOCKS() → ошибка
  Создан --> Создан: DisableSOCKS() → no-op
```

Цикл `Enable → Disable → Enable` поддерживается. Подробнее — в [mod/socks/README.md](mod/socks/README.md).

### Менеджер пиров

```go
func (o *Obj) PeerManagerActive() []string
func (o *Obj) PeerManagerOptimize() error
```

Менеджер пиров включается через `ConfigObj.Peers` при вызове `New`. Если `Peers == nil` — методы
возвращают `nil` / `ErrPeerManagerNotEnabled`.

| Метод                   | Описание                                                           |
|-------------------------|--------------------------------------------------------------------|
| `PeerManagerActive()`   | Текущие активные пиры (копия); `nil` если менеджер не используется |
| `PeerManagerOptimize()` | Принудительная переоценка пиров (блокирует до завершения)          |

При `MinPeers > 0` (только активный режим) фоновый мониторинг отслеживает потерю пиров: если число
активных пиров опускается до порога на несколько проверок подряд, автоматически запускается
внеочередная переоценка.

Подробнее о режимах, батчинге, MinPeers watch и валидации пиров — в [mod/peermgr/README.md](mod/peermgr/README.md).

### RetryPeers

```go
func (o *Obj) RetryPeers()
```

Инициирует немедленное переподключение отключённых пиров. Работает независимо от менеджера пиров —
вызывает `core.RetryPeersNow()` напрямую.

### Ask / AskAddr

```go
func (o *Obj) Ask(ctx context.Context, key ed25519.PublicKey) (*ninfo.AskResultObj, error)
func (o *Obj) AskAddr(ctx context.Context, addr string) (*ninfo.AskResultObj, error)
```

Запрос NodeInfo удалённого узла. `Ask` принимает публичный ключ, `AskAddr` — строку-адрес
(64-символьный hex, `<hex>.pk.ygg`, `[ipv6]:port` или голый IPv6). Возвращает распарсенные
метаданные, информацию о ПО и замеренный RTT.

Если удалённый узел использует `ratatoskr`, ответ автоматически разбирается на сигилы — каждый
известный сигил попадает в `AskResultObj.Node.Sigils`, остальные ключи — в `Extra`.

```go
result, err := node.AskAddr(ctx, "200:abcd::1")
if err != nil {
log.Fatal(err)
}
fmt.Printf("RTT: %s, version: %s\n", result.RTT, result.Node.Version)
if result.Software != nil {
fmt.Printf("Software: %s %s\n", result.Software.Name, result.Software.Version)
}
for name, sigil := range result.Node.Sigils {
fmt.Printf("Sigil %s: %v\n", name, sigil.Params())
}
```

Внутренне `ninfo` создаётся всегда при `New()`. Если в `ConfigObj.Sigils` переданы сигилы — они
импортируются в `ninfo` как парсеры для ответов. Подробнее — в [mod/ninfo/README.md](mod/ninfo/README.md).

### Sigils (NodeInfo)

```go
type ConfigObj struct {
// ...
Sigils []sigils.Interface
}
```

Сигилы — типизированные блоки данных для NodeInfo. Каждый сигил владеет набором ключей в NodeInfo
и умеет записывать/читать их. При передаче в `ConfigObj.Sigils`:

1. `sigil_core.New()` собирает NodeInfo из базового `Config.NodeInfo` и переданных сигилов
2. Результат записывается в `Config.NodeInfo` перед запуском core
3. Те же сигилы импортируются в `ninfo` как парсеры для `Ask`/`AskAddr`

```go
node, err := ratatoskr.New(ratatoskr.ConfigObj{
Ctx: ctx,
Sigils: []sigils.Interface{
info.New("my-node", "My cool Yggdrasil node"),
public.New(ed25519.PublicKey(pk)),
inet.New("192.168.1.1", 8080),
},
})
```

Встроенные сигилы: `info`, `public`, `inet`, `services`. Можно создавать свои, реализуя
`sigils.Interface`. Подробнее — в [mod/sigils/README.md](mod/sigils/README.md) и
[mod/sigils/sigil_core/README.md](mod/sigils/sigil_core/README.md).

### Snapshot

```go
func (o *Obj) Snapshot() SnapshotObj
```

Собирает полное состояние узла за один вызов:

```mermaid
flowchart LR
  Snapshot --> Addr["Address, Subnet, PublicKey, MTU"]
  Snapshot --> RST["RSTDropped"]
  Snapshot --> Peers["GetPeers() → []PeerSnapshotObj"]
  Snapshot --> Active["PeerManagerActive() → []string"]
  Snapshot --> SOCKS["IsEnabled, Addr, IsUnix"]
```

Возвращает `SnapshotObj` с JSON-тегами — можно сериализовать напрямую для `/status` или `/metrics`.

### Close

```go
func (o *Obj) Close() error
```

Останавливает все компоненты. Идемпотентный (`sync.Once`) — безопасен для повторного и конкурентного
вызова.

Порядок shutdown:

```mermaid
flowchart TD
  Close --> PM["peermgr.Stop()"]
  PM --> S1["Disable SOCKS"]
  S1 --> S15["ninfo.Close()"]
  S15 --> S2["core.Close() — multicast, admin, listeners, core.Stop, NIC, gVisor"]
  S2 --> Done["Завершено"]
```

Собирает ошибки всех компонентов через `errors.Join`.

---

## Конфигурация

### ConfigObj

Параметры создания узла.

| Поле              | Тип                  | По умолчанию | Описание                                                                           |
|-------------------|----------------------|--------------|------------------------------------------------------------------------------------|
| `Ctx`             | `context.Context`    | `nil`        | Родительский контекст; при отмене — автоматический `Close()`. `nil` — ручное       |
| `Config`          | `*config.NodeConfig` | `nil`        | Конфигурация Yggdrasil. `nil` — случайные ключи                                    |
| `Logger`          | `yggcore.Logger`     | `nil`        | Логгер. `nil` — логи отбрасываются                                                 |
| `CoreStopTimeout` | `time.Duration`      | `0`          | Таймаут `core.Stop()`. `0` — без ограничений                                       |
| `Peers`           | `*peermgr.ConfigObj` | `nil`        | Менеджер пиров. `nil` — пиры из `Config.Peers`. Не `nil` + `Config.Peers` → ошибка |
| `Sigils`          | `[]sigils.Interface` | `nil`        | Сигилы для NodeInfo. `nil` — не используются. Комбинируется с `Config.NodeInfo`    |

### SOCKSConfigObj

Параметры SOCKS5-прокси.

| Поле             | Тип    | По умолчанию | Описание                                                                |
|------------------|--------|--------------|-------------------------------------------------------------------------|
| `Addr`           | string | обязательное | TCP `"127.0.0.1:1080"` или Unix `"/tmp/ygg.sock"` (`/` или `.` — Unix)  |
| `Nameserver`     | string | `""`         | DNS в сети Yggdrasil. `"[ipv6]:port"`. Пустая строка — только `.pk.ygg` |
| `Verbose`        | bool   | `false`      | Логирование каждого SOCKS-соединения                                    |
| `MaxConnections` | int    | `0`          | Лимит одновременных соединений. `0` — без ограничений                   |

---

## Типы снимков

### SnapshotObj

| Поле          | Тип                 | Описание                                 |
|---------------|---------------------|------------------------------------------|
| `Address`     | `string`            | IPv6-адрес узла                          |
| `Subnet`      | `string`            | Подсеть `/64`                            |
| `PublicKey`   | `string`            | Публичный ключ ed25519 (hex)             |
| `MTU`         | `uint64`            | MTU стека                                |
| `RSTDropped`  | `int64`             | Счётчик отброшенных RST-пакетов          |
| `Peers`       | `[]PeerSnapshotObj` | Состояние каждого пира                   |
| `ActivePeers` | `[]string`          | Пиры, выбранные менеджером (`omitempty`) |
| `SOCKS`       | `SOCKSSnapshotObj`  | Состояние SOCKS5-прокси                  |

### PeerSnapshotObj

| Поле            | Тип             | Описание                       |
|-----------------|-----------------|--------------------------------|
| `URI`           | `string`        | URI подключения                |
| `Up`            | `bool`          | Подключён                      |
| `Inbound`       | `bool`          | Входящее подключение           |
| `Key`           | `string`        | Публичный ключ пира (hex)      |
| `Latency`       | `time.Duration` | Задержка                       |
| `Cost`          | `uint64`        | Стоимость маршрута             |
| `RXBytes`       | `uint64`        | Принято байт                   |
| `TXBytes`       | `uint64`        | Отправлено байт                |
| `Uptime`        | `time.Duration` | Время подключения              |
| `LastError`     | `string`        | Последняя ошибка (`omitempty`) |
| `LastErrorTime` | `time.Time`     | Время последней ошибки         |

### SOCKSSnapshotObj

| Поле      | Тип    | Описание                 |
|-----------|--------|--------------------------|
| `Enabled` | `bool` | Прокси запущен           |
| `Addr`    | string | Адрес (`omitempty`)      |
| `IsUnix`  | `bool` | Unix-сокет (`omitempty`) |

---

## Ошибки

| Переменная                 | Описание                                              |
|----------------------------|-------------------------------------------------------|
| `ErrPeersConflict`         | `Config.Peers` и `Peers` менеджер заданы одновременно |
| `ErrPeerManagerNotEnabled` | Вызван метод менеджера пиров, но менеджер не включён  |

Ошибки из `core.Interface` (`ErrNotAvailable` и т.д.) описаны в [mod/core/README.md](mod/core/README.md).

---

## Потокобезопасность

Все публичные методы `Obj` безопасны для конкурентного использования.

| Метод / группа                          | Гарантия                                                     |
|-----------------------------------------|--------------------------------------------------------------|
| `DialContext`, `Listen`, `ListenPacket` | Потокобезопасны; netstack через `atomic.Pointer`             |
| `EnableSOCKS` / `DisableSOCKS`          | Защищены мьютексом                                           |
| `EnableMulticast` / `EnableAdmin`       | Защищены `sync.RWMutex`                                      |
| `AddPeer` / `RemovePeer`                | Делегируют в `yggdrasil-go/core` (потокобезопасно)           |
| `PeerManagerActive`                     | Возвращает копию; защищён мьютексом                          |
| `PeerManagerOptimize`                   | Блокирует; сериализован через `optimizeMu`                   |
| `Ask` / `AskAddr`                       | Потокобезопасны; сетевой вызов в горутине, отменяется по ctx |
| `Close`                                 | Идемпотентный (`sync.Once`)                                  |
| `Snapshot`                              | Потокобезопасен; собирает данные из потокобезопасных методов |

---

## Жизненный цикл

```mermaid
flowchart TD
  START([Создание]) --> NEW["ratatoskr.New(cfg)"]
  NEW --> SG{Sigils задан?}
  SG -->|Да| SC["sigil_core.New() → NodeInfo"]
  SG -->|Нет| CORE
  SC --> CORE["core.New() — Yggdrasil + netstack + NIC"]
  CORE --> NI["ninfo.New()"]
  NI --> PM{Peers задан?}
  PM -->|Да| PMSTART["peermgr.New() + Start()"]
  PM -->|Нет| READY
  PMSTART --> READY([Узел готов])
  READY -->|опционально| SOCKS["EnableSOCKS()"]
  READY -->|опционально| MC["EnableMulticast()"]
  READY -->|опционально| ADM["EnableAdmin()"]
  READY -->|опционально| PEERS["AddPeer() / RemovePeer()"]
  READY -->|опционально| ASK["Ask() / AskAddr()"]
  SOCKS --> READY
  MC --> READY
  ADM --> READY
  PEERS --> READY
  ASK --> READY
  READY --> CLOSE["Close()"]
  CLOSE --> S1["peermgr.Stop()"]
  S1 --> S2["Disable SOCKS"]
  S2 --> S25["ninfo.Close()"]
  S25 --> S3["core.Close()"]
  S3 --> DONE([Завершено])
```

Три способа завершения:

```go
// 1. Явный Close()
defer node.Close()

// 2. Через контекст
ctx, cancel := context.WithCancel(context.Background())
node, _ = ratatoskr.New(ratatoskr.ConfigObj{Ctx: ctx})
cancel() // → Close() автоматически

// 3. По сигналу ОС
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
node, _ = ratatoskr.New(ratatoskr.ConfigObj{Ctx: ctx})
<-ctx.Done()
```

---

## Примеры использования

### HTTP-клиент

```go
client := &http.Client{
Transport: &http.Transport{
DialContext: node.DialContext,
},
}

resp, err := client.Get("http://[200:abcd::1]:8080/api/v1/status")
```

### TCP-сервер

```go
ln, err := node.Listen("tcp", ":8080")
if err != nil {
log.Fatal(err)
}
defer ln.Close()

fmt.Printf("http://[%s]:8080/\n", node.Address())
http.Serve(ln, handler)
```

### UDP

```go
pc, err := node.ListenPacket("udp", ":9000")
if err != nil {
log.Fatal(err)
}
defer pc.Close()

buf := make([]byte, 1500)
for {
n, addr, err := pc.ReadFrom(buf)
if err != nil {
break
}
pc.WriteTo(buf[:n], addr)
}
```

### SOCKS5-прокси

```go
err = node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
Addr:           "127.0.0.1:1080",
Nameserver:     "[200:abcd::1]:53",
Verbose:        true,
MaxConnections: 128,
})
defer node.DisableSOCKS()

// curl --proxy socks5h://127.0.0.1:1080 http://example.pk.ygg/
```

Unix-сокет:

```go
err = node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
Addr: "/tmp/ygg-socks.sock",
})
```

### Split proxy (Yggdrasil + напрямую)

SOCKS5-прокси, который маршрутизирует Yggdrasil-адреса (`200::/7`) через ноду,
а всё остальное — напрямую через обычную сеть:

```go
import (
"context"
"net"

"github.com/voluminor/ratatoskr/mod/resolver"
"github.com/voluminor/ratatoskr/mod/socks"
)

// split dialer: Yggdrasil-адреса → нода, остальное → напрямую
dial := func (ctx context.Context, network, addr string) (net.Conn, error) {
host, _, _ := net.SplitHostPort(addr)
if ip := net.ParseIP(host); ip != nil && ip[0]&0xfe == 0x02 { // 200::/7
return node.DialContext(ctx, network, addr)
}
return (&net.Dialer{}).DialContext(ctx, network, addr)
}

srv := socks.New(dialerFunc(dial))
srv.Enable(socks.EnableConfigObj{
Addr:     "127.0.0.1:1080",
Resolver: resolver.New(node, "[200:abcd::1]:53"), // DNS через Yggdrasil
Logger:   logger,
})
defer srv.Disable()

// dialerFunc адаптирует функцию в proxy.ContextDialer
type dialerFunc func (ctx context.Context, network, addr string) (net.Conn, error)

func (f dialerFunc) DialContext(ctx context.Context, n, a string) (net.Conn, error) {
return f(ctx, n, a)
}
```

Можно использовать как system-wide SOCKS5-прокси — обычный интернет-трафик проходит
без изменений, только Yggdrasil-адреса маршрутизируются через ноду:

```bash
# Yggdrasil IPv6 — через ноду
curl --proxy socks5h://127.0.0.1:1080 http://[200:b0aa:c535:89fb:4c73:bbd:c30b:2665]/

# .pk.ygg домен — resolver конвертирует в 200::/7, затем через ноду
curl --proxy socks5h://127.0.0.1:1080 http://a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924.pk.ygg/

# Обычный интернет — напрямую, минуя Yggdrasil
curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

### Менеджер пиров

```go
node, err := ratatoskr.New(ratatoskr.ConfigObj{
Ctx: ctx,
Peers: &peermgr.ConfigObj{
Peers: []string{
"tls://peer1.example.com:17117",
"tls://peer2.example.com:17117",
"quic://peer3.example.com:17117",
},
ProbeTimeout:    10 * time.Second,
RefreshInterval: 5 * time.Minute,
MaxPerProto:     1,
BatchSize:       2,
},
})

active := node.PeerManagerActive()
node.PeerManagerOptimize() // принудительная переоценка
```

Пассивный режим (добавить всех без выбора):

```go
Peers: &peermgr.ConfigObj{
Peers:       peers,
MaxPerProto: -1,
}
```

### Snapshot → JSON

```go
snap := node.Snapshot()
data, _ := json.MarshalIndent(snap, "", "  ")
fmt.Println(string(data))
```

### Multicast и Admin

```go
// mDNS-обнаружение пиров в локальной сети
if err := node.EnableMulticast(logger); err != nil {
log.Fatal(err)
}
defer node.DisableMulticast()

// Admin-сокет
if err := node.EnableAdmin("unix:///tmp/ygg-admin.sock"); err != nil {
log.Fatal(err)
}
defer node.DisableAdmin()
```

### Адаптер логгера для slog

```go
type slogAdapter struct{ l *slog.Logger }

func (a slogAdapter) Infof(f string, v ...interface{})  { a.l.Info(fmt.Sprintf(f, v...)) }
func (a slogAdapter) Infoln(v ...interface{})           { a.l.Info(fmt.Sprint(v...)) }
func (a slogAdapter) Warnf(f string, v ...interface{})  { a.l.Warn(fmt.Sprintf(f, v...)) }
func (a slogAdapter) Warnln(v ...interface{})           { a.l.Warn(fmt.Sprint(v...)) }
func (a slogAdapter) Errorf(f string, v ...interface{}) { a.l.Error(fmt.Sprintf(f, v...)) }
func (a slogAdapter) Errorln(v ...interface{})          { a.l.Error(fmt.Sprint(v...)) }
func (a slogAdapter) Debugf(f string, v ...interface{}) { a.l.Debug(fmt.Sprintf(f, v...)) }
func (a slogAdapter) Debugln(v ...interface{})          { a.l.Debug(fmt.Sprint(v...)) }
func (a slogAdapter) Printf(f string, v ...interface{}) { a.l.Info(fmt.Sprintf(f, v...)) }
func (a slogAdapter) Println(v ...interface{})          { a.l.Info(fmt.Sprint(v...)) }
func (a slogAdapter) Traceln(v ...interface{})          {}

node, _ := ratatoskr.New(ratatoskr.ConfigObj{
Logger: slogAdapter{l: slog.Default()},
})
```

---

## Модули

| Модуль                                   | Описание                                                             |
|------------------------------------------|----------------------------------------------------------------------|
| [`mod/core`](mod/core/README.md)         | Ядро: узел Yggdrasil, netstack, NIC, multicast, admin                |
| [`mod/peermgr`](mod/peermgr/README.md)   | Менеджер пиров: пробинг, выбор лучших, ротация, MinPeers watch       |
| [`mod/socks`](mod/socks/README.md)       | SOCKS5-прокси (TCP/Unix), лимит соединений                           |
| [`mod/resolver`](mod/resolver/README.md) | Резолвер: `.pk.ygg`, IP-литералы, DNS через Yggdrasil                |
| [`mod/forward`](mod/forward/README.md)   | TCP/UDP-форвардинг между локальной сетью и Yggdrasil                 |
| [`mod/probe`](mod/probe/README.md)       | Исследование топологии (BFS), трассировка маршрутов                  |
| [`mod/settings`](mod/settings/README.md) | Загрузка, парсинг и сохранение конфигурации                          |
| [`mod/sigils`](mod/sigils/README.md)     | Типизированные блоки NodeInfo (info, services, public, inet)         |
| [`mod/ninfo`](mod/ninfo/README.md)       | Запрос и парсинг NodeInfo удалённых узлов, управление parse-сигилами |

---

## Примеры приложений

Готовые примеры — в [`cmd/embedded/`](cmd/embedded/):

| Пример                                | Описание                      |
|---------------------------------------|-------------------------------|
| [`http`](cmd/embedded/http)           | HTTP-сервер в сети Yggdrasil  |
| [`tiny-http`](cmd/embedded/tiny-http) | Минимальный HTTP-сервер       |
| [`tiny-chat`](cmd/embedded/tiny-chat) | Чат через Yggdrasil           |
| [`mobile`](cmd/embedded/mobile)       | Пример для мобильных платформ |

Также в [`cmd/yggstack/`](cmd/yggstack/) — реализация yggstack на базе ratatoskr.

---

## Поддерживаемые платформы

Тесты запускаются на Linux (amd64, arm64), macOS (arm64) и Windows (amd64).
Кросс-сборка проверяется на каждый PR для **25 целей**:

| ОС      | Архитектуры                                                                                     |
|---------|-------------------------------------------------------------------------------------------------|
| Linux   | amd64, arm64, armv7, armv6, 386, riscv64, mips64, mips64le, mips, mipsle, ppc64, ppc64le, s390x |
| Windows | amd64, arm64, 386                                                                               |
| macOS   | amd64, arm64                                                                                    |
| FreeBSD | amd64, arm64, 386                                                                               |
| OpenBSD | amd64, arm64                                                                                    |
| NetBSD  | amd64, arm64                                                                                    |
