# ratatoskr

Go-библиотека для встраивания узла Yggdrasil в приложения. Предоставляет стандартные Go-сетевые примитивы
(`DialContext`, `Listen`, `ListenPacket`) поверх userspace TCP/IP стека (gVisor netstack), без необходимости
создавать TUN-интерфейс или получать root-права.

## Архитектура

```mermaid
graph TB
    App[Приложение]

subgraph ratatoskr
Obj[ratatoskr.Obj]
        SOCKS[SOCKS5-прокси]
        Resolver[Резолвер .pk.ygg / DNS]
PeerMgr[PeerManager — выбор пиров]
    end

subgraph core
CoreObj[core.Obj]
Netstack[netstack — userspace TCP/UDP]
NIC[NIC — мост пакетов]
Multicast[Multicast — mDNS обнаружение]
Admin[Admin — управляющий сокет]
end

subgraph external [Внешние зависимости]
YggCore[yggdrasil-go/core]
gVisor[gVisor netstack]
end

App --> Obj
Obj --> CoreObj
Obj --> SOCKS
Obj --> PeerMgr
SOCKS --> Resolver
SOCKS -->|DialContext|CoreObj
Resolver -->|DialContext для DNS|CoreObj
PeerMgr -->|AddPeer / RemovePeer|CoreObj

CoreObj --> Netstack
CoreObj --> Multicast
CoreObj --> Admin
Netstack --> NIC
NIC -->|IPv6 - пакеты|YggCore
Netstack --> gVisor
```

## Путь пакета

Как данные проходят через стек — от приложения до Yggdrasil-сети и обратно:

```mermaid
sequenceDiagram
    participant App as Приложение
    participant NS as Netstack (gVisor)
    participant NIC as NIC (мост)
    participant Ygg as Yggdrasil Core
    Note over App, Ygg: Исходящий пакет (Dial / Write)
    App ->> NS: DialContext("tcp", "[ipv6]:port")
    NS ->> NIC: WritePackets(IPv6-пакет)
    NIC ->> Ygg: ipv6rwc.Write(raw bytes)
    Ygg -->> Ygg: Маршрутизация через оверлейную сеть
    Note over App, Ygg: Входящий пакет (Listen / Read)
    Ygg ->> NIC: ipv6rwc.Read(raw bytes)
    NIC ->> NS: DeliverNetworkPacket(IPv6)
    NS ->> App: net.Conn.Read(data)
```

## Внутренняя архитектура NIC

NIC (`nicObj`) — мост между gVisor и Yggdrasil на уровне IPv6-пакетов.

```mermaid
graph LR
    subgraph nicObj
        ReadLoop[Read goroutine]
        RSTLoop[RST goroutine]
        RSTQueue[RST queue<br/>chan, настраиваемый размер]
        WriteBufPool[sync.Pool<br/>65535 bytes, fallback]
    end

    YGG[ipv6rwc] -->|Read| ReadLoop
    ReadLoop -->|DeliverNetworkPacket| GV[gVisor stack]
    GV -->|WritePackets| WP[writePacket]
    WP -->|Write| YGG
    GV -->|TCP RST| RSTQueue
    RSTQueue --> RSTLoop
    RSTLoop -->|writePacket| YGG
```

**Обработка TCP RST:** пакеты RST без payload отправляются не напрямую, а через буферизированную очередь
(`chan *PacketBuffer`). Размер очереди задаётся через `core.ConfigObj.RSTQueueSize` (по умолчанию 100).
Счётчик отброшенных RST-пакетов доступен через `core.Obj.RSTDropped()`.

**Стратегия при переполнении RST-очереди:**

1. Попытка отправить в канал
2. Если канал полон — вытеснение самого старого пакета с debug-логированием
3. Повторная попытка отправить
4. Если снова неудача — пакет отбрасывается, инкрементируется счётчик дропов, debug-логирование

**Запись пакетов (writePacket):** используется zero-copy через `AsViewList` — данные пакета передаются
в `ipv6rwc.Write` напрямую без копирования. Если пакет состоит из нескольких View (редкий случай),
данные собираются в буфер из `sync.Pool`. Паника в `WritePackets` перехватывается через `recover`
и логируется без краша всего стека.

## Структура модуля

```mermaid
graph LR
subgraph "ratatoskr (корневой пакет)"
A[Obj — фасад]
B[ConfigObj]
C[SOCKSConfigObj]
end

subgraph "core"
D[Obj — узел Yggdrasil]
E[Interface — контракт]
F[netstackObj — TCP/UDP стек]
G[nicObj — LinkEndpoint]
H[componentObj — lifecycle]
end

subgraph "peermgr"
PM[Obj — менеджер пиров]
SEL[selector — выбор лучших]
end

subgraph "resolver"
I[Obj — резолвер имён]
end

subgraph "socks"
J[Obj — SOCKS5-сервер]
K[Interface — контракт]
end

A -->|встраивает|E
A -->|использует|J
A -->|создаёт|I
A -->|опционально|PM
PM -->|AddPeer/RemovePeer|E
PM --> SEL
D -->|реализует|E
D -->|содержит|F
F -->|содержит|G
D -->|содержит|H
J -->|реализует|K
```

## Пакеты

### `ratatoskr` (корневой)

Фасад для встраивания. Объединяет ядро, SOCKS-прокси, резолвер и менеджер пиров в одну точку входа.

| Тип              | Назначение                                                              |
|------------------|-------------------------------------------------------------------------|
| `Obj`            | Узел с полным набором возможностей: сетевые методы + SOCKS + управление |
| `ConfigObj`      | Контекст, конфиг Yggdrasil, логгер, таймаут, менеджер пиров             |
| `SOCKSConfigObj` | Адрес прокси, DNS-сервер, verbose, лимит соединений                     |

### `core`

Ядро — узел Yggdrasil с userspace сетевым стеком.

| Тип            | Назначение                                                                   |
|----------------|------------------------------------------------------------------------------|
| `Obj`          | Узел: DialContext, Listen, ListenPacket, управление пирами, multicast, admin |
| `Interface`    | Публичный контракт — всё, что нужно внешнему коду                            |
| `netstackObj`  | gVisor TCP/UDP/ICMP стек                                                     |
| `nicObj`       | Мост между gVisor и Yggdrasil на уровне IPv6-пакетов                         |
| `componentObj` | Обобщённый Enable/Disable lifecycle для multicast и admin                    |

### `peermgr`

Менеджер пиров — автоматический выбор и поддержание оптимального набора пиров.

| Тип         | Назначение                                                |
|-------------|-----------------------------------------------------------|
| `Obj`       | Менеджер: пробинг, выбор лучших, периодическое обновление |
| `ConfigObj` | Параметры: список кандидатов, таймауты, стратегия выбора  |

**Режимы `MaxPerProto`:**

| Значение  | Поведение                                                                    |
|-----------|------------------------------------------------------------------------------|
| `0` / `1` | Один лучший пир на протокол (по умолчанию)                                   |
| `N > 1`   | Топ-N пиров на протокол, отсортированных по латентности                      |
| `-1`      | Пассивный режим: добавить всех кандидатов без выбора; пробинг не выполняется |

**Логика `optimizeActive`:**

```mermaid
flowchart TD
    ADD[AddPeer для всех кандидатов] --> WAIT[Ожидание ProbeTimeout]
    WAIT --> BUILD[buildResults: сопоставить с GetPeers]
    BUILD --> SELECT[selectBest: топ-N по протоколу]
    SELECT --> REMOVE[RemovePeer для проигравших]
    REMOVE --> STORE[Сохранить active список]
```

### `resolver`

Резолвер имён с тремя стратегиями:

```mermaid
flowchart TD
    Input[Входное имя]
    Input --> PK{Суффикс .pk.ygg?}
    PK -->|Да| HEX[Декодировать hex публичного ключа]
    HEX --> ADDR[Вычислить IPv6 из ключа]
    PK -->|Нет| IP{Это IPv6-литерал?}
    IP -->|Да| PASS[Вернуть как есть]
    IP -->|Нет| NS{Nameserver настроен?}
    NS -->|Нет| ERR[Ошибка: no nameserver configured]
    NS -->|Да| DNS[DNS-запрос через Yggdrasil]
    DNS --> RESULT[Первый AAAA-адрес]
```

**Формат `.pk.ygg`:** `<hex-pubkey>.pk.ygg` или `subdomain.<hex-pubkey>.pk.ygg`
(при наличии поддоменов используется последний сегмент перед `.pk.ygg`).
Публичный ключ — 32 байта ed25519 в hex (64 символа).

**DNS через Yggdrasil:** если настроен `Nameserver`, DNS-запросы (`AAAA`) идут через `DialContext` ядра —
трафик не утекает в системный резолвер. Без nameserver резолвинг DNS-имён возвращает ошибку.

### `socks`

SOCKS5-прокси поверх Yggdrasil. Поддерживает TCP и Unix-сокеты. Без аутентификации.

```mermaid
stateDiagram-v2
    [*] --> Created: New()
    Created --> Enabled: Enable()
    Enabled --> Created: Disable()
    Enabled --> Enabled: Enable() → ошибка
    Created --> Created: Disable() → no-op
```

## Конфигурация

### ConfigObj (ratatoskr)

| Поле              | Тип                  | По умолчанию | Описание                                                                                                                                    |
|-------------------|----------------------|--------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| `Ctx`             | `context.Context`    | `nil`        | Родительский контекст; при отмене узел автоматически вызывает `Close()`. `nil` — ручное управление                                          |
| `Config`          | `*config.NodeConfig` | `nil`        | Конфигурация Yggdrasil (ключи, listen-адреса). `nil` — генерируются случайные ключи. `Config.Peers` должен быть пустым если задан `Peers`   |
| `Logger`          | `yggcore.Logger`     | `nil`        | Логгер; `nil` — логи отбрасываются (noop). Передаётся и в ядро, и в SOCKS, и в менеджер пиров                                               |
| `CoreStopTimeout` | `time.Duration`      | `0`          | Таймаут `core.Stop()` при завершении. `0` — ожидание без ограничений                                                                        |
| `Peers`           | `*peermgr.ConfigObj` | `nil`        | Менеджер пиров. `nil` — пиры берутся из `Config.Peers` как в стандартном Yggdrasil. Не `nil` + `Config.Peers` непустой — ошибка при `New()` |

### ConfigObj (peermgr)

| Поле              | Тип              | По умолчанию | Описание                                                                         |
|-------------------|------------------|--------------|----------------------------------------------------------------------------------|
| `Peers`           | `[]string`       | обязательное | Список URI-кандидатов: `"tls://host:port"`, `"tcp://..."`, `"quic://..."` и т.д. |
| `ProbeTimeout`    | `time.Duration`  | `10s`        | Ожидание подключения при пробинге. Игнорируется при `MaxPerProto == -1`          |
| `RefreshInterval` | `time.Duration`  | `0`          | Интервал автоматической перепроверки. `0` — только при запуске                   |
| `MaxPerProto`     | `int`            | `1`          | Число лучших пиров на протокол. `-1` — пассивный режим                           |
| `Logger`          | `yggcore.Logger` | `nil`        | Логгер; `nil` — берётся из родительского `ConfigObj.Logger`                      |

### ConfigObj (core)

| Поле              | Тип                  | По умолчанию | Описание                                         |
|-------------------|----------------------|--------------|--------------------------------------------------|
| `Config`          | `*config.NodeConfig` | `nil`        | Конфигурация Yggdrasil. `nil` — случайные ключи  |
| `Logger`          | `yggcore.Logger`     | `nil`        | Логгер; `nil` — noop                             |
| `CoreStopTimeout` | `time.Duration`      | `0`          | Таймаут `core.Stop()`. `0` — без ограничений     |
| `RSTQueueSize`    | `int`                | `100`        | Размер очереди отложенных RST-пакетов. `0` → 100 |

### SOCKSConfigObj (ratatoskr)

| Поле             | Тип    | По умолчанию | Описание                                                                                                                    |
|------------------|--------|--------------|-----------------------------------------------------------------------------------------------------------------------------|
| `Addr`           | string | обязательное | Адрес прокси: TCP `"127.0.0.1:1080"` или Unix-сокет `"/tmp/ygg.sock"`. Путь, начинающийся с `/` или `.` — Unix              |
| `Nameserver`     | string | `""`         | DNS-сервер в сети Yggdrasil. Формат: `"[ipv6]:port"`. Пустая строка — только `.pk.ygg` и IP-литералы                        |
| `Verbose`        | bool   | `false`      | Подробное логирование каждого SOCKS-соединения                                                                              |
| `MaxConnections` | int    | `0`          | Максимум одновременных соединений. `0` — без ограничений. При достижении лимита новые соединения ожидают освобождения слота |

## Валидация адресов

Сетевые методы (`DialContext`, `Listen`, `ListenPacket`) принимают адреса в формате `"[ipv6]:port"` или `":port"`.

| Ввод                | Поведение                                     |
|---------------------|-----------------------------------------------|
| `"[200:abc::1]:80"` | Валидный IPv6 + порт                          |
| `":8080"`           | Пустой host — bind на все адреса (для Listen) |
| `":0"`              | Эфемерный порт (назначается ОС)               |
| `"localhost:80"`    | Ошибка: `invalid IP address "localhost"`      |
| `"[::1]:99999"`     | Ошибка: `port 99999 out of range 0-65535`     |
| `"bad"`             | Ошибка: `net.SplitHostPort` failed            |

Поддерживаемые сети: `tcp`, `tcp6` (для Dial/Listen), `udp`, `udp6` (для Dial/ListenPacket).

## Rate limiting (SOCKS)

При установленном `MaxConnections > 0` прокси ограничивает число одновременных соединений через семафор:

```mermaid
flowchart TD
   WAIT[Ожидание слота в семафоре] --> ACCEPT[Accept соединение]
   ACCEPT --> SERVE[Обслужить через SOCKS5]
   SERVE --> CLOSE[Закрыть соединение] --> FREE[Освободить слот] --> WAIT
```

- Блокировка на семафоре **до** `Accept` — backpressure передаётся на TCP backlog ядра
- При заполненном backlog ядро само отбрасывает SYN — нет busy-loop на уровне приложения
- Слот семафора освобождается ровно один раз при закрытии соединения (`sync.Once`)

## Потокобезопасность

Все публичные методы `Obj` и `core.Obj` безопасны для конкурентного использования.

| Метод / группа                          | Гарантия                                                                            |
|-----------------------------------------|-------------------------------------------------------------------------------------|
| `DialContext`, `Listen`, `ListenPacket` | Потокобезопасны; netstack защищён через `atomic.Pointer`                            |
| `EnableSOCKS` / `DisableSOCKS`          | Защищены мьютексом; повторный `Enable` без `Disable` — ошибка                       |
| `EnableMulticast` / `DisableMulticast`  | Защищены `sync.RWMutex`; повторный `Enable` — ошибка                                |
| `EnableAdmin` / `DisableAdmin`          | Аналогично multicast                                                                |
| `AddPeer` / `RemovePeer`                | Потокобезопасны (делегируют в `yggdrasil-go/core`)                                  |
| `PeerManagerActive`                     | Защищён мьютексом внутри `peermgr.Obj`; возвращает копию списка                     |
| `PeerManagerOptimize`                   | Блокирует вызывающую горутину до завершения пробинга                                |
| `Close`                                 | Идемпотентный (`sync.Once`); безопасен для повторного вызова и конкурентного вызова |
| `Address`, `Subnet`, `PublicKey`, `MTU` | Потокобезопасны; только чтение                                                      |

**Конкурентный Enable multicast + admin:** обработчики admin регистрируются атомарно через отдельный
`handlersMu` мьютекс после завершения `enable()`, что исключает ABBA-deadlock между компонентами.

## Обработка ошибок

### Методы возвращающие ошибки

| Метод                 | Ошибки                                                                         |
|-----------------------|--------------------------------------------------------------------------------|
| `New`                 | Ошибка создания core, конфликт `Config.Peers` + `Peers`, ошибка старта peermgr |
| `DialContext`         | `ErrNotAvailable` (узел закрыт), ошибки gVisor, невалидный адрес               |
| `Listen`              | `ErrNotAvailable`, ошибки gVisor, невалидный адрес                             |
| `ListenPacket`        | `ErrNotAvailable`, ошибки gVisor, невалидный адрес                             |
| `EnableSOCKS`         | `"SOCKS already enabled"`, ошибка listen (занят порт / невалидный путь)        |
| `DisableSOCKS`        | Ошибка закрытия listener                                                       |
| `EnableMulticast`     | `"multicast already enabled"`, невалидный regex, ошибка `multicast.New`        |
| `EnableAdmin`         | `"admin already enabled"`, невалидный адрес, `admin.New` вернул nil            |
| `AddPeer`             | Невалидный URI, ошибка ядра                                                    |
| `RemovePeer`          | Невалидный URI, ошибка ядра                                                    |
| `PeerManagerOptimize` | `"peermgr: not running"` если менеджер не запущен                              |
| `Close`               | Всегда `nil`; ошибки компонентов логируются через `Warnf`, но не возвращаются  |

### ErrNotAvailable

Возвращается из `DialContext`, `Listen`, `ListenPacket` если netstack уже уничтожен (после `Close()`).

### Unix-сокет (SOCKS)

При запуске на Unix-сокете обрабатывается случай устаревшего файла:

```mermaid
flowchart TD
    LISTEN[net.Listen unix] --> OK{Успех?}
    OK -->|Да| DONE[Готово]
    OK -->|Нет| INUSE{EADDRINUSE?}
    INUSE -->|Нет| FAIL[Ошибка]
    INUSE -->|Да| PROBE[Dial к сокету]
    PROBE --> ALIVE{Ответ?}
    ALIVE -->|Да| FAIL2[another instance is listening]
    ALIVE -->|Нет| SYMLINK{Символическая ссылка?}
    SYMLINK -->|Да| FAIL3[refusing to remove: is a symlink]
    SYMLINK -->|Нет| REMOVE[Удалить файл] --> RETRY[Повторный Listen]
```

## Жизненный цикл

```mermaid
flowchart TD
    START([Создание]) --> NEW[ratatoskr.New]
    NEW --> CORE[Запуск Yggdrasil Core]
    CORE --> NS[Создание netstack + NIC]
    NS --> GOROUTINES[Запуск goroutine: read + RST]
    GOROUTINES --> ROUTE[Маршрут 0200::/7]
    ROUTE --> PMCHECK{Peers задан?}
    PMCHECK -->|Да| PMSTART[peermgr.Start — пробинг асинхронно]
    PMCHECK -->|Нет| READY
    PMSTART --> READY([Узел готов])
    READY -->|опционально| SOCKS[EnableSOCKS]
    READY -->|опционально| MC[EnableMulticast]
    READY -->|опционально| ADM[EnableAdmin]
    READY -->|опционально| PEER[AddPeer / RemovePeer]
    SOCKS --> READY
    MC --> READY
    ADM --> READY
    PEER --> READY
    READY --> CLOSE[Close]
    CLOSE --> S0[peermgr.Stop — RemovePeer активных]
    S0 --> S1[Disable SOCKS]
    S1 --> S2[Disable Multicast + Admin]
    S2 --> S3[Закрыть listeners]
    S3 --> S4[core.Stop]
    S4 --> S5[Закрыть NIC: done → ipv6rwc.Close → wait goroutines]
    S5 --> S6[Уничтожить gVisor stack]
    S6 --> DONE([Завершено])
```

### Порядок shutdown (Close)

1. **peermgr.Stop()** — отмена контекста пробинга, ожидание горутин, `RemovePeer` всех активных пиров
2. **Disable SOCKS** — закрытие listener останавливает `Serve`, `wg.Wait()` ждёт завершения. Unix-сокет удаляется
3. **Disable Multicast + Admin** — вызов `stopFn()` каждого компонента
4. **Закрытие listeners** — все listener'ы, созданные через `Listen`/`ListenPacket`, закрываются
5. **core.Stop()** — остановка Yggdrasil core. Разблокирует `ipv6rwc.Read()` в NIC
6. **NIC Close** — `close(done)` сигнализирует горутинам, `ipv6rwc.Close()`, ожидание `readDone` и `rstDone`, очистка
   RST-очереди, `RemoveNIC`
7. **stack.Destroy()** — уничтожение gVisor стека

При наличии `CoreStopTimeout > 0`: если `core.Stop()` не завершился за указанное время,
логируется предупреждение и shutdown продолжается.

### Автозавершение через контекст

Если передан `Ctx` в `ConfigObj`, горутина слушает отмену контекста и вызывает `Close()` автоматически.
При ручном `Close()` горутина завершается через канал `done`.

## Быстрый старт

```go
package main

import (
    "context"
    "fmt"
    "net/http"

    "github.com/voluminor/ratatoskr"
)

func main() {
    node, err := ratatoskr.New(ratatoskr.ConfigObj{
        Ctx: context.Background(),
    })
    if err != nil {
        panic(err)
    }
    defer node.Close()

    fmt.Println("Адрес:", node.Address())

    client := &http.Client{
        Transport: &http.Transport{
            DialContext: node.DialContext,
        },
    }

    resp, err := client.Get("http://[200:abcd::1]:8080/")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
}
```

## Менеджер пиров

```go
import (
"github.com/voluminor/ratatoskr"
"github.com/voluminor/ratatoskr/mod/peermgr"
)

node, err := ratatoskr.New(ratatoskr.ConfigObj{
Ctx: ctx,
Peers: &peermgr.ConfigObj{
Peers: []string{
"tls://peer1.example.com:17117",
"tls://peer2.example.com:17117",
"quic://peer3.example.com:17117",
},
ProbeTimeout:    10 * time.Second, // ожидание при пробинге
RefreshInterval: 5 * time.Minute, // переперобировать каждые 5 минут
MaxPerProto:     1, // один лучший пир на протокол
},
})

// Текущие активные пиры (выбранные менеджером)
active := node.PeerManagerActive()

// Внеплановая перепроверка (блокирует)
err = node.PeerManagerOptimize()
```

**Пассивный режим** (`MaxPerProto: -1`) — добавить всех кандидатов без выбора лучшего,
поведение идентично стандартному Yggdrasil с `Config.Peers`:

```go
Peers: &peermgr.ConfigObj{
Peers:           peers,
MaxPerProto:     -1,
RefreshInterval: 10 * time.Minute, // переподключать весь список целиком
},
```

## Запуск с SOCKS5-прокси

```go
err = node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
Addr:           "127.0.0.1:1080",
Nameserver:     "[200:abcd::1]:53",
Verbose:        true,
MaxConnections: 128,
})
if err != nil {
panic(err)
}
defer node.DisableSOCKS()

// curl --proxy socks5h://127.0.0.1:1080 http://example.pk.ygg/
```

## TCP-сервер в сети Yggdrasil

```go
ln, err := node.Listen("tcp", ":8080")
if err != nil {
panic(err)
}
defer ln.Close()

fmt.Printf("Слушаю на http://[%s]:8080/\n", node.Address())
http.Serve(ln, handler)
```

## UDP в сети Yggdrasil

```go
pc, err := node.ListenPacket("udp", ":9000")
if err != nil {
panic(err)
}
defer pc.Close()

buf := make([]byte, 1500)
n, addr, err := pc.ReadFrom(buf)
```

## Управление пирами в runtime

```go
// Добавить пир вручную (без менеджера)
err = node.AddPeer("tcp://1.2.3.4:5678")
err = node.AddPeer("quic://[200:abc::1]:5678")

// Удалить пир
err = node.RemovePeer("tcp://1.2.3.4:5678")

// Инициировать переподключение ко всем отключённым пирам
node.RetryPeers()
```

## Multicast и Admin

```go
import golog "github.com/gologme/log"

// mDNS-обнаружение пиров в локальной сети
// Интерфейсы берутся из NodeConfig.MulticastInterfaces
mcLogger := golog.New(os.Stderr, "", golog.LstdFlags)
err = node.EnableMulticast(mcLogger)
defer node.DisableMulticast()

// Admin-сокет (JSON API для управления)
err = node.EnableAdmin("unix:///tmp/ygg-admin.sock")
// или TCP:
err = node.EnableAdmin("tcp://127.0.0.1:9001")
defer node.DisableAdmin()
```
