# Developing ERL

Install Go 1.25 or newer. The library and examples use only the standard library;
`go test` does not need to download third-party runtime packages. `make help` lists
the available tasks. No database, container runtime, or code generation is required.

## Everyday workflow

```sh
make fmt
make check
make examples
```

`make check` checks formatting, runs `go vet ./...`, and runs all tests with the race
detector. The tests open ephemeral loopback ports, so a restrictive sandbox may need
permission to listen locally. Do not disable the HTTP tests to work around that.

Use focused tests while iterating:

```sh
go test -run TestRouting .
go test -run TestMiddlewareAndGroups .
go test -run TestHTTPProtocols .
go test ./examples/...
```

Changes to route parsing, precedence, or URL handling should also run `make fuzz`.
Keep a small deterministic regression test for each discovered bug. The differential
fuzzer compares the common semantics with ServeMux; strict-path behavior, automatic
OPTIONS, and the standalone escaped-slash difference have dedicated tests instead.

## Project layout

| Area | Purpose |
| --- | --- |
| `router.go` | Public API, groups, middleware composition, method dispatch, fallbacks |
| `tree.go` | Pattern validation, static-path index, compressed literal prefixes, wildcard matching |
| `router_test.go`, `http_test.go` | Routing contracts and real protocol integration tests |
| `examples/server` | Service-owned registration, TLS/h2c configuration, graceful shutdown |
| `examples/middleware` | Global, group, and route middleware; authentication and request context |
| `internal/benchfixture` | Shared, tested ERL/ServeMux workload |
| `cmd/erlbench` | Separate-process HTTP/2 benchmark runner |
| `cmd/erlbenchstats` | Complete-pair validation and exact paired sign-test analysis |
| `bench` | Methodology, raw measurements, and reported results |

## Design contracts

- Keep `http.Handler`, `http.HandlerFunc`, and standard middleware compatibility.
- Preserve the caller's response writer and request cancellation context.
- Configure routing before serving. Middleware chains are snapshots, not live lists.
- Optimize static and wildcard routing without changing precedence, escaping,
  trailing-slash behavior, or which handler receives a request.
- Populate parameters before middleware runs. Avoid shared or pooled request state
  that handlers might retain after returning.
- Keep the runtime dependency-free and use public Go APIs. Benchmark tooling may
  have separately installed, pinned dependencies.
- Update the public README, examples, and relevant behavioral tests when changing
  an API or routing contract. Exported symbols need Go documentation comments.

## Performance work

Start with the [benchmark guide](bench/README.md). Make a correctness-preserving
change, run focused routing benchmarks, then use the fixed HTTP/2 comparison when
claiming throughput improvements. Keep raw input files, environment information,
sample counts, and statistical output alongside any claim.

Use distinct output directories for each experiment. Do not run benchmarks alongside
builds, race tests, fuzzing, or other load generators. Include parameter-storage
allocations in microbenchmarks; reusing a request after `SetPathValue` hides those costs.
Unchanged or inconclusive HTTP/2 results must be reported as such.

The runner refuses nonempty results directories. `make bench-http2` defaults to
`bench/results/local-tls`; override it with `make bench-http2 BENCH_OUT=bench/results/my-change`.

```sh
make tools build
./bin/erlbench -out bench/results/my-change
./bin/benchstat bench/results/my-change/std.txt bench/results/my-change/erl.txt
```

The runner supports reproducible randomized pair order with `-order random -seed N`.
Choose the sample count, order, and statistical test before collecting data. The
[independent confirmation protocol](bench/CONFIRMATION.md) is one recorded example.
`erlbenchstats` validates complete adjacent pairs and reports a two-sided sign test:

```sh
./bin/erlbenchstats -alpha 0.01 -min-pairs 40 -check bench/results/my-confirmation
```

It emits JSON and exits with status 0 when the requested gate passes, 2 when a
valid series does not pass, or 1 for invalid input. The tool checks measurement
integrity; the recorded experiment protocol must still establish the workload,
environment, and advance choice of analysis. Do not switch tests after seeing a
result and call that same data an independent confirmation.

The GitHub Actions workflow runs formatting, vet, race tests, example builds, and
bounded fuzzing on Go 1.25 and the current stable Go release. Throughput measurements
remain a controlled local task because shared CI hardware is unsuitable for small
performance thresholds. The workflow becomes active once this project is hosted
in a GitHub repository.

Before publishing, select the final module import path and a license, update imports,
and run the checks again. This workspace currently uses the local module name `erl`.
