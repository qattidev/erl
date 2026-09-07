package erl_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"erl"
)

func ExampleNew() {
	router := erl.New()
	router.GET("/hello/:name", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %s!", r.PathValue("name"))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/hello/Ada", nil))
	fmt.Println(w.Body.String())
	// Output: Hello, Ada!
}

func ExampleGroup() {
	// A service can export a function with this signature from its own package.
	registerUsers := func(r erl.Registrar) {
		r.GET("/:id", func(w http.ResponseWriter, req *http.Request) {
			fmt.Fprintln(w, req.PathValue("id"))
		})
	}
	router := erl.New()
	registerUsers(router.Group("/api/users"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/users/42", nil))
	fmt.Print(w.Body.String())
	// Output: 42
}

func ExampleMiddleware() {
	var authentication erl.Middleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	router := erl.New()
	router.Group("/private", authentication).GET("/profile", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/private/profile", nil))
	fmt.Println(w.Code)
	// Output: 401
}
