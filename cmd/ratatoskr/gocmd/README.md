# Utility command execution

`gocmd` executes values parsed by [gsettings](../gsettings/README.md). `Run` returns `handled=false` when no utility
command is selected.

## Current status

The package compiles against the current forwarding and probe constructors. Key, configuration, and settings behavior
has unit coverage; network commands still require live peers and end-to-end validation.

## Command groups

| Group     | Purpose                                                   |
|-----------|-----------------------------------------------------------|
| key       | Vanity search, address derivation, PEM conversion         |
| conf      | Generate Ratatoskr config; import/export Yggdrasil config |
| ask       | Query remote NodeInfo                                     |
| peer info | Connect and report peer state                             |
| forward   | Run local TCP or UDP forwarding                           |
| probe     | Topology scan, route trace, or latency samples            |

Commands create short-lived nodes with admin disabled. Network commands use a 5-second root close budget. Probe defaults
are a 40-second command timeout, depth 3, concurrency 64, and 4 latency samples.

Configuration conversion preserves the hexadecimal private-key scalar across JSON, YAML, and HJSON. Generated
private-key files use mode `0600`.
