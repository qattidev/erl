// erlbench compares separate HTTP/2 server processes on a fixed REST workload.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"erl/internal/benchfixture"
)

var (
	role        = flag.String("role", "run", "run or server (internal subprocess)")
	kind        = flag.String("router", "both", "both, erl, or std")
	protocol    = flag.String("protocol", "tls", "tls or h2c")
	runs        = flag.Int("runs", 10, "measurements per router")
	duration    = flag.Duration("duration", 5*time.Second, "measurement duration")
	warmup      = flag.Duration("warmup", time.Second, "warmup duration")
	concurrency = flag.Int("concurrency", 64, "concurrent requests")
	connections = flag.Int("connections", 4, "persistent HTTP/2 connections")
	routeCount  = flag.Int("routes", 500, "route count (multiple of 10)")
	serverProcs = flag.Int("server-procs", 4, "server GOMAXPROCS")
	clientProcs = flag.Int("client-procs", 4, "load generator GOMAXPROCS")
	output      = flag.String("out", "", "directory for raw JSONL and benchstat input")
	profile     = flag.String("cpuprofile", "", "server CPU profile (single-router, single-run only)")
	order       = flag.String("order", "alternating", "pair order: alternating or random")
	seed        = flag.Uint64("seed", 1, "seed for randomized pair order")
)

type result struct {
	Router      string  `json:"router"`
	Protocol    string  `json:"protocol"`
	Run         int     `json:"run"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	Connections int     `json:"connections"`
	Seconds     float64 `json:"seconds"`
	RPS         float64 `json:"requests_per_second"`
	P50         float64 `json:"p50_ms"`
	P95         float64 `json:"p95_ms"`
	P99         float64 `json:"p99_ms"`
}

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if *protocol != "tls" && *protocol != "h2c" {
		return fmt.Errorf("protocol must be tls or h2c")
	}
	if *routeCount < 10 || *routeCount%10 != 0 || *serverProcs < 1 || *clientProcs < 1 {
		return fmt.Errorf("invalid route count or process count")
	}
	if *role == "server" {
		return serve()
	}
	if *role != "run" || (*kind != "both" && *kind != "erl" && *kind != "std") {
		return fmt.Errorf("invalid role or router")
	}
	if *connections < 1 || *concurrency < *connections || *concurrency%*connections != 0 || *runs < 1 || *duration <= 0 || *warmup < 0 {
		return fmt.Errorf("concurrency must be a positive multiple of connections; runs and duration must be positive")
	}
	if *profile != "" && (*runs != 1 || *kind == "both") {
		return fmt.Errorf("CPU profiling requires one run and one router")
	}
	if *order != "alternating" && *order != "random" {
		return fmt.Errorf("order must be alternating or random")
	}
	rng := rand.New(rand.NewPCG(*seed, *seed+1))
	runtime.GOMAXPROCS(*clientProcs)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var raw io.Writer = io.Discard
	files := make(map[string]*os.File)
	if *output != "" {
		if err := prepareOutput(*output); err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(*output, "results.jsonl"))
		if err != nil {
			return err
		}
		defer f.Close()
		raw = f
		for _, name := range []string{"erl", "std"} {
			f, err := os.Create(filepath.Join(*output, name+".txt"))
			if err != nil {
				return err
			}
			defer f.Close()
			files[name] = f
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		binary, err := os.ReadFile(executable)
		if err != nil {
			return err
		}
		metadata := map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "server_procs": *serverProcs, "client_procs": *clientProcs, "concurrency": *concurrency, "connections": *connections, "routes": *routeCount, "duration": duration.String(), "warmup": warmup.String(), "runs": *runs, "protocol": *protocol, "binary_sha256": fmt.Sprintf("%x", sha256.Sum256(binary)), "order": *order, "seed": *seed, "started_at": time.Now().UTC().Format(time.RFC3339)}
		data, _ := json.MarshalIndent(metadata, "", "  ")
		if err := os.WriteFile(filepath.Join(*output, "environment.json"), append(data, '\n'), 0644); err != nil {
			return err
		}
	}
	for i := range *runs {
		pairOrder := []string{*kind}
		if *kind == "both" {
			pairOrder = []string{"erl", "std"}
			reverse := i%2 == 1
			if *order == "random" {
				reverse = rng.IntN(2) == 1
			}
			if reverse {
				pairOrder[0], pairOrder[1] = pairOrder[1], pairOrder[0]
			}
		}
		for _, name := range pairOrder {
			r, err := trial(ctx, name, i+1)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(io.MultiWriter(os.Stdout, raw)).Encode(r); err != nil {
				return err
			}
			if f := files[name]; f != nil {
				if _, err := fmt.Fprintf(f, "BenchmarkHTTP2/protocol=%s/routes=%d/concurrency=%d-%d %d %.4f ns/op %.4f req/s %.4f p50-ms %.4f p95-ms %.4f p99-ms\n", *protocol, *routeCount, *concurrency, *serverProcs, r.Requests, 1e9/r.RPS, r.RPS, r.P50, r.P95, r.P99); err != nil {
					return err
				}
			}
			if r.Errors > 0 || r.Connections != *connections {
				return fmt.Errorf("invalid measurement: %d errors, %d connections", r.Errors, r.Connections)
			}
		}
	}
	return nil
}

// prepareOutput keeps a later experiment from overwriting recorded evidence.
func prepareOutput(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("output directory %q is not empty; choose a new directory to preserve previous measurements", dir)
	}
	return nil
}

func serve() error {
	if *kind != "erl" && *kind != "std" {
		return fmt.Errorf("server router must be erl or std")
	}
	runtime.GOMAXPROCS(*serverProcs)
	if *profile != "" {
		f, err := os.Create(*profile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	h, _ := benchfixture.New(*kind, *routeCount, nil, true)
	s := httptest.NewUnstartedServer(h)
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetHTTP2(*protocol == "tls")
	p.SetUnencryptedHTTP2(*protocol == "h2c")
	s.Config.Protocols = p
	s.Config.ReadHeaderTimeout = 5 * time.Second
	if *protocol == "tls" {
		s.EnableHTTP2 = true
		s.StartTLS()
	} else {
		s.Start()
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"url": s.URL}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func trial(ctx context.Context, name string, iteration int) (result, error) {
	exe, err := os.Executable()
	if err != nil {
		return result{}, err
	}
	cmd := exec.CommandContext(ctx, exe, "-role", "server", "-router", name, "-protocol", *protocol, "-routes", strconv.Itoa(*routeCount), "-server-procs", strconv.Itoa(*serverProcs), "-cpuprofile", *profile)
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return result{}, err
	}
	if err := cmd.Start(); err != nil {
		return result{}, err
	}
	defer func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() }()
	var ready struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(bufio.NewReader(pipe)).Decode(&ready); err != nil {
		return result{}, fmt.Errorf("server startup: %w", err)
	}
	clients := make([]*http.Client, *connections)
	var seen sync.Map
	for i := range clients {
		p := new(http.Protocols)
		p.SetHTTP2(*protocol == "tls")
		p.SetUnencryptedHTTP2(*protocol == "h2c")
		tr := &http.Transport{
			Protocols:       p,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Only the subprocess's loopback test certificate.
			MaxConnsPerHost: 1,
		}
		defer tr.CloseIdleConnections()
		clients[i] = &http.Client{Transport: tr, Timeout: 5 * time.Second}
	}
	_, ops := benchfixture.New("std", *routeCount, nil, false)
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { seen.Store(info.Conn, true) }}
	clientCtx := httptrace.WithClientTrace(ctx, trace)
	// Establish each connection before starting concurrent streams.
	for _, client := range clients {
		if err := perform(clientCtx, client, ready.URL, ops[0]); err != nil {
			return result{}, err
		}
	}
	if *warmup > 0 {
		_, errs, _ := load(clientCtx, clients, ready.URL, ops, *warmup)
		if errs > 0 {
			return result{}, fmt.Errorf("warmup had %d errors", errs)
		}
	}
	latencies, errs, elapsed := load(clientCtx, clients, ready.URL, ops, *duration)
	if ctx.Err() != nil {
		return result{}, ctx.Err()
	}
	nconn := 0
	seen.Range(func(_, _ any) bool { nconn++; return true })
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	percentile := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return float64(latencies[int(float64(len(latencies)-1)*p)]) / float64(time.Millisecond)
	}
	return result{Router: name, Protocol: *protocol, Run: iteration, Requests: len(latencies), Errors: errs, Connections: nconn, Seconds: elapsed.Seconds(), RPS: float64(len(latencies)) / elapsed.Seconds(), P50: percentile(.50), P95: percentile(.95), P99: percentile(.99)}, nil
}

func perform(ctx context.Context, client *http.Client, address string, op benchfixture.Request) error {
	req, err := http.NewRequestWithContext(ctx, op.Method, address+op.Path, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return err
	}
	if res.ProtoMajor != 2 || res.StatusCode != 200 || !bytes.Equal(body, benchfixture.Body) || res.Header.Get("X-Id") != op.ID || res.Header.Get("X-Item") != op.Item || res.Header.Get("X-Path") != op.Tail || res.Header.Get("X-Bench-A") != "1" || res.Header.Get("X-Bench-B") != "1" {
		return fmt.Errorf("incorrect response for %s %s: %s %d", op.Method, op.Path, res.Proto, res.StatusCode)
	}
	return nil
}

func load(ctx context.Context, clients []*http.Client, address string, ops []benchfixture.Request, length time.Duration) ([]time.Duration, int, time.Duration) {
	type sample struct {
		latencies []time.Duration
		errors    int
	}
	samples := make([]sample, *concurrency)
	start := time.Now()
	deadline := start.Add(length)
	var wg sync.WaitGroup
	for worker := range samples {
		wg.Go(func() {
			s := &samples[worker]
			s.latencies = make([]time.Duration, 0, int(length.Seconds()*2000))
			index := (worker * 17) % len(ops)
			client := clients[worker%len(clients)]
			for time.Now().Before(deadline) && ctx.Err() == nil {
				begin := time.Now()
				if err := perform(ctx, client, address, ops[index]); err != nil {
					s.errors++
				} else {
					s.latencies = append(s.latencies, time.Since(begin))
				}
				index++
				if index == len(ops) {
					index = 0
				}
			}
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	var latencies []time.Duration
	errors := 0
	for _, s := range samples {
		latencies = append(latencies, s.latencies...)
		errors += s.errors
	}
	return latencies, errors, elapsed
}
