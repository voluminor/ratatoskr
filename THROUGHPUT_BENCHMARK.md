# Ratatoskr Throughput Benchmark

## Summary

| Protocol |                   Direct Docker |          Ratatoskr/Yggdrasil | Overlay/direct | Overlay median loss |
|----------|--------------------------------:|-----------------------------:|---------------:|--------------------:|
| TCP      | 4,808.941 MiB/s (40.340 Gbit/s) | 198.493 MiB/s (1.665 Gbit/s) |         4.128% |                 N/A |
| UDP      |    439.738 MiB/s (3.689 Gbit/s) |  44.502 MiB/s (0.373 Gbit/s) |        10.120% |             24.228% |

- The Ratatoskr control and lifecycle code is not an observed throughput bottleneck. Profiles point to
  Yggdrasil/Ironwood cryptography and packet handling, gVisor networking, syscalls, copies, allocations, and scheduling.
  The exact split between those components was not isolated; see [Attribution](#attribution-ratatoskr-or-yggdrasil).
- The 4.128% TCP ratio compares each path at its independently selected best concurrency. A closer one-stream comparison
  produced 7.244%. Neither value is a standalone Yggdrasil efficiency figure; see [TCP baseline details](#tcp).
- Saturated UDP had material loss on both paths and high overlay variation. Its result is maximum observed receiver
  goodput, not sustainable lossless capacity; see [UDP baseline details](#udp)
  and [Stability warnings](#stability-warnings).
- Two scheduler contexts were enough to reach the single-stream plateau. One caused severe UDP scheduler contention,
  while four and eight did not improve single-stream throughput; see [CPU scaling](#cpu-scaling).
- The measured single-stream overlay TCP ceiling was about 1.67 Gbit/s on this host and topology. Deployment
  implications are listed under [Operational guidance](#operational-guidance).

## Contents

- [Test environment](#test-environment)
- [Method](#method)
- [Baseline results](#baseline-results)
    - [TCP](#tcp)
    - [UDP](#udp)
- [CPU scaling](#cpu-scaling)
- [Stability warnings](#stability-warnings)
- [CPU-profile evidence](#cpu-profile-evidence)
- [Attribution: Ratatoskr or Yggdrasil?](#attribution-ratatoskr-or-yggdrasil)
- [Operational guidance](#operational-guidance)
- [Reproduction and retained data](#reproduction-and-retained-data)

## Test environment

| Item                             | Value                                                                                                                                                       |
|----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Total elapsed time               | 2,135 seconds (35 minutes 35 seconds)                                                                                                                       |
| Host kernel                      | Linux 6.11.0-29-generic, x86-64                                                                                                                             |
| Logical CPUs visible to Docker   | 8                                                                                                                                                           |
| Host memory visible to Docker    | 33,527,910,400 bytes (31.22 GiB)                                                                                                                            |
| Docker Engine                    | 28.4.0                                                                                                                                                      |
| Docker storage / cgroups         | overlay2 / cgroup v2                                                                                                                                        |
| Diagnostic container build image | `golang:1.25-bookworm`                                                                                                                                      |
| Yggdrasil                        | `github.com/yggdrasil-network/yggdrasil-go v0.5.14`                                                                                                         |
| gVisor                           | `gvisor.dev/gvisor`, revision `968e93457fe6`                                                                                                                |
| Tested direction                 | `node-a` sender to `node-b` sink                                                                                                                            |
| Direct path                      | Go diagnostic sender, Docker bridge, Go diagnostic sink                                                                                                     |
| Overlay path                     | Go diagnostic sender, gVisor TCP/IP, Ratatoskr core, Yggdrasil/Ironwood, `ygg-hub-1`, Yggdrasil/Ironwood, Ratatoskr core, gVisor TCP/IP, Go diagnostic sink |

Both edge nodes peer with `ygg-hub-1`. The second hub and `node-c` belong to the general diagnostic topology but are not
on the measured `node-a` to `node-b` route.

## Method

- Traffic was one-way into a streaming sink; echo traffic was not used for throughput measurements.
- TCP and UDP were tested over both the direct Docker path and the Ratatoskr/Yggdrasil path.
- Each condition had a 5-second warm-up that was discarded.
- Each recorded condition used three independent 20-second repetitions.
- Direct-first and Yggdrasil-first ordering alternated between repetitions to reduce systematic thermal and
  background-load bias.
- Baseline concurrency candidates were 1, 4, and 16 streams.
- The winning concurrency for each path was selected by median receiver goodput, not by a single best sample.
- TCP writes used 262,144-byte application payload chunks.
- UDP used 1,200-byte datagrams.
- CPU scaling tested `GOMAXPROCS=1,2,4,8` on both embedded edge processes.
- Scaling held concurrency at one stream, the Yggdrasil winner for both protocols, so changes in stream count could not
  be mistaken for CPU scaling.
- CPU profiles and runtime traces were collected in separate load runs and were not included in the clean throughput
  samples.
- `GOMAXPROCS` was restored to the original value of 8 on both nodes after the run.

All rates below are receiver goodput. MiB means 1,048,576 bytes. Gbit/s uses decimal gigabits. Displayed values are
rounded from the finalized JSON aggregates; percentages were calculated before display rounding.

## Baseline results

### TCP

| Path                | Selected streams |         Minimum |          Median |         Maximum |            Mean | Relative range |
|---------------------|-----------------:|----------------:|----------------:|----------------:|----------------:|---------------:|
| Direct Docker       |               16 | 4,757.701 MiB/s | 4,808.941 MiB/s | 5,081.062 MiB/s | 4,882.568 MiB/s |         6.724% |
| Ratatoskr/Yggdrasil |                1 |   185.627 MiB/s |   198.493 MiB/s |   201.471 MiB/s |   195.197 MiB/s |         7.982% |

The ratio of the independently selected maxima is 4.128%. In throughput terms, the direct path delivered 24.228 times
the overlay goodput. Calling the remaining 95.872% “Ratatoskr overhead” would be incorrect because the two paths execute
fundamentally different network stacks and use different winning concurrency.

At the closer one-stream, eight-`GOMAXPROCS` comparison, direct TCP delivered 2,747.266 MiB/s and the overlay delivered
199.025 MiB/s. That ratio is 7.244%, or a 13.804-fold difference. This is still a full-stack comparison rather than a
Yggdrasil-only measurement.

### UDP

| Path                | Selected streams |       Minimum |        Median |       Maximum |          Mean | Relative range |
|---------------------|-----------------:|--------------:|--------------:|--------------:|--------------:|---------------:|
| Direct Docker       |                4 | 433.758 MiB/s | 439.738 MiB/s | 442.426 MiB/s | 438.641 MiB/s |         1.971% |
| Ratatoskr/Yggdrasil |                1 |  40.408 MiB/s |  44.502 MiB/s |  51.677 MiB/s |  45.529 MiB/s |        25.322% |

| Path                | Minimum loss | Median loss | Maximum loss | Mean loss | Relative loss range |
|---------------------|-------------:|------------:|-------------:|----------:|--------------------:|
| Direct Docker       |      13.351% |     13.817% |      14.102% |   13.757% |              5.438% |
| Ratatoskr/Yggdrasil |      20.181% |     24.228% |      28.732% |   24.381% |             35.293% |

The maximum-to-maximum UDP ratio is 10.120%, a 9.881-fold difference. These values represent receiver goodput while the
sender deliberately saturates the path. Because both paths lose packets, they are not estimates of sustainable lossless
UDP capacity. The overlay result was also unstable across repetitions: its 25.322% throughput range is well above the
runner's 10% warning threshold.

## CPU scaling

### TCP, one stream

| `GOMAXPROCS` |   Direct median | Overlay median | Overlay range | Overlay/direct | Overlay speedup from 1 | Scaling efficiency |
|-------------:|----------------:|---------------:|--------------:|---------------:|-----------------------:|-------------------:|
|            1 | 2,766.299 MiB/s |  106.490 MiB/s |       16.275% |         3.850% |                 1.000x |           100.000% |
|            2 | 2,746.221 MiB/s |  204.547 MiB/s |        6.981% |         7.448% |                 1.921x |            96.040% |
|            4 | 2,744.469 MiB/s |  196.815 MiB/s |        9.000% |         7.171% |                 1.848x |            46.205% |
|            8 | 2,747.266 MiB/s |  199.025 MiB/s |        9.117% |         7.244% |                 1.869x |            23.362% |

Two scheduler contexts nearly doubled TCP overlay throughput. Increasing the setting from 2 to 4 or 8 did not improve a
single stream. The direct one-stream baseline remained near 2.75 GiB/s throughout, so this plateau is not explained by
the diagnostic source or sink becoming slower at higher settings.

The TCP sender profile at `GOMAXPROCS=8` accumulated 5.18 CPU-seconds during 3.10 seconds of wall time, equivalent to
about 1.67 continuously busy cores. A single overlay flow therefore did not use the remaining scheduler capacity.
Additional independent flows may scale differently, but that was not established by this run.

### UDP, one stream

| `GOMAXPROCS` | Direct median | Overlay median | Overlay range | Overlay loss | Overlay/direct | Overlay speedup from 1 | Scaling efficiency |
|-------------:|--------------:|---------------:|--------------:|-------------:|---------------:|-----------------------:|-------------------:|
|            1 | 248.059 MiB/s |    3.437 MiB/s |        2.437% |      96.705% |         1.386% |                 1.000x |           100.000% |
|            2 | 194.751 MiB/s |   56.327 MiB/s |       16.430% |      26.997% |        28.922% |                16.387x |           819.372% |
|            4 | 188.977 MiB/s |   52.852 MiB/s |       21.378% |      21.084% |        27.967% |                15.377x |           384.413% |
|            8 | 182.752 MiB/s |   49.025 MiB/s |       16.127% |      19.674% |        26.826% |                14.263x |           178.289% |

The apparent superlinear efficiencies do not mean that the algorithm scales beyond linearly. At one scheduler context,
the sender consumed about 95% of its available CPU while the receiver accumulated only 0.26 CPU-seconds in 3.09 seconds
of wall time because little useful traffic reached it. The sender's traffic generator and overlay packet-processing
goroutines competed for a single scheduler context; 96.705% of packets were lost and goodput collapsed. Two contexts
removed that sender-side scheduler bottleneck. More than two did not improve saturated one-stream UDP goodput and all
2-to-8 results remained noisy. The corresponding hot paths are recorded
under [CPU-profile evidence](#cpu-profile-evidence).

The direct UDP baseline decreased as the process-wide setting increased. This reinforces why direct measurements were
repeated at each CPU setting instead of normalizing every point against one unrelated direct run.

## Stability warnings

The runner reports, but does not fail, a condition whose relative range exceeds 10%. The completed run emitted warnings
for:

- direct TCP, 4 streams, `GOMAXPROCS=8`;
- overlay TCP, 16 streams, `GOMAXPROCS=8`;
- overlay UDP, 1, 4, and 16 streams, `GOMAXPROCS=8`;
- direct UDP, 16 streams, `GOMAXPROCS=8`;
- TCP scaling at `GOMAXPROCS=1`;
- UDP scaling at `GOMAXPROCS=2,4,8`.

The selected direct and overlay TCP baseline conditions remained below the warning threshold. The selected overlay UDP
baseline did not. UDP numbers should therefore be treated as a capacity region with substantial loss and jitter, not as
a stable service-level target.

## CPU-profile evidence

### TCP overlay sender, `GOMAXPROCS=8`

The 3.10-second profile contained 5.18 CPU-seconds of samples, or 166.90% of one core. The largest flat costs were:

| Function or subsystem | Flat CPU |
|-----------------------|---------:|
| Salsa20               |   22.39% |
| `syscall6`            |   14.29% |
| `memmove`             |   13.51% |
| Poly1305 update       |    8.30% |
| `futex`               |    4.44% |
| gVisor checksum       |    4.25% |

This profile is dominated by encryption, kernel transitions, copies, synchronization, and checksum work. It also
explains the scaling plateau: the single stream used roughly 1.67 cores even though eight scheduler contexts were
permitted.

### UDP overlay sender, `GOMAXPROCS=2`

The 3.10-second profile contained 5.57 CPU-seconds of samples, or 179.49% of one core. Notable flat costs were
`syscall6` at 17.41%, Salsa20 at 9.52%, `memmove` at 5.75%, and Poly1305 at 2.51%. Allocation and garbage-collection
work was visible. In cumulative terms, gVisor UDP endpoint writes accounted for 24.60%, phony inbox processing for
63.91%, and Ironwood encrypted-session sends for 20.65%.

### UDP overlay receiver, `GOMAXPROCS=2`

The 3.10-second profile contained 5.42 CPU-seconds of samples, or 174.71% of one core. Yggdrasil `ipv6rwc` key-store
packet reads accounted for 8.12% flat and 13.84% cumulative CPU. Salsa20 used 7.75%, `syscall6` 7.20%, and `memmove`
4.24% flat. Phony inbox processing accounted for 39.11% cumulatively. Allocations and pools were also visible.

### UDP overlay at `GOMAXPROCS=1`

The sender used 3.00 CPU-seconds during 3.15 seconds of wall time, or 95.13% of its only scheduler context. The receiver
used only 0.26 CPU-seconds during 3.09 seconds, or 8.42%, because little useful traffic reached it. Sender hot paths
included Salsa20, Poly1305, memory copies, gVisor checksums, pools, and allocations. gVisor UDP writes accounted for
37.67% cumulative CPU. This sender-side scheduler saturation matches the measured 3.437 MiB/s goodput and 96.705% packet
loss.

## Attribution: Ratatoskr or Yggdrasil?

The [baseline results](#baseline-results) and [CPU profiles](#cpu-profile-evidence) support three separate statements.

1. **Ratatoskr's control and wrapper code is not the main throughput bottleneck.** It does not appear among the
   significant entries in the [CPU profiles](#cpu-profile-evidence).

2. **The result is not a measurement of Yggdrasil alone.** The overlay side includes Ratatoskr's gVisor integration
   because ordinary TCP and UDP sockets are implemented by a userspace gVisor stack connected to Yggdrasil through
   `ipv6rwc`. It also crosses the intermediate `ygg-hub-1`. The current test cannot assign an exact percentage to
   gVisor, the edge Yggdrasil cores, Ironwood, or the hub.

3. **“Yggdrasil reaches only 4-5% of the possible network speed” is the wrong conclusion.** As
   the [TCP baseline details](#tcp) show, the headline ratio uses different winning concurrency on the two paths. Its
   denominator is a same-host Docker path operating at kernel and memory throughput, not a realistic physical-network
   limit. The closer one-stream ratio still describes the complete Ratatoskr/Yggdrasil/gVisor route and must not be
   published as standalone Yggdrasil efficiency.

An exact attribution requires additional A/B measurements:

- upstream Yggdrasil packet I/O through `ipv6rwc`, without gVisor or TCP;
- a gVisor-only loop without Yggdrasil;
- the current complete Ratatoskr path;
- direct Yggdrasil peering between the edge nodes versus the hub route;
- CPU profiles from the Yggdrasil hub as well as both edges.

Until those controls exist, the defensible conclusion is that Ratatoskr glue overhead is small in the observed profiles,
while most end-to-end cost belongs to the encrypted userspace Yggdrasil/Ironwood plus gVisor data path. The benchmark
does not prove how that cost divides between those upstream components.

## Operational guidance

- Use at least two scheduler contexts for an embedded node expected to carry sustained traffic. The one-context failure
  mode is quantified under [CPU scaling](#cpu-scaling).
- More than two contexts did not increase single-stream throughput in this topology. Do not infer that the whole node
  can never use more cores; concurrent independent flows require a separate scaling run.
- The observed TCP ceiling was about 1.67 Gbit/s for one overlay stream on this host and topology;
  see [TCP baseline details](#tcp).
- Saturated UDP did not produce a stable, low-loss operating point. Capacity planning must target a lower sender rate
  and measure latency and loss at controlled offered loads.
- Compare against the intended physical link and workload, not only against same-host Docker throughput.
- Re-run after changes to Yggdrasil, gVisor, MTU, cryptography, queueing, or the NIC bridge. Those are the areas most
  likely to move the result.

## Reproduction and retained data

Run the full benchmark with:

```bash
bash tests/scripts/up.sh --throughput --keep-state
```

The runner writes `summary.json`, raw repetitions, runtime snapshots, profiles, and traces under
`tmp/tests/results/throughput`. Generated benchmark state is intentionally temporary and is removed unless
`--keep-state` is supplied.

The raw artifacts from the run documented here were deleted after the results and profile summaries were checked, as
required by the repository's temporary-file policy. This document therefore preserves the finalized aggregates and
profile observations, but it is not a substitute for the original JSON and pprof files when re-analysis is required.
