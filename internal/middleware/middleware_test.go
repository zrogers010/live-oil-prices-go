package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSONMiddlewareSetsContentType(t *testing.T) {
	wrapped := JSON(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	res := httptest.NewRecorder()

	wrapped(res, req)

	if got := res.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", got)
	}
}

func TestCORSPreflightReturnsOK(t *testing.T) {
	target := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("handler should not run for preflight OPTIONS")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/anything", nil)
	res := httptest.NewRecorder()

	target.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected allow origin *, got %q", got)
	}
}

func TestCORSPassThroughRetainsNext(t *testing.T) {
	called := false
	target := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	res := httptest.NewRecorder()

	target.ServeHTTP(res, req)

	if !called {
		t.Fatalf("expected wrapped handler to be called")
	}
	if res.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", res.Code)
	}
}

func TestRecoveryRecoversPanics(t *testing.T) {
	target := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	res := httptest.NewRecorder()

	target.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", res.Code)
	}
	if body := res.Body.String(); body != "Internal Server Error\n" {
		t.Fatalf("unexpected response body: %q", body)
	}
}
