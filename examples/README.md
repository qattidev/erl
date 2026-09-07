# Runnable examples

Run these commands from the repository root. Both servers handle SIGINT/SIGTERM with
a five-second graceful shutdown deadline. They bind to loopback by default and can
run at the same time on different ports.

## Service registration and HTTP/2

```sh
go run ./examples/server -protocol h2c
curl http://127.0.0.1:8080/health
curl --http2-prior-knowledge http://127.0.0.1:8080/users/42
```

The user response is `{"id":"42","name":"Ada"}`. An unknown ID returns 404.
[The server](server/main.go) creates a router, adds global logging, and calls
`user.RegisterRoutes(router.Group("/users"), user.NewService())`.
[The user package](services/user/routes.go) owns its handler, service data, and a
per-route middleware wrapper that sets `X-Service: user`.

For TLS, use a certificate trusted by your client:

```sh
go run ./examples/server -protocol tls -addr :8443 -cert cert.pem -key key.pem
curl --http2 https://localhost:8443/users/42
```

Omitting `-protocol` selects HTTP/1.1. Cleartext HTTP/2 uses prior knowledge and must
be explicitly enabled. A curl build with HTTP/2 support is required for the HTTP/2
commands; the Go integration tests exercise both modes without curl.

## Middleware composition and request context

```sh
go run ./examples/middleware -h2c
curl http://127.0.0.1:8081/health
curl -i http://127.0.0.1:8081/api/users/42
curl --http2-prior-knowledge -H 'Authorization: Bearer demo-token' \
  http://127.0.0.1:8081/api/users/42
```

The unauthenticated request returns 401 and `WWW-Authenticate: Bearer`. With the
example token, the response is `{"id":"42","subject":"example-user"}` and includes
`Cache-Control: no-store`.

[This example](middleware/main.go) demonstrates all three middleware levels:

1. Global logging records the registered route and request duration.
2. Group authentication short-circuits unauthorized requests and adds a typed
   request-context value for authorized requests.
3. Route middleware sets a response header before the handler reads both the
   context value and `r.PathValue("id")`.

The fixed token and subject are demonstration data. Replace `authenticate` with your
application's identity provider when adapting the example. `-token` changes the
demo token, and `-addr` changes the listen address.

## Build and test

```sh
make examples
./bin/erl-example-server -protocol h2c
./bin/erl-example-middleware -h2c
go test ./examples/...
```

Run the two server commands in separate terminals, or stop one before starting the
next. Tests verify package registration, router isolation, authentication
short-circuiting, route middleware, and context/parameter propagation.
