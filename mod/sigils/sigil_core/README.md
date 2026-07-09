# mod/sigils/sigil_core

Sigil registration, assembly, and metadata management for local NodeInfo.

Takes a base `map[string]any` (NodeInfo) and a set of sigils, validates and merges them, and maintains the `ratatoskr`
metadata key that lists active sigils and the library version.

## Obj

```go
obj, errs := sigil_core.New(nodeInfo, sigils...)
```

Creates an `Obj` from a base NodeInfo map and optional sigils. If `nodeInfo` is `nil`, an empty map is used. Errors are
non-fatal: each failed sigil is skipped, the rest are applied normally.

| Method      | Signature                          | Description                                                         |
|-------------|------------------------------------|---------------------------------------------------------------------|
| `NodeInfo`  | `() map[string]any`                | Returns a copy of the assembled map ready for `yggcore.SetNodeInfo` |
| `Sigils`    | `() map[string]sigils.Interface`   | Returns cloned registered sigils                                    |
| `Add`       | `(sg ...sigils.Interface) []error` | Registers sigils, writes keys into map, updates ratatoskr metadata  |
| `Get`       | `(name string) sigils.Interface`   | Returns sigil by name or `nil`                                      |
| `Del`       | `(name string) error`              | Removes sigil, its keys, and updates ratatoskr metadata             |
| `String`    | `() string`                        | Human-readable summary                                              |
| `LenSigils` | `() int`                           | Number of registered sigils                                         |
| `LenLocal`  | `() int`                           | Number of keys in assembled NodeInfo                                |
| `Len`       | `() int`                           | `LenSigils() + LenLocal()`                                          |

`Add` validates each sigil name, checks for duplicates, calls `SetParams` to merge keys into the internal map, and
updates the `ratatoskr` metadata key via `CompileInfo`.

`Del` removes keys that were introduced by that sigil during `Add` and recompiles the metadata key. User-supplied base
keys that existed before `Add` are preserved.

`NodeInfo()` returns a shallow structural copy of the assembled map: adding, deleting, or replacing top-level keys in
the
returned map does not change `Obj` state, but mutating a nested map or slice value can still affect shared data.
`Sigils()` returns a new map and clones each sigil, skipping a third-party sigil whose `Clone()` returns `nil`.

`Obj` is still not safe for concurrent mutation. If an embedding application calls `Add` or `Del` while another
goroutine reads `NodeInfo`, `Sigils`, `String`, or length methods, it must provide its own synchronization. The usual
ratatoskr flow builds `sigil_core.Obj` during startup and then treats it as immutable.

## Package-level functions

| Function      | Signature                              | Description                                                |
|---------------|----------------------------------------|------------------------------------------------------------|
| `CompileInfo` | `(map[string]sigils.Interface) string` | Assembles the ratatoskr metadata string from sigil names   |
| `ParseInfo`   | `(string) (string, []string, error)`   | Parses a ratatoskr info string into version and sigil list |

### Metadata format

```
[sigil1,sigil2] v0.1.3
```

Sigil names are sorted alphabetically. The version comes from `target.Version`.

```mermaid
flowchart LR
    base["base NodeInfo map"]
    sg["sigils"]
    base --> Obj
    sg --> Add["Add: validate + SetParams"]
    Add --> compile["CompileInfo"]
    compile --> meta["ratatoskr key: '[inet,info] v0.1.3'"]
    meta --> Obj
    Obj --> NodeInfo["NodeInfo() → map[string]any"]
```
