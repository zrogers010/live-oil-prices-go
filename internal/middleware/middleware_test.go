package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogging_WithRetryCount(t *testing.T) {
	logged := ""
	logFunc := func(format string, args ...interface{}) {
		logged = args[0].(string) // simplistic capture for test verification
	}

	// Patch log.Printf temporarily
	oldLogPrintf := logPrintf
	logPrintf = logFunc
	defer func() { logPrintf = oldLogPrintf }()

	hr := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))

	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", "test-id")
	r.Header.Set("X-Retry-Count", "3")

	w := httptest.NewRecorder()
	hr.ServeHTTP(w, r)

	if logged == "" {
		t.Error("Expected log output, got empty string")
	}
}

func TestLogging_WithoutRetryCount(t *testing.T) {
	logged := ""
	logFunc := func(format string, args ...interface{}) {
		logged = args[0].(string) // simplistic capture for test verification
	}

	// Patch log.Printf temporarily
	oldLogPrintf := logPrintf
	logPrintf = logFunc
	defer func() { logPrintf = oldLogPrintf }()

	hr := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))

	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("X-Request-Id", "test-id")

	w := httptest.NewRecorder()
	hr.ServeHTTP(w, r)

	if logged == "" {
		t.Error("Expected log output, got empty string")
	}
}

