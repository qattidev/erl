# Recorded measurements

Measured locally on 2026-09-07 with Go 1.25.11 on an Apple M4 Pro. Native runs use
macOS/arm64 and 14 logical CPUs; Linux runs use the existing arm64 Podman VM with
four virtual CPUs. These results apply to the fixed workload in [the benchmark
guide](README.md), not to every application or machine. The independent Linux
confirmation establishes **2.22% higher median TLS HTTP/2 throughput** for ERL in
that environment. The original macOS and exploratory Linux throughput comparisons
remain inconclusive. Faster dispatch is established for several tested cases.

## Routing microbenchmarks

Ten samples per case, 300 ms per sample, default GOMAXPROCS (14). These are the
results after compressing consecutive static path segments during registration.

| 500-route case | ServeMux ns/op | ERL ns/op | ERL time reduction |
| --- | ---: | ---: | ---: |
| Static | 158.8 | 60.08 | 62.16% |
| One parameter | 198.3 | 173.8 | 12.36% |
| Two parameters | 265.9 | 186.2 | 29.96% |
| Catch-all | 347.5 | 172.8 | 50.27% |
| Mixed | 231.9 | 163.0 | 29.72% |

All five differences are significant (`p < 0.001`, n=10). The full matrix also
covers 10 and 5,000 routes. ERL's one-parameter case at 5,000 routes did not show
a significant difference. See [raw samples](dispatch.txt) and
[benchstat output](dispatch-summary.txt).

Memory is a tradeoff: the mixed case allocates 521 B/op for ERL versus 343 B/op for
ServeMux. Both figures include a fresh 320-byte request copy per operation. ERL
uses the public `Request.SetPathValue` API, which allocates a map for parameters;
ServeMux uses its internal parameter storage. These benchmarks do not recycle
populated requests or hide that cost.

## Initial TLS HTTP/2 comparison

Before the prefix-compression optimization, ten alternating five-second samples
per router produced the following medians at 64 concurrent requests over four
connections, with four Go processors each for the server and client:

| Metric | ServeMux | ERL |
| --- | ---: | ---: |
| Requests/second | 147,847 | 148,985 |
| p50 latency | 0.3864 ms | 0.3862 ms |
| p95 latency | 0.8465 ms | 0.8298 ms |
| p99 latency | 1.096 ms | 1.071 ms |
| Response errors | 0 | 0 |

The approximately 0.77% throughput difference is **not statistically significant**
(`p = 0.481`, n=10). This run does not meet the HTTP/2 throughput acceptance target.
Raw data, environment details, and statistical output are retained under
[results/tls](results/tls/).

## Optimized HTTP/2 comparisons

After prefix compression, the native TLS run used twenty alternating ten-second
samples per router. The larger sampling plan was selected before starting that run.
The native h2c run used ten five-second samples. Both retain the original workload,
64 concurrent requests, four connections, and four Go processors per process.

| Series | Samples/router | ServeMux req/s | ERL req/s | Median change | Throughput p |
| --- | ---: | ---: | ---: | ---: | ---: |
| [Native TLS](results/tls-optimized/) | 20 | 150,033 | 151,645 | +1.07% | 0.221 |
| [Native h2c](results/h2c/) | 10 | 153,207 | 153,999 | +0.52% | 0.579 |
| [Linux VM TLS, exploratory](results/linux-tls/) | 20 | 82,767 | 83,877 | +1.34% | 0.149 |

These benchstat throughput comparisons are **inconclusive**. The Linux VM run uses
two Go processors per process and five-second samples; compare routers within each
series, not absolute throughput across environments.

| Series | ServeMux p50/p95/p99, ms | ERL p50/p95/p99, ms |
| --- | ---: | ---: |
| Native TLS | 0.3862 / 0.8165 / 1.0467 | 0.3868 / 0.8030 / 1.0295 |
| Native h2c | 0.3679 / 0.8354 / 1.0714 | 0.3693 / 0.8226 / 1.0553 |
| Linux VM TLS, exploratory | 0.6744 / 1.5966 / 2.2930 | 0.6614 / 1.5636 / 2.2055 |

These are medians of each run's latency percentiles. Native TLS p95 and p99 were
lower for ERL in the secondary benchstat comparisons (`p = 0.001` and `0.025`,
respectively); these secondary metrics do not satisfy the throughput target.
All completed measurements reported zero response errors and four connections.

### Supplemental concurrency sweep

Three three-second native TLS samples per router at each concurrency level are
descriptive only. They are too few to support the acceptance claim.

| Concurrent requests | Connections | ServeMux req/s | ERL req/s | Median change |
| --- | ---: | ---: | ---: | ---: |
| [1](results/tls-c1/) | 1 | 14,298 | 14,575 | +1.94% |
| [16](results/tls-c16/) | 4 | 94,045 | 93,520 | −0.56% |
| [256](results/tls-c256/) | 4 | 170,954 | 163,365 | −4.44% |

At concurrency 256, ERL's median p95 was also higher: 3.1102 ms versus 3.0175 ms.
The observed regression is retained, not excluded. All samples had zero response
errors and the requested connection count.

## Independent Linux confirmation

The exploratory Linux series produced 16 ERL wins in 20 adjacent pairs, with a
two-sided sign-test `p = 0.0118179`. That analysis was selected after seeing the
data, so it is **not** an independent acceptance result. All original unpaired
results above remain part of the report.

A separate 40-pair run completed under the advance
[confirmation protocol](CONFIRMATION.md). It used five measured seconds per router
after one warmup second, randomized pair order with seed `20260907`, TLS HTTP/2,
500 routes, 64 concurrent requests, four connections, and two Go processors each
for client and server. ERL ran first in 21 pairs and ServeMux in 19. Every planned
measurement is retained; all 80 had zero response errors and exactly four connections.

| Metric | ServeMux | ERL |
| --- | ---: | ---: |
| Median requests/second | 84,816 | 86,699 |
| Median p50 latency | 0.6609 ms | 0.6430 ms |
| Median p95 latency | 1.5466 ms | 1.5113 ms |
| Median p99 latency | 2.1983 ms | 2.1340 ms |

ERL won **33 of 40 pairs**, with no ties. The primary two-sided exact sign test gives
**p = 0.000042277**, passing the predeclared `p < 0.01` threshold. Overall median
throughput is **2.22% higher**, and the median within-pair change is +2.60%. The
independent confirmation gate passes. The secondary unpaired benchstat throughput
comparison is also significant (`p < 0.001`, n=40), so this series also meets the
original median-throughput and unpaired-significance target.

Secondary unpaired latency comparisons favor ERL for p50 and p95 (`p < 0.001`);
the p99 difference is inconclusive (`p = 0.082`). These are medians of run-level
percentiles, not percentiles pooled across all requests.

See [the paired analysis](results/linux-tls-confirmation/paired.json),
[benchstat output](results/linux-tls-confirmation/comparison.txt),
[all raw measurements](results/linux-tls-confirmation/results.jsonl),
[environment and binary digest](results/linux-tls-confirmation/environment.json),
and [library/workload source checksums](results/linux-tls-confirmation/library-sha256.txt).

This is evidence for the fixed workload on this local Linux VM, not a guarantee
for every deployment. Native macOS throughput remains inconclusive, the short
concurrency-256 sweep observed a regression, and parameter allocations remain
higher. Measure an application's own routes, handlers, concurrency, and hardware
before relying on a throughput improvement.

## Correctness and examples

- `make check` passes formatting, vet, and race-enabled tests.
- The post-optimization routing fuzz run completed approximately 1.5 million cases
  in 30 seconds without a failure.
- Both example binaries build. Smoke tests verified the service example over TLS
  HTTP/2 and cleartext HTTP/2, the middleware example over cleartext HTTP/2, and
  clean SIGTERM shutdown for all three runs.
- Protocol tests also verify HTTP/1.1 fallback, concurrent HTTP/2 streams, streaming,
  request cancellation, and HEAD body suppression.

GitHub Actions configuration has been added but has not run remotely in this local,
unpublished workspace.
