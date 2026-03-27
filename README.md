# ratatoskr

> **[Русская версия](README.RU.md)**

Go library for embedding a Yggdrasil node into an application. The network stack runs in userspace
on top of gVisor netstack — no TUN interface, root access, or external dependencies required.

- **Userspace stack.** TCP/UDP over gVisor netstack, no OS privileges.
- **Standard Go interfaces.** `DialContext`, `Listen`, `ListenPacket` — compatible with `net.Conn`,
  `net.Listener`, `http.Transport`, etc.
- **`core.Interface` as a contract.** Packages `socks`, `peermgr`, and the root `ratatoskr` depend on
  the interface, not on `core.Obj` implementation. You can plug in your own implementation for testing
  or custom transports.

### ratatoskr vs yggstack

[yggstack](https://github.com/yggdrasil-network/yggstack) is a ready-made binary for end users
(SOCKS proxy, TCP/UDP forwarding via CLI flags). `ratatoskr` is a library for developers:
a node is created with `ratatoskr.New()`, everything is controlled through the Go API.

### Out of the box

- `core` — node startup, `DialContext`/`Listen`/`ListenPacket`, peer management, address, subnet,
  public key
- Automatic shutdown via `context.Context`
- Thread safety, idempotent `Close()`

### Optional

- **SOCKS5 proxy** — `EnableSOCKS()` / `DisableSOCKS()`
- **mDNS (multicast)** — `EnableMulticast()` / `DisableMulticast()`, peer discovery on local network
- **Admin socket** — `EnableAdmin()` / `DisableAdmin()`, unix/tcp
- **Peer manager** (`peermgr`) — peer rotation and optimization; enabled via `ConfigObj.Peers`
- **Resolver** (`mod/resolver`) — `.pk.ygg` address resolver
- **Forward** (`mod/forward`) — TCP/UDP forwarding

### Examples

Ready-made examples in [`cmd/embedded/`](cmd/embedded/):

| Example                               | Description              |
|---------------------------------------|--------------------------|
| [`http`](cmd/embedded/http)           | HTTP server on Yggdrasil |
| [`tiny-http`](cmd/embedded/tiny-http) | Minimal HTTP server      |
| [`tiny-chat`](cmd/embedded/tiny-chat) | Chat over Yggdrasil      |
| [`mobile`](cmd/embedded/mobile)       | Mobile platform example  |

Also [`cmd/yggstack/`](cmd/yggstack/) — yggstack implementation built on ratatoskr.

## Table of contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Architecture](#architecture)
- [Module structure](#module-structure)
- [Packages](#packages)
- [Configuration](#configuration)
- [Usage examples](#usage-examples)
- [Snapshot](#snapshot)
- [Thread safety](#thread-safety)
- [Error handling](#error-handling)
- [Lifecycle](#lifecycle)

## Installation

```bash
go get github.com/voluminor/ratatoskr
```

Minimum Go version: **1.24**.

## Quick start

Create a node, connect to the network, and make an HTTP request:

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
		// Ctx: when the context is cancelled, the node calls Close() automatically
		Ctx: ctx,
		// Peers: peer manager will automatically select the best connection
		Peers: &peermgr.ConfigObj{
			Peers: []string{
				"tls://peer1.example.com:17117",
				"tls://peer2.example.com:17117",
			},
			MaxPerProto: 1, // one best peer per protocol
		},
	})
	if err != nil {
		panic(err)
	}
	defer node.Close()

	// Node's IPv6 address on the Yggdrasil network (200::/7)
	fmt.Println("Network address:", node.Address())

	// Use the node as transport for the standard http.Client
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

## Architecture

```mermaid
graph TB
    App[Application]

subgraph ratatoskr
Obj[ratatoskr.Obj]
SOCKS[SOCKS5 proxy]
Resolver[Resolver .pk.ygg / DNS]
PeerMgr[PeerManager — peer selection]
    end

subgraph core
CoreObj[core.Obj]
Netstack[netstack — userspace TCP/UDP]
NIC[NIC — packet bridge]
Multicast[Multicast — mDNS discovery]
Admin[Admin — control socket]
end

subgraph external [External dependencies]
YggCore[yggdrasil-go/core]
gVisor[gVisor netstack]
end

App --> Obj
Obj --> CoreObj
Obj --> SOCKS
Obj --> PeerMgr
SOCKS --> Resolver
SOCKS -->|DialContext|CoreObj
Resolver -->|DialContext for DNS|CoreObj
PeerMgr -->|AddPeer / RemovePeer|CoreObj

CoreObj --> Netstack
CoreObj --> Multicast
CoreObj --> Admin
Netstack --> NIC
NIC -->|IPv6 packets|YggCore
Netstack --> gVisor
```

### Packet path

How data flows through the stack — from application to Yggdrasil network and back:

```mermaid
sequenceDiagram
    participant App as Application
    participant NS as Netstack (gVisor)
    participant NIC as NIC (bridge)
    participant Ygg as Yggdrasil Core
    Note over App, Ygg: Outgoing packet (Dial / Write)
    App ->> NS: DialContext("tcp", "[ipv6]:port")
    NS ->> NIC: WritePackets(IPv6 packet)
    NIC ->> Ygg: ipv6rwc.Write(raw bytes)
    Ygg -->> Ygg: Routing through overlay network
    Note over App, Ygg: Incoming packet (Listen / Read)
    Ygg ->> NIC: ipv6rwc.Read(raw bytes)
    NIC ->> NS: DeliverNetworkPacket(IPv6)
    NS ->> App: net.Conn.Read(data)
```

### NIC internal architecture

NIC (`nicObj`) — bridge between gVisor and Yggdrasil at the IPv6 packet level.

```mermaid
graph LR
    subgraph nicObj
        ReadLoop[Read goroutine]
        RSTLoop[RST goroutine]
        RSTQueue[RST queue<br/>chan, configurable size]
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

**TCP RST handling:** RST packets without payload are sent not directly but through a buffered queue
(`chan *PacketBuffer`). Queue size is set via `core.ConfigObj.RSTQueueSize` (default 100).
The counter of dropped RST packets is available via `core.Obj.RSTDropped()`.

**RST queue overflow strategy:**

1. Attempt to send to channel
2. If channel is full — evict the oldest packet
3. Retry sending
4. If still fails — packet is dropped, drop counter is incremented

**Packet writing (writePacket):** uses zero-copy via `AsViewList` — packet data is passed
to `ipv6rwc.Write` directly without copying. If a packet consists of multiple Views (rare case),
data is assembled into a buffer from `sync.Pool`. Panics in `WritePackets` are recovered
and logged without crashing the entire stack.

## Module structure

```mermaid
graph LR
subgraph "ratatoskr (root package)"
A[Obj — facade]
B[ConfigObj]
C[SOCKSConfigObj]
end

subgraph "core"
D[Obj — Yggdrasil node]
E[Interface — contract]
F[netstackObj — TCP/UDP stack]
G[nicObj — LinkEndpoint]
H[componentObj — lifecycle]
end

subgraph "peermgr"
PM[Obj — peer manager]
SEL[selector — best selection]
end

subgraph "resolver"
I[Obj — name resolver]
end

subgraph "socks"
J[Obj — SOCKS5 server]
K[Interface — contract]
end

A -->|embeds|E
A -->|uses|J
A -->|creates|I
A -->|optionally|PM
PM -->|AddPeer/RemovePeer|E
PM --> SEL
D -->|implements|E
D -->|contains|F
F -->|contains|G
D -->|contains|H
J -->|implements|K
```

## Packages

### `ratatoskr` (root)

Facade for embedding. Combines core, SOCKS proxy, resolver, and peer manager into a single entry point.

| Type               | Purpose                                                        |
|--------------------|----------------------------------------------------------------|
| `Obj`              | Node with full capabilities: network methods + SOCKS + control |
| `ConfigObj`        | Context, Yggdrasil config, logger, timeout, peer manager       |
| `SOCKSConfigObj`   | Proxy address, DNS server, verbose, connection limit           |
| `SnapshotObj`      | Full node state: address, peers, SOCKS, counters               |
| `PeerSnapshotObj`  | Single peer state: URI, Up, Latency, traffic                   |
| `SOCKSSnapshotObj` | SOCKS5 proxy state: Enabled, Addr, IsUnix                      |

### `core`

Core — Yggdrasil node with userspace network stack.

| Type           | Purpose                                                                                              |
|----------------|------------------------------------------------------------------------------------------------------|
| `Obj`          | Node: DialContext, Listen, ListenPacket, peer management, multicast, admin. Core is `atomic.Pointer` |
| `Interface`    | Public contract — everything external code needs                                                     |
| `netstackObj`  | gVisor TCP/UDP/ICMP stack                                                                            |
| `nicObj`       | Bridge between gVisor and Yggdrasil at the IPv6 packet level                                         |
| `componentObj` | Generic Enable/Disable lifecycle for multicast and admin                                             |

### `peermgr`

Peer manager — automatic selection and maintenance of the optimal peer set.

| Type             | Purpose                                                      |
|------------------|--------------------------------------------------------------|
| `Obj`            | Manager: probing, best selection, periodic refresh           |
| `ConfigObj`      | Parameters: candidate list, timeouts, selection strategy     |
| `ValidatePeers`  | Public URI validation function: duplicates, parsing, schemes |
| `AllowedSchemes` | Allowed transport schemes: `tcp`, `tls`, `quic`, `ws`, `wss` |

**`MaxPerProto` modes:**

| Value     | Behavior                                                       |
|-----------|----------------------------------------------------------------|
| `0` / `1` | One best peer per protocol (default)                           |
| `N > 1`   | Top-N peers per protocol, sorted by latency                    |
| `-1`      | Passive mode: add all candidates without selection; no probing |

**`optimizeActive` logic:**

With `BatchSize <= 1` — one batch = entire list (backward compatibility):

```mermaid
flowchart TD
    ADD[AddPeer for all candidates] --> WAIT[Wait ProbeTimeout]
    WAIT --> BUILD[buildResults: match with GetPeers]
    BUILD --> SELECT[selectBest: top-N per protocol]
    SELECT --> REMOVE[RemovePeer for losers]
    REMOVE --> STORE[Save active list]
```

With `BatchSize >= 2` — sliding window, elimination race:

```mermaid
flowchart TD
    START[connected = current winners] --> BATCH[Take next batch from Peers]
    BATCH --> ADD[AddPeer for batch]
    ADD --> WAIT[Wait ProbeTimeout]
    WAIT --> BUILD[buildResults for all connected + batch]
    BUILD --> SELECT[selectBest: top-N per protocol]
    SELECT --> REMOVE[RemovePeer losers]
   REMOVE --> UPDATE[connected = winners]
    UPDATE --> MORE{More batches?}
    MORE -->|Yes| BATCH
    MORE -->|No| DONE[Final active list]
```

Each new batch races against current winners. Worst performers are eliminated after each round —
ultimately the best from the entire list remain.

### `resolver`

Name resolver with three strategies:

```mermaid
flowchart TD
    Input[Input name]
    Input --> PK{Suffix .pk.ygg?}
    PK -->|Yes| HEX[Decode hex public key]
    HEX --> ADDR[Compute IPv6 from key]
    PK -->|No| IP{IPv6 literal?}
    IP -->|Yes| PASS[Return as-is]
    IP -->|No| NS{Nameserver configured?}
    NS -->|No| ERR[Error: no nameserver configured]
    NS -->|Yes| DNS[DNS query via Yggdrasil]
    DNS --> RESULT[First AAAA record]
```

**`.pk.ygg` format:** `<hex-pubkey>.pk.ygg` or `subdomain.<hex-pubkey>.pk.ygg`
(with subdomains, the last segment before `.pk.ygg` is used).
Public key — 32 bytes ed25519 in hex (64 characters).

**DNS over Yggdrasil:** if `Nameserver` is configured, DNS queries (`AAAA`) go through the core's `DialContext` —
traffic does not leak to the system resolver. Without a nameserver, DNS name resolution returns an error.

### `socks`

SOCKS5 proxy over Yggdrasil. Supports TCP and Unix sockets. No authentication.

```mermaid
stateDiagram-v2
    [*] --> Created: New()
    Created --> Enabled: Enable()
    Enabled --> Created: Disable()
    Enabled --> Enabled: Enable() → error
    Created --> Created: Disable() → no-op
```

## Configuration

### ConfigObj (ratatoskr)

| Field             | Type                 | Default | Description                                                                                                                          |
|-------------------|----------------------|---------|--------------------------------------------------------------------------------------------------------------------------------------|
| `Ctx`             | `context.Context`    | `nil`   | Parent context; on cancellation the node automatically calls `Close()`. `nil` — manual control                                       |
| `Config`          | `*config.NodeConfig` | `nil`   | Yggdrasil configuration (keys, listen addresses). `nil` — random keys generated. `Config.Peers` must be empty if `Peers` is set      |
| `Logger`          | `yggcore.Logger`     | `nil`   | Logger; `nil` — logs discarded (noop). Passed to core, SOCKS, and peer manager                                                       |
| `CoreStopTimeout` | `time.Duration`      | `0`     | `core.Stop()` timeout on shutdown. `0` — wait indefinitely                                                                           |
| `Peers`           | `*peermgr.ConfigObj` | `nil`   | Peer manager. `nil` — peers come from `Config.Peers` as in standard Yggdrasil. Non-nil + `Config.Peers` non-empty — error in `New()` |

### ConfigObj (peermgr)

| Field                | Type             | Default  | Description                                                                  |
|----------------------|------------------|----------|------------------------------------------------------------------------------|
| `Peers`              | `[]string`       | required | Candidate URI list: `"tls://host:port"`, `"tcp://..."`, `"quic://..."`, etc. |
| `ProbeTimeout`       | `time.Duration`  | `10s`    | Connection timeout during probing. Ignored when `MaxPerProto == -1`          |
| `RefreshInterval`    | `time.Duration`  | `0`      | Automatic re-check interval. `0` — only at startup                           |
| `MaxPerProto`        | `int`            | `1`      | Number of best peers per protocol. `-1` — passive mode                       |
| `BatchSize`          | `int`            | `0`      | Probing batch size. `0`/`1` — all at once; `≥ 2` — sliding window            |
| `Logger`             | `yggcore.Logger` | required | Logger; `nil` returns an error                                               |
| `OnNoReachablePeers` | `func()`         | `nil`    | Callback when no reachable peers found after probing                         |

### Peer validation (peermgr)

`peermgr.ValidatePeers([]string) → ([]peerEntryObj, []error)` — public function, called in `New()`.
Can be used separately for pre-validation.

| Step          | Action                                                                     |
|---------------|----------------------------------------------------------------------------|
| Empty strings | Skipped without error                                                      |
| Duplicates    | Error `"duplicate peer %q"`, entry discarded; order of remaining preserved |
| URI parsing   | `url.Parse`; on error — entry discarded                                    |
| Host          | Required; `"tls://:8080"` — error `"missing host"`                         |
| Scheme        | From `AllowedSchemes`: `tcp`, `tls`, `quic`, `ws`, `wss`; others — error   |

In `New()`: each error is logged via `Warnf`. If no peers remain after validation — `New()` returns
an error.

### ConfigObj (core)

| Field             | Type                 | Default | Description                                  |
|-------------------|----------------------|---------|----------------------------------------------|
| `Config`          | `*config.NodeConfig` | `nil`   | Yggdrasil configuration. `nil` — random keys |
| `Logger`          | `yggcore.Logger`     | `nil`   | Logger; `nil` — noop                         |
| `CoreStopTimeout` | `time.Duration`      | `0`     | `core.Stop()` timeout. `0` — no limit        |
| `RSTQueueSize`    | `int`                | `100`   | Deferred RST packet queue size. `0` → 100    |

### SOCKSConfigObj (ratatoskr)

| Field            | Type   | Default  | Description                                                                                                         |
|------------------|--------|----------|---------------------------------------------------------------------------------------------------------------------|
| `Addr`           | string | required | Proxy address: TCP `"127.0.0.1:1080"` or Unix socket `"/tmp/ygg.sock"`. Path starting with `/` or `.` — Unix        |
| `Nameserver`     | string | `""`     | DNS server on Yggdrasil network. Format: `"[ipv6]:port"`. Empty string — only `.pk.ygg` and IP literals             |
| `Verbose`        | bool   | `false`  | Verbose logging for each SOCKS connection                                                                           |
| `MaxConnections` | int    | `0`      | Maximum concurrent connections. `0` — unlimited. When limit reached, new connections wait for a slot to be released |

### Address validation

Network methods (`DialContext`, `Listen`, `ListenPacket`) accept addresses in `"[ipv6]:port"` or `":port"` format.

| Input               | Behavior                                    |
|---------------------|---------------------------------------------|
| `"[200:abc::1]:80"` | Valid IPv6 + port                           |
| `":8080"`           | Empty host — bind to all addresses (Listen) |
| `":0"`              | Ephemeral port (assigned by OS)             |
| `"localhost:80"`    | Error: `invalid IP address "localhost"`     |
| `"[::1]:99999"`     | Error: `port 99999 out of range 0-65535`    |
| `"bad"`             | Error: `net.SplitHostPort` failed           |

Supported networks: `tcp`, `tcp6` (for Dial/Listen), `udp`, `udp6` (for Dial/ListenPacket).

## Usage examples

### HTTP client via Yggdrasil

The simplest way — pass `node.DialContext` as transport. All TCP connections will go through Yggdrasil.

```go
client := &http.Client{
Transport: &http.Transport{
// node.DialContext routes connections through the overlay network
DialContext: node.DialContext,
},
}

// Access a service by IPv6 address on the Yggdrasil network
resp, err := client.Get("http://[200:abcd::1]:8080/api/v1/status")
if err != nil {
log.Fatal(err)
}
defer resp.Body.Close()
```

### TCP server on Yggdrasil

The node becomes visible on the Yggdrasil network by its IPv6 address. `Listen` accepts connections only from
the overlay network — the server does not contact the external internet.

```go
// ":8080" — listen on all node addresses (equivalent to [200:...]:8080)
ln, err := node.Listen("tcp", ":8080")
if err != nil {
log.Fatal(err)
}
defer ln.Close()

fmt.Printf("Server available at: http://[%s]:8080/\n", node.Address())

// Standard http.Serve works with any net.Listener
http.Serve(ln, http.HandlerFunc(func (w http.ResponseWriter, r *http.Request) {
fmt.Fprintf(w, "Hello from Yggdrasil node %s", node.Address())
}))
```

### UDP on Yggdrasil

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
log.Printf("UDP from %s: %s", addr, buf[:n])
// Reply to sender
pc.WriteTo(buf[:n], addr)
}
```

### SOCKS5 proxy with DNS over Yggdrasil

SOCKS5 proxy allows using Yggdrasil from any application supporting SOCKS5 (curl, browser, git).
`socks5h://` — mode with name resolution on the proxy side.

```go
err = node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
// Address for the SOCKS5 proxy to listen on
Addr: "127.0.0.1:1080",
// DNS server inside the Yggdrasil network — names are resolved via overlay
Nameserver: "[200:abcd::1]:53",
// Log each connection (useful for debugging)
Verbose: true,
// Maximum 128 concurrent connections; 0 — unlimited
MaxConnections: 128,
})
if err != nil {
log.Fatal(err)
}
defer node.DisableSOCKS()

// Usage from terminal:
// curl --proxy socks5h://127.0.0.1:1080 http://example.pk.ygg/
// curl --proxy socks5h://127.0.0.1:1080 http://[200:abcd::1]:8080/
```

#### SOCKS5 proxy via Unix socket

```go
err = node.EnableSOCKS(ratatoskr.SOCKSConfigObj{
// Path starting with "/" — Unix socket (faster than TCP for local use)
Addr:       "/tmp/ygg-socks.sock",
Nameserver: "[200:abcd::1]:53",
})
defer node.DisableSOCKS()

// curl --proxy socks5h://unix:/tmp/ygg-socks.sock http://example.pk.ygg/
```

### Peer manager

#### Active mode — select best

The manager probes all candidates and keeps the N best per protocol. With `RefreshInterval > 0`, probing
repeats periodically.

```go
node, err := ratatoskr.New(ratatoskr.ConfigObj{
Ctx: ctx,
Peers: &peermgr.ConfigObj{
Peers: []string{
"tls://peer1.example.com:17117",
"tls://peer2.example.com:17117",
"tls://peer3.example.com:17117",
"quic://peer4.example.com:17117",
},
// Wait for connection no more than 10 seconds per batch
ProbeTimeout: 10 * time.Second,
// Re-probe every 5 minutes
RefreshInterval: 5 * time.Minute,
// One best TLS peer and one best QUIC peer
MaxPerProto: 1,
// Sliding window: two candidates at a time
BatchSize: 2,
// Call when no reachable peers found
OnNoReachablePeers: func () {
log.Println("No reachable peers!")
},
},
})

// Get current active peers (selected by manager)
active := node.PeerManagerActive()
log.Println("Active peers:", active)

// Trigger an unscheduled re-check (blocks until completion)
if err := node.PeerManagerOptimize(); err != nil {
log.Println("Optimization:", err)
}
```

#### Passive mode — add all without selection

Passive mode (`MaxPerProto: -1`) does not perform probing and adds all candidates immediately.
Identical to standard Yggdrasil behavior with `Config.Peers`.

```go
Peers: &peermgr.ConfigObj{
Peers: []string{
"tls://peer1.example.com:17117",
"tls://peer2.example.com:17117",
},
// -1 = passive mode, no probing
MaxPerProto: -1,
// Reconnect the entire list every 10 minutes
RefreshInterval: 10 * time.Minute,
},
```

#### Pre-validating the peer list

```go
import "github.com/voluminor/ratatoskr/mod/peermgr"

peers := []string{
"tls://peer1.example.com:17117",
"tls://peer1.example.com:17117", // duplicate
"ftp://invalid:1234",            // unsupported scheme
"", // empty string, will be skipped
}

valid, errs := peermgr.ValidatePeers(peers)
for _, e := range errs {
log.Println("Peer error:", e)
}
log.Printf("Valid peers: %d", len(valid))
```

### Runtime peer management

```go
// Add a peer manually (without manager)
if err := node.AddPeer("tls://1.2.3.4:17117"); err != nil {
log.Println("AddPeer:", err)
}
if err := node.AddPeer("quic://[200:abc::1]:17117"); err != nil {
log.Println("AddPeer:", err)
}

// Remove a peer
if err := node.RemovePeer("tls://1.2.3.4:17117"); err != nil {
log.Println("RemovePeer:", err)
}

// Reconnect all disconnected peers
node.RetryPeers()
```

### Monitoring via Snapshot

`Snapshot()` collects full node state in a single atomic call.

```go
snap := node.Snapshot()

// Basic node parameters
log.Printf("Address:    %s", snap.Address)
log.Printf("Subnet:     %s", snap.Subnet)
log.Printf("Public key: %s", snap.PublicKey)
log.Printf("MTU:        %d", snap.MTU)
log.Printf("RST drops:  %d", snap.RSTDropped)

// Peer state
for _, p := range snap.Peers {
status := "DOWN"
if p.Up {
status = fmt.Sprintf("UP, latency=%.1fms", float64(p.Latency)/float64(time.Millisecond))
}
log.Printf("  Peer %s: %s, RX=%d TX=%d", p.URI, status, p.RXBytes, p.TXBytes)
}

// Peers selected by manager
if len(snap.ActivePeers) > 0 {
log.Println("Active (manager):", snap.ActivePeers)
}

// SOCKS state
if snap.SOCKS.Enabled {
log.Printf("SOCKS5: %s (unix=%v)", snap.SOCKS.Addr, snap.SOCKS.IsUnix)
}

// Serialize to JSON for export (e.g., /metrics or /status)
data, _ := json.MarshalIndent(snap, "", "  ")
fmt.Println(string(data))
```

### Logging

`ratatoskr` accepts any object implementing `yggcore.Logger`. Example adapter for `log/slog`:

```go
import (
"log/slog"
"fmt"
)

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

node, err := ratatoskr.New(ratatoskr.ConfigObj{
Logger: slogAdapter{l: slog.Default()},
})
```

### Multicast and Admin

```go
import (
"os"
golog "github.com/gologme/log"
)

// mDNS peer discovery on local network.
// Interfaces are set in NodeConfig.MulticastInterfaces.
mcLogger := golog.New(os.Stderr, "[multicast] ", golog.LstdFlags)
if err := node.EnableMulticast(mcLogger); err != nil {
log.Fatal(err)
}
defer node.DisableMulticast()

// Admin socket — JSON API for node management.
// Unix socket (recommended for local management):
if err := node.EnableAdmin("unix:///tmp/ygg-admin.sock"); err != nil {
log.Fatal(err)
}
// Or TCP:
// node.EnableAdmin("tcp://127.0.0.1:9001")
defer node.DisableAdmin()
```

### Graceful shutdown

Three ways to shut down a node:

```go
// 1. Explicit Close() call — idempotent, safe for repeated calls
defer node.Close()

// 2. Via context — Close() is called automatically on cancellation
ctx, cancel := context.WithCancel(context.Background())
node, _ = ratatoskr.New(ratatoskr.ConfigObj{Ctx: ctx})
// ...
cancel() // → node shuts down on its own

// 3. Via OS signal
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
node, _ = ratatoskr.New(ratatoskr.ConfigObj{Ctx: ctx})
<-ctx.Done() // wait for signal; node is already shutting down
```

## Snapshot

`Snapshot()` — collects full node state in a single call. Returns `SnapshotObj` with JSON tags.

| Field         | Type                | Description                                 |
|---------------|---------------------|---------------------------------------------|
| `Address`     | `string`            | Node IPv6 address                           |
| `Subnet`      | `string`            | `/64` subnet                                |
| `PublicKey`   | `string`            | ed25519 public key (hex)                    |
| `MTU`         | `uint64`            | Stack MTU                                   |
| `RSTDropped`  | `int64`             | Dropped RST packet counter                  |
| `Peers`       | `[]PeerSnapshotObj` | Each peer state (URI, Up, Latency, traffic) |
| `ActivePeers` | `[]string`          | Peers selected by manager (`omitempty`)     |
| `SOCKS`       | `SOCKSSnapshotObj`  | SOCKS5 proxy state (Enabled, Addr, IsUnix)  |

## Thread safety

All public methods of `Obj` and `core.Obj` are safe for concurrent use.

| Method / group                          | Guarantee                                                                           |
|-----------------------------------------|-------------------------------------------------------------------------------------|
| `DialContext`, `Listen`, `ListenPacket` | Thread-safe; netstack protected via `atomic.Pointer`                                |
| `EnableSOCKS` / `DisableSOCKS`          | Mutex-protected; repeated `Enable` without `Disable` — error                        |
| `EnableMulticast` / `DisableMulticast`  | `sync.RWMutex`-protected; repeated `Enable` — error                                 |
| `EnableAdmin` / `DisableAdmin`          | Same as multicast                                                                   |
| `AddPeer` / `RemovePeer`                | Thread-safe; core protected via `atomic.Pointer` (delegates to `yggdrasil-go/core`) |
| `PeerManagerActive`                     | Mutex-protected inside `peermgr.Obj`; returns a copy of the list                    |
| `PeerManagerOptimize`                   | Blocks until completion; serialized with automatic probing via `optimizeMu`         |
| `Close`                                 | Idempotent (`sync.Once`); safe for repeated and concurrent calls                    |
| `Address`, `Subnet`, `PublicKey`, `MTU` | Thread-safe; core and netstack via `atomic.Pointer`                                 |
| `Snapshot`                              | Thread-safe; collects data from thread-safe methods                                 |

**Concurrent Enable multicast + admin:** admin handlers are registered atomically via a separate
`handlersMu` mutex after `enable()` completes, which prevents ABBA deadlock between components.

## Error handling

### Methods returning errors

| Method                | Errors                                                                      |
|-----------------------|-----------------------------------------------------------------------------|
| `New`                 | Core creation error, `Config.Peers` + `Peers` conflict, peermgr start error |
| `DialContext`         | `ErrNotAvailable` (node closed), gVisor errors, invalid address             |
| `Listen`              | `ErrNotAvailable`, gVisor errors, invalid address                           |
| `ListenPacket`        | `ErrNotAvailable`, gVisor errors, invalid address                           |
| `EnableSOCKS`         | `"SOCKS already enabled"`, listen error (port busy / invalid path)          |
| `DisableSOCKS`        | Listener close error                                                        |
| `EnableMulticast`     | `"multicast already enabled"`, invalid regex, `multicast.New` error         |
| `EnableAdmin`         | `"admin already enabled"`, invalid address, `admin.New` returned nil        |
| `AddPeer`             | Invalid URI, core error                                                     |
| `RemovePeer`          | Invalid URI, core error                                                     |
| `PeerManagerOptimize` | `"peermgr: not running"` if manager is not started                          |
| `Close`               | Collects errors from all components via `errors.Join`; idempotent           |

### ErrNotAvailable

Returned from `DialContext`, `Listen`, `ListenPacket` if netstack is already destroyed (after `Close()`).

```go
conn, err := node.DialContext(ctx, "tcp", "[200:abc::1]:80")
if errors.Is(err, ratatoskr.ErrNotAvailable) {
// Node is already closed — don't attempt to reconnect
return
}
```

### Unix socket (SOCKS)

When starting on a Unix socket, the stale file case is handled:

```mermaid
flowchart TD
    LISTEN[net.Listen unix] --> OK{Success?}
    OK -->|Yes| DONE[Done]
    OK -->|No| INUSE{EADDRINUSE?}
    INUSE -->|No| FAIL[Error]
    INUSE -->|Yes| PROBE[Dial to socket]
    PROBE --> ALIVE{Response?}
    ALIVE -->|Yes| FAIL2[another instance is listening]
    ALIVE -->|No| SYMLINK{Symlink?}
    SYMLINK -->|Yes| FAIL3[refusing to remove: is a symlink]
    SYMLINK -->|No| REMOVE[Remove file] --> RETRY[Retry Listen]
```

### Rate limiting (SOCKS)

With `MaxConnections > 0`, the proxy limits the number of concurrent connections via a semaphore:

```mermaid
flowchart TD
    ACCEPT[Accept connection] --> SEM[Acquire semaphore slot]
    SEM --> SERVE[Serve via SOCKS5]
    SERVE --> CLOSE[Close connection] --> FREE[Release slot] --> ACCEPT
```

- `Accept` is called **before** acquiring the semaphore — on shutdown, `listener.Close()` correctly unblocks the wait
- The semaphore blocks processing when the limit is reached; the connection is already accepted but waits for a slot
- The semaphore slot is released exactly once on connection close (`sync.Once`)

## Lifecycle

```mermaid
flowchart TD
    START([Creation]) --> NEW[ratatoskr.New]
    NEW --> CORE[Start Yggdrasil Core]
    CORE --> NS[Create netstack + NIC]
    NS --> GOROUTINES[Start goroutines: read + RST]
    GOROUTINES --> ROUTE[Route 0200::/7]
    ROUTE --> PMCHECK{Peers set?}
    PMCHECK -->|Yes| PMSTART[peermgr.Start — async probing]
PMCHECK -->|No|READY
PMSTART --> READY([Node ready])
READY -->|optionally|SOCKS[EnableSOCKS]
READY -->|optionally|MC[EnableMulticast]
READY -->|optionally|ADM[EnableAdmin]
READY -->|optionally|PEER[AddPeer / RemovePeer]
    SOCKS --> READY
    MC --> READY
    ADM --> READY
    PEER --> READY
    READY --> CLOSE[Close]
CLOSE --> S0[peermgr.Stop — RemovePeer active peers]
    S0 --> S1[Disable SOCKS]
    S1 --> S2[Disable Multicast + Admin]
S2 --> S3[Close listeners]
    S3 --> S4[core.Stop]
    S4 --> S5[Close NIC: done → ipv6rwc.Close → wait goroutines]
S5 --> S6[Destroy gVisor stack]
S6 --> DONE([Done])
```

### Shutdown order (Close)

1. **peermgr.Stop()** — cancel probing context, wait for goroutines, `RemovePeer` all active peers
2. **Disable SOCKS** — closing the listener stops `Serve`, `wg.Wait()` waits for completion. Unix socket is removed
3. **Disable Multicast + Admin** — call `stopFn()` for each component
4. **Close listeners** — all listeners created via `Listen`/`ListenPacket` are closed
5. **core.Stop()** — stop Yggdrasil core. Unblocks `ipv6rwc.Read()` in NIC
6. **NIC Close** — `close(done)` signals goroutines, `ipv6rwc.Close()`, wait for `readDone` and `rstDone`, drain
   RST queue, `RemoveNIC`
7. **stack.Destroy()** — destroy gVisor stack

With `CoreStopTimeout > 0`: if `core.Stop()` does not complete within the specified time,
a warning is logged and shutdown continues.

### Auto-shutdown via context

If `Ctx` is passed in `ConfigObj`, a goroutine listens for context cancellation and calls `Close()` automatically.
On manual `Close()`, the goroutine terminates via the `done` channel.
