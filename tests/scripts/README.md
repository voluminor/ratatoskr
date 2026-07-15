# Test environment scripts

These scripts create disposable state under `tmp/tests`, build and start the container topology, and remove it. Run them
from any directory; each script resolves the repository root itself.

## Scripts

### `up.sh`

Build and start the environment:

```bash
bash tests/scripts/up.sh
```

Options:

| Option         | Effect                                                         |
|----------------|----------------------------------------------------------------|
| `--no-build`   | Reuse `rts-node`, `rts-ygghub`, and `rts-verifier` images      |
| `--no-rebuild` | Reuse each node's `tmp/tests/node-*/bin/ratatoskr-diag` binary |
| `--verify`     | Run the smoke verifier, then stop the stack                    |
| `--throughput` | Run throughput diagnostics, then stop the stack                |
| `--keep-state` | Preserve `tmp/tests` after `--verify` or `--throughput`        |

`--verify` and `--throughput` are mutually exclusive. Both modes install an exit trap that stops the Compose profile
even when the verifier fails.

### `bootstrap.sh`

Create the shared Go caches, per-node directories, three JSON configurations, and `tmp/tests/topology.txt`:

```bash
bash tests/scripts/bootstrap.sh
```

The generated configurations use MTU 65,535, a 10-second close timeout, a SOCKS limit of 2 connections, and the ports
documented in [../README.md](../README.md#services-and-host-ports).

### `down.sh`

Stop and remove the Compose services and network:

```bash
bash tests/scripts/down.sh
```

Remove generated state as well:

```bash
bash tests/scripts/down.sh --clean
```

Also delete the three test images and prune BuildKit cache:

```bash
bash tests/scripts/down.sh --clean --prune
```

## State ownership

The repository is mounted read-only in node containers. Each node copies it into its own directory under `tmp/tests`,
builds there, and uses caches under `tmp/tests/cache`. Profiles, traces, verifier output, binaries, and rendered
configuration therefore never modify tracked source files.
