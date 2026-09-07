// Example middleware server: go run ./examples/middleware -h2c
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"erl"
)

type subjectKey struct{}

func main() {
	address := flag.String("addr", "127.0.0.1:8081", "listen address")
	token := flag.String("token", "demo-token", "example bearer token")
	h2c := flag.Bool("h2c", false, "enable cleartext HTTP/2 with prior knowledge")
	flag.Parse()
	if *token == "" {
		slog.Error("-token must not be empty")
		os.Exit(1)
	}
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(*h2c)
	srv := &http.Server{Addr: *address, Handler: newRouter(*token), Protocols: p, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	slog.Info("middleware example starting", "address", *address, "h2c", *h2c)
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		stop()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = srv.Shutdown(shutdown); err != nil {
			srv.Close()
		} else {
			err = <-done
		}
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func newRouter(token string) *erl.Router {
	r := erl.New()
	r.Use(logRequest)
	r.GET("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	api := r.Group("/api", authenticate(token))
	api.GET("/users/:id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": r.PathValue("id"), "subject": r.Context().Value(subjectKey{}).(string)})
	}, noStore)
	return r
}

func authenticate(token string) erl.Middleware {
	expected := []byte("Bearer " + token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), subjectKey{}, "example-user")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "route", r.Pattern, "duration", time.Since(start))
	})
}
