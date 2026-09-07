package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestExactSignTest(t *testing.T) {
	for _, tc := range []struct {
		wins, losses int
		want         float64
	}{
		{0, 0, 1}, {5, 5, 1}, {1, 0, 1}, {9, 1, .021484375}, {16, 4, .01181793212890625}, {4, 16, .01181793212890625}, {40, 0, math.Ldexp(2, -40)},
	} {
		if got := signP(tc.wins, tc.losses); got != tc.want {
			t.Errorf("%d/%d: got %.16g, want %.16g", tc.wins, tc.losses, got, tc.want)
		}
	}
}

func sampleSeries() []measurement {
	var rows []measurement
	for i := range 10 {
		a := measurement{Router: "erl", Protocol: "tls", Run: i + 1, Requests: 110, Connections: 4, Seconds: 1, RPS: 110}
		b := measurement{Router: "std", Protocol: "tls", Run: i + 1, Requests: 100, Connections: 4, Seconds: 1, RPS: 100}
		if i%2 == 0 {
			a, b = b, a
		}
		rows = append(rows, a, b)
	}
	return rows
}

func writeSeries(t *testing.T, rows []measurement) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(`{"runs":10,"connections":4,"protocol":"tls"}`), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(f)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAnalyzePairs(t *testing.T) {
	s, err := analyze(writeSeries(t, sampleSeries()), .01, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Passed || s.Wins != 10 || s.Losses != 0 || s.StandardMedian != 100 || s.ERLMedian != 110 || s.PValue != .001953125 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	rows := sampleSeries()
	for i := range rows {
		rows[i].Requests = 100
		rows[i].RPS = 100
	}
	s, err = analyze(writeSeries(t, rows), .01, 10)
	if err != nil {
		t.Fatal(err)
	}
	if s.Passed || s.Ties != 10 || s.PValue != 1 {
		t.Fatalf("ties should not establish a win: %+v", s)
	}
}

func TestRejectInvalidMeasurements(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]measurement) []measurement
	}{
		{"incomplete", func(r []measurement) []measurement { return r[:len(r)-1] }},
		{"duplicate", func(r []measurement) []measurement { r[1] = r[0]; return r }},
		{"not adjacent", func(r []measurement) []measurement { r[1], r[2] = r[2], r[1]; return r }},
		{"response errors", func(r []measurement) []measurement { r[0].Errors = 1; return r }},
		{"connection mismatch", func(r []measurement) []measurement { r[0].Connections = 5; return r }},
		{"protocol mismatch", func(r []measurement) []measurement { r[0].Protocol = "h2c"; return r }},
		{"incorrect throughput", func(r []measurement) []measurement { r[0].RPS = 1000; return r }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := analyze(writeSeries(t, tc.change(sampleSeries())), .05, 10); err == nil {
				t.Fatal("invalid series accepted")
			}
		})
	}
}
