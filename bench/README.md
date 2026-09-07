# Benchmark methodology

The acceptance target is higher median TLS HTTP/2 throughput than `net/http.ServeMux`
with a two-sided significance level of `p < 0.05` across at least ten alternating
measurements per router. Routing microbenchmarks are diagnostic; they do not replace
this end-to-end requirement.

The original comparisons use benchstat's unpaired test. A separate Linux experiment
uses a stricter paired test chosen before collecting its independent data; its
fixed design and acceptance rule are in [CONFIRMATION.md](CONFIRMATION.md). Keep
the original results alongside that experiment.

## Fixed workload

- 500 routes: 200 static, 150 with one parameter, 100 with two parameters, and 50
  catch-alls. One in five routes uses POST; the remainder use GET.
- Workers cycle through the same deterministic route set, with different starting
  offsets. Request proportions converge to the route proportions over each run.
- Every handler reads the same parameter names, sets equivalent response headers,
  and writes the same preencoded 256-byte JSON body. Parameter values appear in
  response headers so the load generator can validate them.
- Two identical lightweight middleware wrappers set fixed headers. Both routers
  precompose the wrappers during registration.
- The fixture translates ERL patterns to equivalent ServeMux patterns. A test
  compares every workload operation's status, body, and headers.
- Primary load: 64 concurrent requests over four persistent HTTP/2 connections,
  four server Go processors, and four client Go processors.
- The load generator and each server run in separate OS processes on the same
  machine. Every measurement starts a fresh server and establishes connections,
  then warms up for one second. Each measured run lasts five seconds plus draining
  the final in-flight requests. Router order alternates each pair.
- TLS and HTTP server configuration, handlers, middleware, clients, and workload
  are identical. The loopback server uses httptest's certificate; only this local
  benchmark client disables certificate verification.
- Every response must use HTTP/2, have status 200, and contain the expected headers
  and body. Measurements with errors or an unexpected connection count fail.
- Timing includes client request construction, network I/O, parameter verification,
  and body reads. Latencies cover full request completion. CPU or scheduling limits
  in the client and transport can mask improvements in routing.

## Reproduce

Run without other CPU-heavy tasks. All comparisons must use the same Go version,
hardware, configuration, and source revision. Record raw measurements before interpreting
them. Do not adjust the workload or stop sampling when a favorable result appears.

```sh
go build -o bin/erlbench ./cmd/erlbench
./bin/erlbench -out bench/results/my-tls
./bin/erlbench -protocol h2c -out bench/results/my-h2c

# Supplemental concurrency measurements, with one connection for one request.
./bin/erlbench -concurrency 1 -connections 1 -out bench/results/my-tls-c1
./bin/erlbench -concurrency 16 -out bench/results/my-tls-c16
./bin/erlbench -concurrency 256 -out bench/results/my-tls-c256
```

Each output directory contains `environment.json`, `results.jsonl`, and separate
`std.txt` and `erl.txt` files in Go benchmark format. New runs also record the
benchmark executable's SHA-256 digest, start time, pair-order policy, and random
seed. The runner refuses a nonempty output directory to preserve previous
measurements. Choose a fresh directory for each experiment.

Use this pinned benchstat version with Go 1.25:

```sh
go install golang.org/x/perf/cmd/benchstat@v0.0.0-20250605212013-b481878a17be
benchstat bench/results/my-tls/std.txt bench/results/my-tls/erl.txt
```

Compare `req/s` for throughput and the percentile metrics for latency. A positive
throughput difference without statistical significance does not pass the target.

## Paired confirmation

Use `make tools build` to install the pinned benchstat and build both the runner
and the dependency-free `erlbenchstats` analysis command. Randomized pair order is
reproducible with `-order random -seed N`;
the default remains alternating. Declare sample count and analysis before starting.

For the [recorded Linux confirmation](CONFIRMATION.md), the existing VM has four
virtual CPUs, so the client and server each use two Go processors. With a local
Linux/arm64 container engine and an already cached Alpine 3.20 image:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/erlbench-linux-arm64 ./cmd/erlbench
mkdir -p bench/results/my-linux-confirmation
docker run --rm --pull=never --network=none \
  -v "$PWD/bin/erlbench-linux-arm64:/erlbench:ro" \
  -v "$PWD/bench/results/my-linux-confirmation:/results" \
  --entrypoint /erlbench docker.io/library/alpine:3.20 \
  -server-procs 2 -client-procs 2 -runs 40 -duration 5s \
  -order random -seed 20260907 -out /results
./bin/erlbenchstats -alpha 0.01 -min-pairs 40 -check \
  bench/results/my-linux-confirmation
./bin/benchstat bench/results/my-linux-confirmation/std.txt \
  bench/results/my-linux-confirmation/erl.txt
```

Use a fresh results directory for each run. Container tooling is optional for
normal development; the runner can also execute directly on a Linux host. Record
that environment separately, since it will not reproduce the VM's absolute rates.

`erlbenchstats` rejects incomplete series, mismatched pairs, response errors,
protocol/connection mismatches, and inconsistent throughput arithmetic. Its JSON
includes pair wins, losses, ties, medians, and the exact two-sided sign-test p-value.
With `-check`, status 2 means the valid data did not pass the requested gate; status
1 means invalid input. A successful check supports only the declared experiment,
not a universal throughput claim.

## Routing microbenchmarks and profiles

Microbenchmarks include a fresh shallow copy of an unpopulated request for every
operation. This includes the same request-object allocation for both routers and
prevents reused parameter maps from hiding ERL's `SetPathValue` allocation cost.
They exclude the HTTP workload's middleware and response serialization, use a
discard writer, and read each supported parameter name in the handler.

```sh
go test -run '^$' -bench BenchmarkDispatch -benchmem -count 10 . > bench/dispatch.txt
benchstat -col '/router@(std erl)' -row '/routes /case' bench/dispatch.txt

# Profile one server; profiling runs are diagnostic, not acceptance samples.
./bin/erlbench -router erl -runs 1 -duration 10s -cpuprofile /tmp/erl.prof
go tool pprof -top /tmp/erl.prof
```

Profile analysis requires the `pprof` tool from a full Go installation; some minimal
toolchain installations omit it. This does not affect running the benchmarks or tests.

See [RESULTS.md](RESULTS.md) for the recorded results and acceptance status.
