# mod/sigils

Typed data blocks for Yggdrasil NodeInfo.

NodeInfo is a JSON object (max 16 KB) attached to each Yggdrasil node. Other nodes can request it via the Yggdrasil
protocol. Sigils provide structured access to this data: building local NodeInfo from Go types, and parsing foreign
NodeInfo received as JSON.

Each sigil owns one or more top-level keys in the NodeInfo map. The `ninfo` module manages sigil registration, conflict
detection, and assembly into the final NodeInfo.

## Table of contents

- [Creating a new sigil](#creating-a-new-sigil)
    - [File structure](#file-structure)
    - [Rules](#rules)
    - [Testing requirements](#testing-requirements)
    - [Registration](#registration)
- [Interface methods](#interface-methods)
    - [GetName](#getname)
    - [GetParams](#getparams)
    - [SetParams](#setparams)
    - [ParseParams](#parseparams)
    - [Match](#match)
    - [Params](#params)
- [Package-level functions](#package-level-functions)
- [Built-in sigils](#built-in-sigils)
    - [info](#info)
    - [public](#public)
    - [inet](#inet)
    - [services](#services)

---

## Creating a new sigil

### File structure

Every sigil lives in its own subpackage under `mod/sigils/<name>/` with three files:

```
mod/sigils/<name>/
├── values.go  — constants, variables, regexps
├── func.go    — package-level functions: Name(), Keys(), ParseParams(), Match(), Parse()
└── obj.go     — Obj struct, New() constructor, sigils.Interface methods
```

### Rules

**The 16 KB constraint is everything.** NodeInfo is shared across all sigils and the ratatoskr metadata key. Every byte
counts.

1. **Strict limits on everything.** Define maximum counts for every collection (arrays, maps) and maximum lengths for
   every string. No unbounded data. The less data a sigil stores, the better — leave room for other sigils.

2. **Validate all input with regexps.** Every string field must have a compiled regexp in `values.go`. Reject invalid
   input at construction time in `New()`, not later.

3. **Never mutate input maps.** `SetParams` and `ParseParams` must copy, never write to the caller's map.

4. **Handle JSON types in Match/Parse.** Foreign NodeInfo arrives from `encoding/json`: arrays are `[]any`, maps are
   `map[string]any`, numbers are `float64`. Your `Match` checks structure and types only, not content validity.

5. **Name must pass `ValidateName`.** Pattern: `[a-z0-9._-]{3,32}`.

6. **Keys must not conflict.** Each sigil's top-level NodeInfo keys must be unique across all sigils. `SetParams`returns
   an error on collision.

7. **Provide both object methods and package-level functions.** Package-level `Match()`, `ParseParams()`, `Parse()` work
   without an object instance, useful for quick checks. Object methods delegate to them and additionally store parsed
   data.

### Testing requirements

Every sigil must have an `<name>_test.go` with:

- **Constructor tests** — valid input, every boundary (min/max lengths, min/max counts), every invalid case (too short,
  too long, wrong characters, duplicates, empty, nil, exceeding limits).
- **Match tests** — valid match, missing key, wrong type at every nesting level, empty collections, nil values, wrong
  JSON types (int vs float64).
- **Parse tests** — valid round-trip, error on non-matching input.
- **SetParams tests** — no conflict, conflict error, input not mutated.
- **ParseParams tests** — key present, key absent, data stored in object.
- **Params tests** — empty object, populated object.
- **Benchmarks** — at least for `New`, `Match`, `Parse`.

### Registration

After creating the sigil package, add it to `target/sigils.go` via the code generator (`_generate/sigils`). This
registers the sigil in the global maps used by `ninfo.Parse` to automatically recognize it in foreign NodeInfo.

---

## Interface methods

Defined in `interface.go`:

```go
type Interface interface {
GetName() string
GetParams() []string
SetParams(map[string]any) (map[string]any, error)
ParseParams(map[string]any) map[string]any
Match(map[string]any) bool
Params() map[string]any
}
```

### GetName

Returns the unique sigil identifier (e.g. `"info"`, `"services"`). Must pass `ValidateName`: `^[a-z0-9._-]{3,32}$`.

### GetParams

Returns the list of top-level NodeInfo keys this sigil owns. Used by `ninfo` for conflict detection when adding sigils
and for cleanup when removing them.

### SetParams

```go
SetParams(NodeInfo map[string]any) (map[string]any, error)
```

Writes sigil data into a **copy** of the input map. Returns the new map with sigil keys added. On key conflict, returns
an error — the original map is never touched. Empty optional fields are skipped.

### ParseParams

```go
ParseParams(NodeInfo map[string]any) map[string]any
```

Extracts this sigil's keys from foreign NodeInfo and **stores the result inside the object** for later retrieval via
`Params()`. Returns a map containing only the extracted keys.

### Match

```go
Match(NodeInfo map[string]any) bool
```

Checks whether foreign NodeInfo contains this sigil with correct structure and JSON types. Does not validate content —
only shape. Must account for JSON unmarshaling types:

| Go source type  | JSON type in `map[string]any`      |
|-----------------|------------------------------------|
| `[]string`      | `[]any` (each element is `string`) |
| `map[string]T`  | `map[string]any`                   |
| `int`, `uint16` | `float64`                          |

### Params

```go
Params() map[string]any
```

Returns the sigil's current data as a NodeInfo fragment. Empty when the object has no data.

---

## Package-level functions

Every sigil package also exports standalone functions that work without an object:

| Function                                     | Description                                                       |
|----------------------------------------------|-------------------------------------------------------------------|
| `Name() string`                              | Returns the sigil name constant                                   |
| `Keys() []string`                            | Returns the list of owned NodeInfo keys                           |
| `Match(map[string]any) bool`                 | Same as interface method, no object needed                        |
| `ParseParams(map[string]any) map[string]any` | Extracts keys without storing into object                         |
| `Parse(map[string]any) (*Obj, error)`        | Creates an `Obj` from foreign NodeInfo (calls `Match` internally) |

---

## Built-in sigils

### info

**NodeInfo keys:** `name`, `type`, `location`, `contact`, `description`

Node identity card.

| Field         | Go type               | Required | NodeInfo key  | Constraints                                                                          |
|---------------|-----------------------|----------|---------------|--------------------------------------------------------------------------------------|
| `Name`        | `string`              | yes      | `name`        | FQDN or friendly name, `[a-z0-9._-]{4,64}`                                           |
| `Type`        | `string`              | yes      | `type`        | Device/role label, `[a-z0-9.-]{2,32}`                                                |
| `Location`    | `string`              | no       | `location`    | Physical location, 2–514 chars, no leading/trailing spaces                           |
| `Contacts`    | `map[string][]string` | no       | `contact`     | Max 8 groups, max 8 per group. Group name: `[a-z0-9.-]{2,32}`, value: 3–258 chars    |
| `Description` | `string`              | no       | `description` | Free-text description (e.g. peering policy), 2–514 chars, no leading/trailing spaces |

`Match` requires at least `name` and `type` as strings. `contact` must be `map[string]any` → `[]any` → `string`.

```json
{
  "name": "home.y.example.net",
  "type": "server",
  "location": "Gravelines, France",
  "contact": {
    "email": [
      "admin@example.net"
    ],
    "xmpp": [
      "admin@jabber.example.net"
    ]
  },
  "description": "open for anyone"
}
```

### public

**NodeInfo key:** `public`

Peering URIs grouped by network type.

| Parameter | Go type               | Constraints                                                                                                                     |
|-----------|-----------------------|---------------------------------------------------------------------------------------------------------------------------------|
| `peers`   | `map[string][]string` | Key — network name `[a-z0-9]{2,16}`, value — URI list. Max 8 groups, max 16 URIs per group. URI: `[a-zA-Z0-9+._/:@[\]-]{8,256}` |

`Match` checks `map[string]any` where each key passes the group name regexp and each value is `[]any` of strings. Empty
map does not match.

```json
{
  "public": {
    "internet": [
      "tls://203.0.113.55:443",
      "tcp://[2001:db8::1]:8443"
    ],
    "tor": [
      "socks://abcdef1234567890.onion:9001"
    ]
  }
}
```

### inet

**NodeInfo key:** `inet`

Real internet addresses of the node (domains, IPs). Maps an Yggdrasil address to a real-world location.

| Parameter | Go type    | Constraints                                                    |
|-----------|------------|----------------------------------------------------------------|
| `addrs`   | `[]string` | Address list, `[a-zA-Z0-9._:/-]{4,256}`, max 32, no duplicates |

`Match` checks for `[]any` of strings, non-empty.

```json
{
  "inet": [
    "example.com",
    "203.0.113.55"
  ]
}
```

### services

**NodeInfo key:** `services`

Ports open on this node inside the Yggdrasil network. Enables service discovery without port scanning.

| Parameter  | Go type             | Constraints                                                                  |
|------------|---------------------|------------------------------------------------------------------------------|
| `services` | `map[string]uint16` | Key — service name `[a-z0-9_-]{2,32}`, value — port 1–65535. Max 256 entries |

`Match` checks `map[string]any` where each key passes the name regexp and each value is `float64` in range 1–65535 with
no fractional part.

```json
{
  "services": {
    "http": 80,
    "ssh": 22,
    "mumble": 64738
  }
}
```
