package erl_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"erl/internal/benchfixture"
)

type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header       { return w.header }
func (*discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (*discardWriter) WriteHeader(int)             {}

var parameterSink string

func BenchmarkDispatch(b *testing.B) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parameterSink = r.PathValue("id")
		parameterSink = r.PathValue("item")
		parameterSink = r.PathValue("path")
		w.WriteHeader(204)
	})
	for _, count := range []int{10, 500, 5000} {
		for _, kind := range []string{"erl", "std"} {
			router, requests := benchfixture.New(kind, count, h, false)
			for _, tc := range []struct {
				name  string
				index int
			}{{"static", 1}, {"param", count * 4 / 10}, {"two_params", count * 7 / 10}, {"catchall", count * 9 / 10}} {
				b.Run(fmt.Sprintf("routes=%d/case=%s/router=%s", count, tc.name, kind), func(b *testing.B) {
					op := requests[tc.index]
					base := httptest.NewRequest(op.Method, op.Path, nil)
					w := &discardWriter{header: make(http.Header)}
					b.ReportAllocs()
					for b.Loop() {
						// Never reuse populated PathValue storage between iterations.
						req := *base
						router.ServeHTTP(w, &req)
					}
				})
			}
			b.Run(fmt.Sprintf("routes=%d/case=mixed/router=%s", count, kind), func(b *testing.B) {
				bases := make([]*http.Request, len(requests))
				for i, op := range requests {
					bases[i] = httptest.NewRequest(op.Method, op.Path, nil)
				}
				w := &discardWriter{header: make(http.Header)}
				i := 0
				b.ReportAllocs()
				for b.Loop() {
					req := *bases[i]
					router.ServeHTTP(w, &req)
					i++
					if i == len(bases) {
						i = 0
					}
				}
			})
		}
	}
}
