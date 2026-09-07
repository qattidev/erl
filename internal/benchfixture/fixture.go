// Package benchfixture defines the shared ERL/ServeMux comparison workload.
package benchfixture

import (
	"fmt"
	"net/http"
	"strings"

	"erl"
)

// Request is one deterministic workload operation and its expected parameters.
type Request struct {
	Method, Path   string
	ID, Item, Tail string
}

// Body is a preencoded 256-byte JSON response, shared by both implementations.
var Body = []byte("{\"data\":\"" + strings.Repeat("x", 244) + "\"}\n")

// New creates count routes: 40% static, 50% parameterized, 10% catch-all.
// One in five uses POST. A nil handler selects the HTTP workload handler.
func New(kind string, count int, handler http.Handler, middleware bool) (http.Handler, []Request) {
	if count < 10 || count%10 != 0 {
		panic("route count must be a positive multiple of 10")
	}
	if handler == nil {
		handler = http.HandlerFunc(serve)
	}
	var wrappers []erl.Middleware
	if middleware {
		wrappers = []erl.Middleware{header("X-Bench-A"), header("X-Bench-B")}
	}
	var register func(string, string, string, http.Handler)
	var result http.Handler
	switch kind {
	case "erl":
		r := erl.New()
		r.Use(wrappers...)
		register = func(method, path, standard string, h http.Handler) { r.Handle(method, path, h) }
		result = r
	case "std":
		r := http.NewServeMux()
		register = func(method, path, standard string, h http.Handler) {
			for i := len(wrappers) - 1; i >= 0; i-- {
				h = wrappers[i](h)
			}
			r.Handle(method+" "+standard, h)
		}
		result = r
	default:
		panic("unknown router " + kind)
	}
	requests := make([]Request, count)
	for i := range count {
		base := fmt.Sprintf("/api/resources/r%d", i)
		method := http.MethodGet
		if i%5 == 0 {
			method = http.MethodPost
		}
		path, standard := base, base
		req := Request{Method: method, Path: base}
		switch {
		case i < count*4/10:
		case i < count*7/10:
			path, standard, req.Path, req.ID = base+"/:id", base+"/{id}", base+"/42", "42"
		case i < count*9/10:
			path, standard, req.Path, req.ID, req.Item = base+"/:id/items/:item", base+"/{id}/items/{item}", base+"/42/items/7", "42", "7"
		default:
			path, standard, req.Path, req.Tail = base+"/*path", base+"/{path...}", base+"/a/b", "a/b"
		}
		register(method, path, standard, handler)
		requests[i] = req
	}
	return result, requests
}

func header(name string) erl.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(name, "1")
			next.ServeHTTP(w, r)
		})
	}
}

func serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", "256")
	if id := r.PathValue("id"); id != "" {
		w.Header().Set("X-Id", id)
	}
	if item := r.PathValue("item"); item != "" {
		w.Header().Set("X-Item", item)
	}
	if path := r.PathValue("path"); path != "" {
		w.Header().Set("X-Path", path)
	}
	w.Write(Body)
}
