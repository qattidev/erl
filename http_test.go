package erl_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"erl"
)

func testServer(t *testing.T, h http.Handler, mode string) (*http.Client, string) {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetHTTP2(mode == "tls")
	p.SetUnencryptedHTTP2(mode == "h2c")
	s.Config.Protocols = p
	if mode == "tls" {
		s.EnableHTTP2 = true
		s.StartTLS()
	} else {
		s.Start()
	}
	t.Cleanup(s.Close)
	client := s.Client()
	if mode == "h2c" {
		cp := new(http.Protocols)
		cp.SetUnencryptedHTTP2(true)
		transport := &http.Transport{Protocols: cp}
		t.Cleanup(transport.CloseIdleConnections)
		client = &http.Client{Transport: transport}
	}
	client.Timeout = 5 * time.Second
	return client, s.URL
}

func TestHTTPProtocols(t *testing.T) {
	for _, mode := range []string{"tls", "h2c", "http1"} {
		t.Run(mode, func(t *testing.T) {
			r := erl.New()
			r.POST("/users/:id", func(w http.ResponseWriter, req *http.Request) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("X-User", req.PathValue("id"))
				w.Write(body)
			})
			r.GET("/head", func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, "body") })
			client, address := testServer(t, r, mode)
			req, _ := http.NewRequest("POST", address+"/users/a%2Fb", http.NoBody)
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			wantProto := 2
			if mode == "http1" {
				wantProto = 1
			}
			if res.ProtoMajor != wantProto || res.StatusCode != 200 || res.Header.Get("X-User") != "a/b" {
				t.Fatalf("got %s %d %q", res.Proto, res.StatusCode, res.Header.Get("X-User"))
			}
			res, err = client.Head(address + "/head")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || len(body) != 0 {
				t.Fatalf("HEAD body %q, err %v", body, err)
			}
			// Both HTTP/2 modes also accept HTTP/1.1 clients.
			fallbackTransport := client.Transport.(*http.Transport).Clone()
			fallbackTransport.Protocols = new(http.Protocols)
			fallbackTransport.Protocols.SetHTTP1(true)
			fallbackTransport.ForceAttemptHTTP2 = false
			if fallbackTransport.TLSClientConfig != nil {
				fallbackTransport.TLSClientConfig.NextProtos = []string{"http/1.1"}
			}
			defer fallbackTransport.CloseIdleConnections()
			fallback := &http.Client{Transport: fallbackTransport, Timeout: 5 * time.Second}
			res, err = fallback.Get(address + "/head")
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.ProtoMajor != 1 {
				t.Fatalf("fallback used %s", res.Proto)
			}
		})
	}
}

func TestHTTP2StreamingAndCancellation(t *testing.T) {
	for _, mode := range []string{"tls", "h2c"} {
		t.Run(mode, func(t *testing.T) {
			canceled := make(chan struct{})
			r := erl.New()
			r.GET("/stream", func(w http.ResponseWriter, req *http.Request) {
				io.WriteString(w, "first\n")
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Error(err)
					return
				}
				<-req.Context().Done()
				close(canceled)
			})
			client, address := testServer(t, r, mode)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", address+"/stream", nil)
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			line, err := bufio.NewReader(res.Body).ReadString('\n')
			if err != nil || line != "first\n" {
				t.Fatalf("got %q, %v", line, err)
			}
			cancel()
			select {
			case <-canceled:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not observe cancellation")
			}
		})
	}
}

func TestHTTP2ConcurrentStreams(t *testing.T) {
	r := erl.New()
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	r.GET("/:id", func(w http.ResponseWriter, req *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return
		}
		io.WriteString(w, req.PathValue("id"))
	})
	client, address := testServer(t, r, "tls")
	tr := client.Transport.(*http.Transport)
	tr.MaxConnsPerHost = 1
	// Warm up the single connection without entering the stream handler.
	res, err := client.Get(address + "/missing/route")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	results := make(chan error, 8)
	for range 8 {
		go func() {
			res, err := client.Get(address + "/42")
			if err != nil {
				results <- err
				return
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err == nil && (res.ProtoMajor != 2 || string(body) != "42") {
				err = fmt.Errorf("incorrect stream response: %s %q", res.Proto, body)
			}
			results <- err
		}()
	}
	for range 8 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("requests did not multiplex on one HTTP/2 connection")
		}
	}
	close(release)
	for range 8 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}
