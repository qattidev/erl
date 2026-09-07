# ERL (Enhanced Routing Library)

ERL is a Go HTTP router with Gin-like registration, standard `net/http` handlers,
route groups, and middleware. It implements `http.Handler`, so HTTP/2, TLS,
streaming, cancellation, and shutdown remain under `http.Server`'s control.

Requires Go 1.25 or newer. There are no external runtime dependencies. The local
module path is `erl`; set the repository's published module path and update imports
before publishing it. Another local module can use `require erl v0.0.0` together
with `replace erl => /path/to/erl`.

## Register routes

```go
router := erl.New()
router.GET("/health", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("ok\n"))
})

router.GET("/users/:id", func(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintln(w, r.PathValue("id"))
})

server := &http.Server{
    Addr:    ":8080",
    Handler: router,
}
err := server.ListenAndServe()
```

`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, and `OPTIONS` accept a path,
an `http.HandlerFunc`, and optional middleware. Use
`Handle(method, path, http.Handler, middleware...)` for a handler object, or
`HandleFunc(method, path, http.HandlerFunc, middleware...)` for any HTTP method.
Methods match case-sensitively.

## Middleware and groups

Middleware has type `func(http.Handler) http.Handler`. The first wrapper is
outermost. Returning without calling `next.ServeHTTP` stops the chain.

```go
func requestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        log.Printf("%s %s %s", r.Method, r.Pattern, time.Since(start))
    })
}

router := erl.New()
router.Use(requestLogger)
api := router.Group("/api")
private := api.Group("/private", authentication)
private.GET("/profile", profileHandler, rateLimit)
```

Middleware runs in global → outer group → inner group → route order, then unwinds
in reverse. `r.PathValue` and `r.Pattern` are available before the wrappers run.
The original response writer is passed through, so `http.ResponseController`,
flushing, and supported optional writer interfaces work normally.

Groups copy their parent's middleware at creation, and routes capture their chain
at registration. Call `Use` before registering the routes or creating the groups
that should inherit it. Later `Use` calls do not rewrite existing routes or groups.
Generated 404, 405, and OPTIONS responses use the root router's current middleware;
they do not run group or per-route middleware.

Configure everything before serving. Concurrent requests are supported. Registering
or changing middleware concurrently with requests is not supported. Create routers
with `New`, keep the returned pointer, and do not copy a router after creation.

## Register from a service package

Each package exports a registration function and accepts its dependencies directly:

```go
// package user
func RegisterRoutes(r erl.Registrar, service *Service) {
    r.GET("/:id", service.getUser)
}

// package main, during startup
router := erl.New()
user.RegisterRoutes(router.Group("/users"), userService)
```

`erl.Registrar` is implemented by both `*erl.Router` and `*erl.Group`. Services do
not need to import the application package or use a global registry. The runnable
[server](examples/server/main.go) mounts the [user service](examples/services/user/routes.go)
this way. Independent router instances have independent registrations.

## Matching rules

| Pattern | Meaning |
| --- | --- |
| `/users/new` | Exact static path |
| `/users/:id` | One nonempty path segment, read with `r.PathValue("id")` |
| `/files/*path` | Remaining path, including slashes, read with `r.PathValue("path")` |

- Match priority is static segment, named segment, then catch-all. If a branch
  cannot complete, matching backtracks. Priority is independent of registration order.
- Catch-alls must be terminal. `/files/*path` matches `/files/` with an empty value
  and `/files/a/b` with `a/b`; it does not match `/files`. Captures omit the leading slash.
- Wildcards occupy whole segments. Names use ASCII letters, digits, and underscores,
  cannot start with a digit, and cannot repeat in one route.
- Paths are case-sensitive. Trailing slashes, repeated slashes, and dot segments are
  literal. ERL does not clean paths or issue automatic redirects. Queries do not
  affect routing. Prefixes and relative route paths must start with `/` or be empty.
  Group prefixes have trailing slashes removed; an empty route path mounts at the
  group's exact prefix, while `/` preserves a trailing slash.
- Escapes are decoded per segment. `/users/a%2Fb` captures `a/b` as one value;
  `/users/%2F` captures `/`. Escaped literal route segments work too. Malformed
  registration escapes panic. This differs from ServeMux's standalone `%2F` handling.
- Malformed patterns, structurally duplicate routes for the same method, nil
  handler functions, and nil middleware panic at registration. Parameter names do
  not distinguish structurally identical routes. Distinct suffixes may use different names.
- Explicit HEAD routes take priority; otherwise HEAD uses GET. `http.Server`
  suppresses the response body for HEAD, as it does with standard Go handlers.
- Explicit OPTIONS routes take priority. Otherwise, a known path returns 204 and a
  sorted `Allow` header. Unsupported methods return 405 with `Allow`; unknown paths
  return 404. `Allow` includes implicit HEAD and OPTIONS support.
- `r.Pattern` is the registered method plus the full ERL path, for example
  `GET /users/:id`. It is empty for generated fallback responses.

The core library provides routing and composition. Serialization, authentication,
logging, and panic recovery can be supplied through standard handlers and middleware.

## HTTP/2 examples

Plain HTTP/1.1:

```sh
go run ./examples/server
curl http://127.0.0.1:8080/users/42
```

Opt-in cleartext HTTP/2 with HTTP/1.1 on the same port:

```sh
go run ./examples/server -protocol h2c
curl --http2-prior-knowledge http://127.0.0.1:8080/users/42
```

TLS HTTP/2 with HTTP/1.1 fallback, using your certificate and key:

```sh
go run ./examples/server -protocol tls -addr :8443 -cert cert.pem -key key.pem
curl --http2 https://localhost:8443/users/42
```

These curl commands require a curl build with HTTP/2 support. Cleartext HTTP/2 uses
prior knowledge; the deprecated HTTP/1.1 `Upgrade: h2c` mechanism is not supported.
Protocol selection uses Go's [Server.Protocols API](https://go.dev/doc/go1.24#net/http).
The example handles SIGINT/SIGTERM with a five-second graceful shutdown deadline.
For your own server, configure `http.Server.Protocols` and use ERL as its `Handler`.

## Test and benchmark

```sh
make test
make vet
make fuzz
make bench
make bench-http2
```

The tests cover matching, middleware, service-package registration, actual TLS and
cleartext HTTP/2 negotiation, HTTP/1.1 fallback, concurrent streams, streaming, and
cancellation. Network tests require loopback sockets.

See [the runnable examples](examples/README.md) and [contributor guide](CONTRIBUTING.md)
for the development workflow, package layout, and extension guidelines.

The [recorded independent Linux VM comparison](bench/RESULTS.md) measured **2.22%
higher median TLS HTTP/2 throughput** than ServeMux across 40 pairs (33 ERL wins;
paired p = 0.0000423). This is specific to the tested workload and environment:
native macOS throughput was inconclusive, and ERL allocates more memory for route
parameters. See [the benchmark methodology](bench/README.md) to reproduce the
comparison and evaluate your application's workload.
