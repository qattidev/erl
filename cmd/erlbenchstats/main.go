// erlbenchstats analyzes adjacent ERL/ServeMux pairs with an exact sign test.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"slices"
)

type measurement struct {
	Router      string  `json:"router"`
	Protocol    string  `json:"protocol"`
	Run         int     `json:"run"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	Connections int     `json:"connections"`
	Seconds     float64 `json:"seconds"`
	RPS         float64 `json:"requests_per_second"`
}

type environment struct {
	Runs        int    `json:"runs"`
	Connections int    `json:"connections"`
	Protocol    string `json:"protocol"`
}

type summary struct {
	Pairs              int     `json:"pairs"`
	Wins               int     `json:"erl_wins"`
	Losses             int     `json:"erl_losses"`
	Ties               int     `json:"ties"`
	PValue             float64 `json:"two_sided_sign_p"`
	Alpha              float64 `json:"alpha"`
	StandardMedian     float64 `json:"std_median_requests_per_second"`
	ERLMedian          float64 `json:"erl_median_requests_per_second"`
	MedianChange       float64 `json:"median_throughput_change_percent"`
	PairedMedianChange float64 `json:"median_paired_change_percent"`
	Passed             bool    `json:"repeatable_win"`
}

func main() {
	alpha := flag.Float64("alpha", .05, "two-sided significance threshold")
	minimum := flag.Int("min-pairs", 10, "minimum complete pairs required")
	check := flag.Bool("check", false, "exit with status 2 unless the repeatable-win gate passes")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: erlbenchstats [flags] results-directory")
		os.Exit(1)
	}
	s, err := analyze(flag.Arg(0), *alpha, *minimum)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check && !s.Passed {
		os.Exit(2)
	}
}

func analyze(dir string, alpha float64, minimum int) (summary, error) {
	if !(alpha > 0 && alpha < 1) || minimum < 1 {
		return summary{}, fmt.Errorf("alpha must be between 0 and 1; min-pairs must be positive")
	}
	data, err := os.ReadFile(filepath.Join(dir, "environment.json"))
	if err != nil {
		return summary{}, err
	}
	var env environment
	if err := json.Unmarshal(data, &env); err != nil {
		return summary{}, err
	}
	if env.Runs < minimum || env.Connections < 1 || (env.Protocol != "tls" && env.Protocol != "h2c") {
		return summary{}, fmt.Errorf("invalid environment or fewer than %d planned pairs", minimum)
	}
	f, err := os.Open(filepath.Join(dir, "results.jsonl"))
	if err != nil {
		return summary{}, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	var rows []measurement
	for {
		var row measurement
		if err := decoder.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			return summary{}, err
		}
		if row.Requests <= 0 || row.Seconds <= 0 || row.RPS <= 0 || row.Errors != 0 || row.Connections != env.Connections || row.Protocol != env.Protocol || math.Abs(row.RPS-float64(row.Requests)/row.Seconds) > row.RPS*1e-9 {
			return summary{}, fmt.Errorf("invalid or failed measurement in run %d", row.Run)
		}
		rows = append(rows, row)
	}
	if len(rows) != 2*env.Runs {
		return summary{}, fmt.Errorf("incomplete series: got %d measurements, expected %d", len(rows), 2*env.Runs)
	}
	s := summary{Pairs: env.Runs, Alpha: alpha}
	var standard, candidate, relative []float64
	for i := range env.Runs {
		a, b := rows[2*i], rows[2*i+1]
		if a.Run != i+1 || b.Run != i+1 || a.Router == b.Router {
			return summary{}, fmt.Errorf("run %d is not a complete adjacent pair", i+1)
		}
		if a.Router == "std" {
			a, b = b, a
		}
		if a.Router != "erl" || b.Router != "std" {
			return summary{}, fmt.Errorf("unknown router in run %d", i+1)
		}
		standard = append(standard, b.RPS)
		candidate = append(candidate, a.RPS)
		relative = append(relative, 100*(a.RPS/b.RPS-1))
		switch {
		case a.RPS > b.RPS:
			s.Wins++
		case a.RPS < b.RPS:
			s.Losses++
		default:
			s.Ties++
		}
	}
	s.PValue = signP(s.Wins, s.Losses)
	s.StandardMedian, s.ERLMedian = median(standard), median(candidate)
	s.MedianChange = 100 * (s.ERLMedian/s.StandardMedian - 1)
	s.PairedMedianChange = median(relative)
	s.Passed = s.Wins > s.Losses && s.ERLMedian > s.StandardMedian && s.PValue < alpha
	return s, nil
}

func median(values []float64) float64 {
	slices.Sort(values)
	i := len(values) / 2
	if len(values)%2 == 0 {
		return (values[i-1] + values[i]) / 2
	}
	return values[i]
}

// signP computes 2*P(Binomial(wins+losses, 0.5) <= min(wins,losses)),
// capped at 1. Ties are omitted. Integer arithmetic keeps the binomial sum exact
// until the final conversion to float64. See the NIST sign-test reference.
func signP(wins, losses int) float64 {
	n := wins + losses
	var term, sum, factor big.Int
	term.SetInt64(1)
	sum.SetInt64(1)
	for k := 1; k <= min(wins, losses); k++ {
		term.Mul(&term, factor.SetInt64(int64(n-k+1)))
		term.Quo(&term, factor.SetInt64(int64(k)))
		sum.Add(&sum, &term)
	}
	sum.Lsh(&sum, 1)
	denominator := new(big.Int).Lsh(big.NewInt(1), uint(n))
	if sum.Cmp(denominator) >= 0 {
		return 1
	}
	value, _ := new(big.Rat).SetFrac(&sum, denominator).Float64()
	return value
}
