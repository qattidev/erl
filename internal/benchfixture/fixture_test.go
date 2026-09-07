package benchfixture

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestEquivalentWorkload(t *testing.T) {
	a, requests := New("erl", 500, nil, true)
	b, _ := New("std", 500, nil, true)
	for _, op := range requests {
		x, y := httptest.NewRecorder(), httptest.NewRecorder()
		a.ServeHTTP(x, httptest.NewRequest(op.Method, op.Path, nil))
		b.ServeHTTP(y, httptest.NewRequest(op.Method, op.Path, nil))
		if x.Code != 200 || y.Code != 200 || !bytes.Equal(x.Body.Bytes(), Body) || !bytes.Equal(y.Body.Bytes(), Body) {
			t.Fatalf("incorrect response: %+v", op)
		}
		for _, header := range []string{"Content-Type", "Content-Length", "X-Id", "X-Item", "X-Path", "X-Bench-A", "X-Bench-B"} {
			if x.Header().Get(header) != y.Header().Get(header) {
				t.Fatalf("%s differs for %+v", header, op)
			}
		}
	}
}
