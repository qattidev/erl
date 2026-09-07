package erl_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"erl"
)

func response(r http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

func TestRouting(t *testing.T) {
	r := erl.New()
	for _, path := range []string{"/", "/users/new", "/users/:id", "/users/:id/posts/:post", "/files/*path", "/slash/", "/encoded/a%2Fb", "/a/:x/c", "/a/b/d", "/a/*tail", "/%61lias", "/literal/%3Aid", "/double//slash", "/dot/../path"} {
		r.GET(path, func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintf(w, "%s|%s|%s|%s|%s|%s", req.Pattern, req.PathValue("id"), req.PathValue("post"), req.PathValue("path"), req.PathValue("x"), req.PathValue("tail"))
		})
	}
	for _, tc := range []struct{ path, want string }{
		{"/", "GET /|||||"},
		{"/users/new", "GET /users/new|||||"},
		{"/users/42?ignored=yes", "GET /users/:id|42||||"},
		{"/users/a%2Fb", "GET /users/:id|a/b||||"},
		{"/users/%2F", "GET /users/:id|/||||"},
		{"/users/a%252Fb", "GET /users/:id|a%2Fb||||"},
		{"/users/hello%20world", "GET /users/:id|hello world||||"},
		{"/users/100%25", "GET /users/:id|100%||||"},
		{"/users/42/posts/7", "GET /users/:id/posts/:post|42|7|||"},
		{"/files/", "GET /files/*path|||||"},
		{"/files/a/b%2Fc", "GET /files/*path|||a/b/c||"},
		{"/slash/", "GET /slash/|||||"},
		{"/encoded/a%2fb", "GET /encoded/a%2Fb|||||"},
		{"/a/b/c", "GET /a/:x/c||||b|"},
		{"/a/b/d", "GET /a/b/d|||||"},
		{"/a/b/e", "GET /a/*tail|||||b/e"},
		{"/alias", "GET /%61lias|||||"},
		{"/literal/:id", "GET /literal/%3Aid|||||"},
		{"/double//slash", "GET /double//slash|||||"},
		{"/dot/../path", "GET /dot/../path|||||"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := response(r, "GET", tc.path)
			if w.Code != 200 || w.Body.String() != tc.want {
				t.Fatalf("got %d %q, want 200 %q", w.Code, w.Body.String(), tc.want)
			}
		})
	}
	for _, path := range []string{"/missing", "/Users/new", "/users/", "/slash", "/files", "/encoded/a/b", "/users/42/posts", "/users/42/"} {
		if w := response(r, "GET", path); w.Code != 404 {
			t.Errorf("%s: got %d, want 404", path, w.Code)
		}
	}
}

func TestStructuralDuplicatesAndInvalidRegistration(t *testing.T) {
	h := func(http.ResponseWriter, *http.Request) {}
	for _, tc := range []struct {
		name string
		add  func(*erl.Router)
	}{
		{"duplicate", func(r *erl.Router) { r.GET("/x", h); r.GET("/x", h) }},
		{"renamed parameter", func(r *erl.Router) { r.GET("/x/:id", h); r.GET("/x/:name", h) }},
		{"escaped duplicate", func(r *erl.Router) { r.GET("/x/a", h); r.GET("/x/%61", h) }},
		{"empty name", func(r *erl.Router) { r.GET("/:", h) }},
		{"repeated name", func(r *erl.Router) { r.GET("/:id/:id", h) }},
		{"invalid name", func(r *erl.Router) { r.GET("/:a-b", h) }},
		{"partial wildcard", func(r *erl.Router) { r.GET("/x:id", h) }},
		{"nonterminal catchall", func(r *erl.Router) { r.GET("/*path/more", h) }},
		{"bad escape", func(r *erl.Router) { r.GET("/%xx", h) }},
		{"query", func(r *erl.Router) { r.GET("/x?q", h) }},
		{"relative path", func(r *erl.Router) { r.GET("x", h) }},
		{"invalid method", func(r *erl.Router) { r.HandleFunc("GET POST", "/", h) }},
		{"empty method", func(r *erl.Router) { r.HandleFunc("", "/", h) }},
		{"nil function", func(r *erl.Router) { r.GET("/", nil) }},
		{"nil handler", func(r *erl.Router) { r.Handle("GET", "/", nil) }},
		{"nil middleware", func(r *erl.Router) { r.Use(nil) }},
		{"nil route middleware", func(r *erl.Router) { r.GET("/", h, nil) }},
		{"nil middleware result", func(r *erl.Router) { r.Use(func(http.Handler) http.Handler { return nil }) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected registration panic")
				}
			}()
			tc.add(erl.New())
		})
	}
}

func TestMethods(t *testing.T) {
	r := erl.New()
	h := func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Pattern) }
	r.GET("/x", h)
	r.POST("/x", h)
	r.HEAD("/explicit", h)
	r.GET("/explicit", h)
	r.OPTIONS("/explicit", h)
	r.HandleFunc("CUSTOM", "/custom", h)
	for _, tc := range []struct {
		method, path, body, allow string
		code                      int
	}{
		{"GET", "/x", "GET /x", "", 200},
		{"POST", "/x", "POST /x", "", 200},
		{"HEAD", "/x", "GET /x", "", 200},
		{"HEAD", "/explicit", "HEAD /explicit", "", 200},
		{"OPTIONS", "/explicit", "OPTIONS /explicit", "", 200},
		{"OPTIONS", "/x", "", "GET, HEAD, OPTIONS, POST", 204},
		{"PUT", "/x", "Method Not Allowed\n", "GET, HEAD, OPTIONS, POST", 405},
		{"OPTIONS", "/missing", "404 page not found\n", "", 404},
		{"CUSTOM", "/custom", "CUSTOM /custom", "", 200},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := response(r, tc.method, tc.path)
			if w.Code != tc.code || w.Body.String() != tc.body || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("got %d %q Allow=%q", w.Code, w.Body.String(), w.Header().Get("Allow"))
			}
		})
	}
}

func TestMiddlewareAndGroups(t *testing.T) {
	var calls []string
	wrap := func(name string) erl.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, name+" before:"+r.PathValue("id"))
				next.ServeHTTP(w, r)
				calls = append(calls, name+" after")
			})
		}
	}
	r := erl.New()
	r.Use(wrap("global"))
	api := r.Group("/api/", wrap("api"))
	users := api.Group("/users", wrap("users"))
	users.GET("/:id", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(201)
	}, wrap("route"))
	// Later middleware must not change previously created groups or routes.
	api.Use(wrap("late group"))
	r.Use(wrap("late root"))
	w := response(r, "GET", "/api/users/42")
	want := []string{"global before:42", "api before:42", "users before:42", "route before:42", "handler", "route after", "users after", "api after", "global after"}
	if w.Code != 201 || !reflect.DeepEqual(calls, want) {
		t.Fatalf("got %d %v, want %v", w.Code, calls, want)
	}
	for _, method := range []string{"GET", "PUT", "OPTIONS"} {
		calls = nil
		response(r, method, "/api/users/missing/extra")
		want = []string{"global before:", "late root before:", "late root after", "global after"}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("fallback middleware: got %v", calls)
		}
	}
	calls = nil
	response(r, "PUT", "/api/users/42")
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("405 middleware: got %v", calls)
	}
	calls = nil
	response(r, "OPTIONS", "/api/users/42")
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("OPTIONS middleware: got %v", calls)
	}
}

func TestMiddlewareShortCircuitAndWriterIdentity(t *testing.T) {
	r := erl.New()
	w := httptest.NewRecorder()
	r.GET("/", func(http.ResponseWriter, *http.Request) { t.Fatal("handler reached") }, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(got http.ResponseWriter, req *http.Request) {
			if got != w {
				t.Error("response writer was wrapped")
			}
			got.WriteHeader(401)
		})
	})
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestParameterNamesBelongToEndpoint(t *testing.T) {
	r := erl.New()
	h := func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "%s/%s/%s", req.PathValue("id"), req.PathValue("name"), req.PathValue("other"))
	}
	r.GET("/:id/a", h)
	r.GET("/:name/b", h)
	r.POST("/:other/a", h)
	for _, tc := range []struct{ method, path, want string }{{"GET", "/1/a", "1//"}, {"GET", "/2/b", "/2/"}, {"POST", "/3/a", "//3"}} {
		if got := response(r, tc.method, tc.path).Body.String(); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}

func TestConcurrentRequests(t *testing.T) {
	r := erl.New()
	r.GET("/:id", func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, req.PathValue("id")) })
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Go(func() {
			for j := range 100 {
				id := fmt.Sprintf("%d-%d", i, j)
				if got := response(r, "GET", "/"+id).Body.String(); got != id {
					t.Errorf("got %q, want %q", got, id)
				}
			}
		})
	}
	wg.Wait()
}

func TestManyParameters(t *testing.T) {
	r := erl.New()
	var pattern, path strings.Builder
	for i := range 20 {
		fmt.Fprintf(&pattern, "/:p%d", i)
		fmt.Fprintf(&path, "/v%d", i)
	}
	r.GET(pattern.String(), func(w http.ResponseWriter, req *http.Request) {
		for i := range 20 {
			if req.PathValue(fmt.Sprintf("p%d", i)) != fmt.Sprintf("v%d", i) {
				t.Errorf("parameter %d incorrect", i)
			}
		}
	})
	if w := response(r, "GET", path.String()); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestCompressedPrefixesAndInsertionOrder(t *testing.T) {
	patterns := []string{"/long/common/parent/child", "/long/common", "/long/common/:id", "/long/common/:id/details/:post", "/long/common/*tail", "/encoded/a%2Fb/:id", "/slashes//:id"}
	for _, reverse := range []bool{false, true} {
		r := erl.New()
		for j := range patterns {
			i := j
			if reverse {
				i = len(patterns) - 1 - j
			}
			r.GET(patterns[i], func(w http.ResponseWriter, req *http.Request) {
				fmt.Fprintf(w, "%s|%s|%s|%s", req.Pattern, req.PathValue("id"), req.PathValue("post"), req.PathValue("tail"))
			})
		}
		for _, tc := range []struct{ path, want string }{
			{"/long/common/parent/child", "GET /long/common/parent/child|||"},
			{"/long/common", "GET /long/common|||"},
			{"/long/common/parent", "GET /long/common/:id|parent||"},
			{"/long/common/parent/details/42", "GET /long/common/:id/details/:post|parent|42|"},
			{"/long/common/parent/nope", "GET /long/common/*tail|||parent/nope"},
			{"/long/common/", "GET /long/common/*tail|||"},
			{"/long/%63ommon/parent", "GET /long/common/:id|parent||"},
			{"/encoded/a%2Fb/42", "GET /encoded/a%2Fb/:id|42||"},
			{"/slashes//42", "GET /slashes//:id|42||"},
		} {
			if got := response(r, "GET", tc.path).Body.String(); got != tc.want {
				t.Fatalf("reverse=%v %s: got %q, want %q", reverse, tc.path, got, tc.want)
			}
		}
		for _, path := range []string{"/long/commonextra", "/longer/common/parent", "/encoded/a/b/42", "/slashes/42"} {
			if w := response(r, "GET", path); w.Code != 404 {
				t.Fatalf("reverse=%v %s: got %d", reverse, path, w.Code)
			}
		}
	}
}

func FuzzPathMatching(f *testing.F) {
	for _, path := range []string{"/users/a", "/users/a%2Fb", "/files/", "/files/a/b", "/users/a/posts/b", "/users/new", "/a/b/c", "/a/b/d", "/a/b/e"} {
		f.Add(path)
	}
	r := erl.New()
	mux := http.NewServeMux()
	h := func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "%s|%s|%s|%s", req.PathValue("id"), req.PathValue("post"), req.PathValue("path"), req.PathValue("x"))
	}
	for _, pair := range [][2]string{{"/users/:id", "/users/{id}"}, {"/users/:id/posts/:post", "/users/{id}/posts/{post}"}, {"/users/new", "/users/new"}, {"/files/*path", "/files/{path...}"}, {"/a/:x/c", "/a/{x}/c"}, {"/a/b/d", "/a/b/d"}, {"/a/*path", "/a/{path...}"}} {
		r.GET(pair[0], h)
		mux.HandleFunc("GET "+pair[1], h)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if len(path) > 4096 || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n\x00 ?#") {
			t.Skip()
		}
		req, err := http.NewRequest("GET", "http://example.test"+path, nil)
		if err != nil {
			t.Skip()
		}
		// ServeMux treats a decoded standalone slash as its trailing-slash
		// sentinel. ERL allows it inside a named parameter, as tested above.
		for _, part := range strings.Split(req.URL.EscapedPath(), "/") {
			if strings.EqualFold(part, "%2f") {
				t.Skip()
			}
		}
		a, b := httptest.NewRecorder(), httptest.NewRecorder()
		r.ServeHTTP(a, req.Clone(req.Context()))
		mux.ServeHTTP(b, req.Clone(req.Context()))
		// ServeMux cleans paths and redirects subtree roots; ERL is strict.
		if b.Code >= 300 && b.Code < 400 {
			return
		}
		if a.Code != b.Code || a.Body.String() != b.Body.String() {
			t.Fatalf("%q: ERL %d %q, ServeMux %d %q", path, a.Code, a.Body.String(), b.Code, b.Body.String())
		}
	})
}
