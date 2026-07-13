# mod/socks

SOCKS5 proxy over Yggdrasil. Allows regular applications to access the Yggdrasil network via the standard
SOCKS5 protocol.

## Contents

- [Overview](#overview)
- [Initialization](#initialization)
- [Runtime control](#runtime-control)
- [TCP and Unix socket](#tcp-and-unix-socket)
- [Connection limiting](#connection-limiting)
- [Unix socket handling](#unix-socket-handling)
- [Errors](#errors)

---

## Overview

```mermaid
flowchart LR
    App["application"] -->|" SOCKS5 "| Proxy["socks.Obj"]
    Proxy -->|" DialContext "| Ygg["Yggdrasil"]

    subgraph Proxy
        Listener["TCP / Unix listener"]
        Socks5["socks5.Server"]
        Limit["limitedListenerObj"]
    end

    Listener --> Socks5
    Socks5 -->|" connect "| Ygg
```

The application connects to a SOCKS5 proxy (TCP or Unix socket), the proxy resolves the address via the provided
`NameResolver`
and establishes a connection through the Yggdrasil dialer.

---

## Initialization

```go
s, err := socks.New(socks.ConfigObj{
    Network:           node, // proxy.ContextDialer, usually core.Obj
    Addr:              "127.0.0.1:1080", // or a socket in a private directory
    Resolver:          resolver,         // name resolver (.pk.ygg, DNS)
    Verbose:           false,
    Logger:            logger,
    MaxConnections:    100, // 0 — safe default, <0 — unlimited
    HandshakeTimeout:  10 * time.Second,
    DialTimeout:       10 * time.Second,
    TunnelIdleTimeout: 5 * time.Minute,
    MaxAssociateTargetsPerSession: 128, // 0 — safe default, <0 — no per-session cap
    Credentials:       credentials, // optional username/password auth
})
```

Creates and starts a SOCKS5 proxy. Close it with `Close`.

---

## Runtime control

```go
s.IsEnabled() // true
s.Addr()      // "127.0.0.1:1080"
s.IsUnix()    // false

s.SetMaxConnections(512)

err := s.Close() // stop and clean up
```

| Method                 | Description                          |
|------------------------|--------------------------------------|
| `Close()`              | Stops the proxy; idempotent          |
| `Addr()`               | Current listening address            |
| `IsUnix()`             | `true` if listening on a Unix socket |
| `IsEnabled()`          | `true` if the proxy is running       |
| `SetMaxConnections(n)` | Updates the active connection limit  |

`MaxConnections` is the only runtime-mutable setting. `DialTimeout` and `TunnelIdleTimeout` are immutable: set them once
via `ConfigObj` at `Start`. `TunnelIdleTimeout` uses a 5 minute safe default when set to `0`; use a negative value only
when idle tunnels must stay open indefinitely.

---

## TCP and Unix socket

The listener type is determined by the address:

| Address                             | Type        |
|-------------------------------------|-------------|
| `127.0.0.1:1080`                    | TCP         |
| `[::1]:1080`                        | TCP         |
| `/run/user/1000/ratatoskr/ygg.sock` | Unix socket |
| `./private/local.sock`              | Unix socket |

Rule: if the address starts with `/` or `.` — Unix socket, otherwise TCP.

---

## Connection limiting

The listener is always wrapped in a `limitedListenerObj` backed by a `common.DynamicLimitObj` — a runtime-adjustable
semaphore, so `SetMaxConnections` can change the limit while the proxy runs. `MaxConnections: 0` uses the safe default
(`256`); a negative value makes the limit unlimited, and `Accept` never blocks on it.

UDP ASSOCIATE targets are bounded to 1024 per server, 128 per principal within that server, and 128 per session by
default. A negative value disables only the per-session cap. Each server also owns its bounded worker pool, so load or
credentials on one embedded proxy cannot consume another proxy's quota or queue.

```mermaid
flowchart LR
    Accept["Accept()"] --> Sem{"semaphore free?"}
    Sem -->|" yes "| Conn["accept connection"]
    Sem -->|" no "| Block["wait"]
    Conn --> Close["Close()"]
    Close --> Release["release semaphore"]
```

- `Accept` blocks when the limit is reached
- `Close` releases the slot exactly once (`sync.Once`)
- Repeated `Close` calls are safe

---

## Unix socket handling

On startup with a Unix socket, stale files are handled:

```mermaid
flowchart TB
    Listen["net.Listen(unix)"] --> OK{"success?"}
    OK -->|" yes "| Done["ready"]
    OK -->|" EADDRINUSE "| Dial["dial socket"]
    Dial --> Alive{"responds?"}
    Alive -->|" yes "| Err["ErrAlreadyListening"]
    Alive -->|" ECONNREFUSED "| Check["Lstat: same socket inode?"]
    Check -->|" symlink "| Refuse["ErrSymlinkRefusal"]
    Check -->|" non-socket "| RefuseSock["ErrSocketRefusal"]
    Check -->|" socket "| Remove["os.Remove → retry"]
```

- If the socket is held by a live process — error
- The parent directory must exist, must not be a symlink, and must be private (`0700` or stricter)
- A stale socket is removed only after `ECONNREFUSED` and a same-inode check
- Symlinks and other non-socket paths are refused, never removed (`ErrSymlinkRefusal` / `ErrSocketRefusal`)
- Socket permissions are fixed at `0600`

On `Close`, the Unix socket file is automatically removed.

---

## Errors

| Variable                  | Description                                  |
|---------------------------|----------------------------------------------|
| `ErrAlreadyEnabled`       | `New` called while the proxy is already open |
| `ErrAlreadyListening`     | Unix socket is held by another process       |
| `ErrAssociateTargetLimit` | UDP ASSOCIATE target limit reached           |
| `ErrInvalidAddress`       | Empty or invalid listen address              |
| `ErrNetworkRequired`      | `Start` called without a `Network` dialer    |
| `ErrSymlinkRefusal`       | Refusal to remove a symlink (safety measure) |
| `ErrSocketRefusal`        | Refusal to remove a non-socket path (safety) |
| `ErrUnsafeSocketDir`      | Unix socket parent directory is not private  |
| `ErrSocketChanged`        | Socket path changed during the stale probe   |
