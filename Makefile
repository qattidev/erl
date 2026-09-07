.PHONY: help check fmt fmt-check test vet fuzz bench bench-http2 build examples tools

BENCH_OUT ?= bench/results/local-tls

help:
	@printf '%s\n' 'make check       Format check, vet, and race tests' 'make fmt         Format Go sources' 'make fuzz        Fuzz routing for 30 seconds' 'make examples    Build both example servers into bin/' 'make build       Build the HTTP/2 benchmark and analysis tools' 'make bench       Run routing microbenchmarks' 'make bench-http2 Run the full TLS HTTP/2 comparison' 'make tools       Install pinned benchstat into bin/'

check: fmt-check vet test

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

test:
	go test -race ./...

vet:
	go vet ./...

fuzz:
	go test -run '^$$' -fuzz FuzzPathMatching -fuzztime 30s .

bench:
	go test -run '^$$' -bench BenchmarkDispatch -benchmem -count 10 .

build:
	go build -o bin/erlbench ./cmd/erlbench
	go build -o bin/erlbenchstats ./cmd/erlbenchstats

bench-http2: build
	./bin/erlbench -out "$(BENCH_OUT)"

examples:
	go build -o bin/erl-example-server ./examples/server
	go build -o bin/erl-example-middleware ./examples/middleware

tools:
	GOBIN="$(CURDIR)/bin" go install golang.org/x/perf/cmd/benchstat@v0.0.0-20250605212013-b481878a17be
