# Live verifier

The verifier container runs black-box checks against the three diagnostic nodes. `run-smoke.sh` checks basic operation
and profiling; `run-throughput.sh` compares direct Docker traffic with Ratatoskr/Yggdrasil and measures CPU scaling.

## Contents

- [Smoke runner](#smoke-runner)
- [Throughput runner](#throughput-runner)
- [SOCKS UDP helper](#socks-udp-helper)
- [Result ownership](#result-ownership)

## Smoke runner

Run through the stack wrapper:

```bash
bash tests/scripts/up.sh --verify --keep-state
```

Hard failures produce a non-zero exit status:

- health response from all three nodes within 240 seconds;
- at least one connected peer per node within another 120 seconds;
- TCP echo from node-a to node-b;
- 512-byte UDP echo from node-a to node-b;
- SOCKS5 enable and disable;
- SOCKS5 TCP to node-b through its `.pk.ygg` name;
- SOCKS5 UDP ASSOCIATE echo to node-b;
- non-empty goroutine profile, CPU profile, and runtime trace.

Soft diagnostics are recorded without failing the run:

- 4,096-byte UDP echo, which can reveal MTU or fragmentation behavior;
- a 5-second, 4-stream TCP echo load used while collecting profiles.

Outputs are stored in `tmp/tests/results/smoke`, including health, snapshot and runtime JSON, individual check
responses, SOCKS output, the goroutine dump, CPU profile, trace, and `smoke-results.txt`.

The script also supports focused container calls:

```bash
/run-smoke.sh health node-a
/run-smoke.sh pprof node-a
/run-smoke.sh throughput
```

## Throughput runner

Run the default suite:

```bash
bash tests/scripts/up.sh --throughput --keep-state
```

The runner performs two phases:

1. Baseline selection measures direct and Yggdrasil TCP/UDP at 1, 4, and 16 streams. Each point gets a 5-second warm-up
   and three 20-second samples. The selected stream count is the highest median receiver goodput for that protocol and
   transport.
2. CPU scaling holds the selected Yggdrasil stream count constant while measuring direct and Yggdrasil paths at
   `GOMAXPROCS` 1, 2, 4, and 8, limited by the container's reported CPU count.

Direct and Yggdrasil samples alternate AB/BA order between repetitions. Profiles and runtime traces run separately from
measured samples.

Environment overrides:

| Variable                            |   Default | Bound or meaning                     |
|-------------------------------------|----------:|--------------------------------------|
| `THROUGHPUT_WARMUP_SECONDS`         |         5 | 1 to 30 seconds                      |
| `THROUGHPUT_MEASURE_SECONDS`        |        20 | 1 to 30 seconds                      |
| `THROUGHPUT_REPETITIONS`            |         3 | Positive integer                     |
| `THROUGHPUT_PROFILE_SECONDS`        |        10 | 1 to 30 seconds                      |
| `THROUGHPUT_TCP_PAYLOAD`            |   262,144 | 1 to 1,048,576 bytes                 |
| `THROUGHPUT_UDP_PAYLOAD`            |     1,200 | 1 to 65,499 bytes                    |
| `THROUGHPUT_STREAMS`                |  `1 4 16` | Each value 1 to 32                   |
| `THROUGHPUT_CPU_VALUES`             | `1 2 4 8` | Positive values up to available CPUs |
| `THROUGHPUT_STABILITY_WARN_PERCENT` |        10 | Relative-range warning threshold     |

`summary.json` contains median, minimum, maximum, mean, relative range, Yggdrasil/direct ratio, speedup from one CPU,
and scaling efficiency. Raw sender and receiver JSON remains alongside each sample.

Interpret receiver goodput as the primary result. UDP sender rate can exceed receiver goodput because writes only
enqueue datagrams. A scaling efficiency above 100% usually means the one-CPU point was scheduler-starved; it does not
prove superlinear algorithmic scaling. Compare profiles before attributing the direct/Yggdrasil gap to Ratatoskr rather
than Yggdrasil routing, encryption, transport, or scheduling.

The suite fails only when a control/data path fails or the receiver records no data. It warns, but does not fail, when
the relative range exceeds the configured threshold.

## SOCKS UDP helper

`socks-udp-check.py` implements the minimum SOCKS5 handshake and UDP ASSOCIATE framing needed for a real UDP echo check.
It sends one payload through the node-a proxy and requires the echoed payload to match. Usage inside the verifier image:

```bash
python3 /socks-udp-check.py node-a 1080 '[200::1]:18081' test-payload
```

## Result ownership

The verifier writes only to `/out`, mounted from `tmp/tests/results`. Use `--keep-state` to preserve it. Without that
option, the wrapper removes `tmp/tests` after teardown.
