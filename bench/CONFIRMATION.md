# Independent paired confirmation protocol

This protocol is recorded before collecting `results/linux-tls-confirmation`.
Earlier unpaired comparisons remain in the report and are not replaced.

The exploratory Linux series drifted from roughly 89,000 to 80,000 requests/second
over time. ERL won 16 of 20 adjacent pairs, but the unpaired benchstat comparison
was inconclusive. That motivates a separate experiment using the existing paired
design explicitly; the exploratory sign-test result is not an acceptance result.

## Fixed collection and analysis

- Keep the library implementation and mixed REST workload unchanged.
- Use the existing Linux/arm64 Podman VM, its four virtual CPUs, and the cached
  `docker.io/library/alpine:3.20` image. Use no external container networking.
- Use two Go processors each for server and client, TLS HTTP/2, 500 routes,
  64 concurrent requests, and four persistent connections.
- Collect **40 complete adjacent pairs**, with five measured seconds per router
  after one warmup second. Select each pair's order using an independent seeded
  pseudorandom coin (`-order random -seed 20260907`).
- Collect every planned pair. Do not inspect intermediate significance or stop
  because a favorable result appears. Response errors or connection mismatches
  invalidate the run; they must not be discarded as individual outliers.
- Primary confirmatory statistic: the **two-sided exact paired sign test**, with
  ERL wins/losses determined by each pair's requests/second. Exclude exact ties.
- Require **p < 0.01**, more ERL wins than losses, and higher overall median ERL
  throughput. This threshold is stricter than the original 0.05 target.
- Retain the usual benchstat comparison, all raw samples, latency percentiles,
  environment information, and executable digest regardless of the outcome.
- Treat this as a controlled local VM result. Do not claim that every deployment,
  the native macOS benchmark, or other concurrency levels will improve.

The [NIST sign-test reference](https://www.itl.nist.gov/div898/software/dataplot/refman1/auxillar/signtest.htm)
describes the paired test, excluded ties, and the binomial distribution under the
null hypothesis. Pairing helps account for slow drift between adjacent measurements;
it does not eliminate all VM noise or establish a universal performance guarantee.

The exact integer-binomial implementation is tested in `cmd/erlbenchstats`.

```sh
go run ./cmd/erlbenchstats -alpha 0.01 -min-pairs 40 -check \
  bench/results/linux-tls-confirmation
```

If this check fails, report the throughput target as unproven. Do not change the
threshold, drop measurements, or reinterpret the original results as a pass.
