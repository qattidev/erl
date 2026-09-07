package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareExample(t *testing.T) {
	r := newRouter("test-token")
	for _, tc := range []struct {
		path, auth string
		code       int
	}{
		{"/health", "", 200},
		{"/api/users/42", "", 401},
		{"/api/users/42", "Bearer wrong", 401},
		{"/api/users/42", "Bearer test-token", 200},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s: got %d, want %d", tc.path, w.Code, tc.code)
		}
		if tc.code == http.StatusUnauthorized {
			if w.Header().Get("WWW-Authenticate") != "Bearer" || w.Header().Get("Cache-Control") != "" {
				t.Fatal("authentication did not short-circuit the route middleware")
			}
		}
		if tc.auth == "Bearer test-token" {
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["id"] != "42" || body["subject"] != "example-user" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("incorrect context, parameter, or middleware response: %v", body)
			}
		}
	}
}
