// Example server: go run ./examples/server -protocol h2c
package main

import (
	"context"
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
	"erl/examples/services/user"
)

func main() {
	address := flag.String("addr", "127.0.0.1:8080", "listen address")
	protocol := flag.String("protocol", "http1", "http1, h2c, or tls")
	cert := flag.String("cert", "", "TLS certificate file")
	key := flag.String("key", "", "TLS private key file")
	flag.Parse()
	if err := run(*address, *protocol, *cert, *key); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(address, protocol, cert, key string) error {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	switch protocol {
	case "http1":
	case "h2c":
		p.SetUnencryptedHTTP2(true)
	case "tls":
		if cert == "" || key == "" {
			return fmt.Errorf("-cert and -key are required for TLS")
		}
		p.SetHTTP2(true)
	default:
		return fmt.Errorf("unknown protocol %q", protocol)
	}
	router := erl.New()
	router.Use(requestLogger)
	router.GET("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	user.RegisterRoutes(router.Group("/users"), user.NewService())
	server := &http.Server{Addr: address, Handler: router, Protocols: p, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errors := make(chan error, 1)
	go func() {
		if protocol == "tls" {
			errors <- server.ListenAndServeTLS(cert, key)
		} else {
			errors <- server.ListenAndServe()
		}
	}()
	slog.Info("server starting", "address", address, "protocol", protocol)
	select {
	case err := <-errors:
		return serverError(err)
	case <-ctx.Done():
		stop() // A second interrupt uses the default signal behavior.
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		return serverError(<-errors)
	}
}

func serverError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "route", r.Pattern, "duration", time.Since(start))
	})
}
