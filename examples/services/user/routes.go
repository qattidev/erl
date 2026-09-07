// Package user demonstrates a service registering its own routes and dependencies.
package user

import (
	"encoding/json"
	"net/http"

	"erl"
)

// User is the example service's response type.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Service is an immutable example store, safe for concurrent reads.
type Service struct{ users map[string]User }

// NewService creates a small example data set.
func NewService() *Service {
	return &Service{users: map[string]User{"42": {ID: "42", Name: "Ada"}}}
}

// RegisterRoutes mounts this service's routes beneath the supplied group.
func RegisterRoutes(r erl.Registrar, service *Service) {
	r.GET("/:id", service.get, serviceHeader)
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	user, ok := s.users[r.PathValue("id")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func serviceHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Service", "user")
		next.ServeHTTP(w, r)
	})
}
