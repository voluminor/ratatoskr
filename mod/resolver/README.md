# mod/resolver

Name resolver for Yggdrasil. Supports three resolution strategies: `.pk.ygg` public key mapping, IP literals, and
DNS queries over the Yggdrasil network.

## Contents

- [Overview](#overview)
- [Initialization](#initialization)
- [Name resolution](#name-resolution)
  - [Strategy order](#strategy-order)
  - [.pk.ygg mapping](#pkygg-mapping)
  - [IP literals](#ip-literals)
    - [DNS](#dns)
- [Errors](#errors)

---

## Overview

```mermaid
flowchart LR
    Name["name"] --> PkYgg{".pk.ygg?"}
    PkYgg -->|" yes "| Decode["hex → ed25519 → IPv6"]
    PkYgg -->|" no "| IPLit{"IP literal?"}
    IPLit -->|" yes "| Return["net.IP"]
    IPLit -->|" no "| DNS{"DNS?"}
    DNS -->|" hasDNS "| Lookup["LookupIP → ip6"]
    DNS -->|" no "| Err["ErrNoNameserver"]
    Decode --> Return
    Lookup --> Return
```

---

## Initialization

```go
r, err := resolver.New(resolver.ConfigObj{
    Dialer:         dialer,
    Nameserver:     "[200::1]:53", // DNS over Yggdrasil
    LookupTimeout:   10 * time.Second,
    CacheTTL:        30 * time.Second,
    CacheMaxEntries: 4096,
})
if err != nil {
    return err
}
defer func() { _ = r.Close() }()
```

If `Nameserver` is empty, DNS resolution is disabled and `Dialer` is optional. If `Nameserver` is set, `Dialer` is
mandatory and `New` returns `ErrDialerRequired` before creating an object when it is missing.

`Close` cancels admitted DNS lookups and waits for their owned goroutines. It is idempotent and returns `error`, so a
standalone resolver satisfies `io.Closer`.

The resolver uses `PreferGo: true` (pure Go DNS, no cgo).

The zero `ConfigObj` is valid and resolves only IP literals and `.pk.ygg` names. DNS additionally requires both fields
shown below. Timing and cache settings have safe defaults for embedded use:

| Field             | Default | Description                                                                        |
|-------------------|---------|------------------------------------------------------------------------------------|
| `Dialer`          | `nil`   | Required when `Nameserver` is non-empty                                            |
| `Nameserver`      | empty   | DNS server address; empty disables DNS                                             |
| `LookupTimeout`   | `10s`   | DNS lookup timeout. `0` — default, `<0` — no resolver deadline (Go DNS client ~5s) |
| `CacheTTL`        | `30s`   | Positive DNS cache TTL. `0` — default, `<0` off                                    |
| `CacheMaxEntries` | `4096`  | Positive DNS cache cap. `0` — default, `<0` off                                    |

Concurrency of DNS lookups is bounded three ways: by the caller (the SOCKS connection limit), by singleflight collapsing
duplicate in-flight names, and by an admission cap of 256 distinct in-flight names — a genuinely new name beyond that
cap
returns `ErrLookupBusy`, while joining an existing flight is always allowed.

---

## Name resolution

```go
ctx, ip, err := r.Resolve(ctx, "a7aa9d653b0259c67a211e7a6ccd281219db1246c75e4ebcf9edbdbdaff55924.pk.ygg")
```

Returns `net.IP` and the original `ctx` (for passing values through the chain).

### Strategy order

Strategies are tried in decreasing order of specificity:

1. **`.pk.ygg`** — if the name ends with `.pk.ygg`
2. **IP literal** — if the name parses as an IP address
3. **DNS** — if a nameserver is configured

The first successful strategy wins.

### .pk.ygg mapping

Suffix: `.pk.ygg`

```
<hex-encoded-ed25519-key>.pk.ygg → IPv6 via address.AddrForKey()
```

Only the canonical `<64hex>.pk.ygg` form is accepted. Subdomains such as `name.<64hex>.pk.ygg` are rejected.
The key must be exactly 32 bytes after hex decoding.

### Settings

`LookupTimeout` and `CacheTTL` are immutable: set them once through `ConfigObj` at `New`; to change them, create a new
resolver. `LookupTimeout < 0` imposes no resolver deadline — the lookup is single-flighted and detached from the
caller's
context, so it is bounded by the Go DNS client's own per-query timeout (~5s), not by the caller's context.
`CacheTTL < 0`
disables caching entirely. Failed DNS lookups are cached for a short bounded TTL to avoid repeated timeout amplification
while a nameserver is down.

### IP literals

IPv4 and IPv6 addresses are returned as-is:

```
200::1       → net.IP{200::1}
192.168.1.1  → net.IP{192.168.1.1}
```

### DNS

IPv6 resolution via the configured nameserver. If no nameserver is set — `ErrNoNameserver` is returned.

```go
r.resolver.LookupIP(ctx, "ip6", name)
```

Returns the first Yggdrasil node address (`200::/7`) or routed subnet address (`300::/7`). Other DNS answers are
ignored; if the response has addresses but none belongs to either Yggdrasil form, resolution returns
`ErrNonYggdrasilAddress`. IP literals remain pass-through and are not filtered.

---

## Errors

| Variable                    | Description                             |
|-----------------------------|-----------------------------------------|
| `ErrNoNameserver`           | DNS server is not configured            |
| `ErrNoAddresses`            | DNS query returned no addresses         |
| `ErrDialerRequired`         | DNS is configured without a dialer      |
| `ErrInvalidPublicKeyDomain` | `.pk.ygg` public key domain is invalid  |
| `ErrInvalidKeyLength`       | Public key is not 32 bytes              |
| `ErrNonYggdrasilAddress`    | DNS response is not a Yggdrasil address |
| `ErrLookupBusy`             | Too many concurrent distinct lookups    |
| `ErrClosed`                 | Resolver has been closed                |
