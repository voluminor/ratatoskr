# Utility command settings

`gsettings` parses only the `-go.*` utility command group. It does not start a node or execute commands.

## Use

```go
cfg, err := gsettings.Parse([]string{
    "-go.ask.addr=200::1",
    "-go.ask.peer=tls://peer.example:443",
    "-go.ask.format=json",
})
if err != nil {
    return err
}
fmt.Println(cfg.Ask.Addr)
```

`IsCommandArgs` detects whether any argument begins with `-go.` or `--go.`. `Parse` rejects positional arguments,
invalid enumerations, and invalid flag encodings. Peer flags may be repeated or comma-separated.

Defaults:

| Setting                          | Value   |
|----------------------------------|---------|
| Ask, peer-info, and probe format | `text`  |
| Forward protocol                 | `tcp`   |
| Generated config format          | `yml`   |
| Generated config preset          | `basic` |
| Import format                    | `yml`   |
| Export format                    | `json`  |

Run its independent tests with:

```bash
cd cmd/ratatoskr
GOWORK=off go test ./gsettings
```
