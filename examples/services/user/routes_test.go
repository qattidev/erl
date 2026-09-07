package user_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"erl"
	"erl/examples/services/user"
)

func TestPackageRegistration(t *testing.T) {
	r := erl.New()
	user.RegisterRoutes(r.Group("/api/users"), user.NewService())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/users/42", nil))
	var got user.User
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.ID != "42" || got.Name != "Ada" || w.Header().Get("X-Service") != "user" {
		t.Fatalf("unexpected response: %d %+v", w.Code, got)
	}
	other := erl.New()
	w = httptest.NewRecorder()
	other.ServeHTTP(w, httptest.NewRequest("GET", "/api/users/42", nil))
	if w.Code != 404 {
		t.Fatal("registrations leaked between router instances")
	}
}
