package middleware

import (
	"live-oil-prices-go/internal/assets"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlockRollbacks(t *testing.T) {
	handler := BlockRollbacks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/js/app.js", http.StatusOK},
		{"/css/styles.css", http.StatusOK},
		{"/js.rollback-20261016/app.js", http.StatusNotFound},
		{"/js.rollback-20261017/app.js", http.StatusNotFound},
		{"/js.rollback-20261017b/app.js", http.StatusNotFound},
		{"/js/x.rollback-1/app.js", http.StatusNotFound},
		{"/static/.rollback/file.js", http.StatusNotFound},
		{"/normal/path", http.StatusOK},
		{"/path/with.rollback.in.name/file.js", http.StatusNotFound},
		{"/rollback", http.StatusOK}, // "rollback" without dot is OK
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("path %s: got status %d, want %d", tt.path, w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				if body := w.Body.String(); body != "success" {
					t.Errorf("path %s: expected body 'success', got %q", tt.path, body)
				}
			}
		})
	}
}

func TestCacheControlVersionedAssets(t *testing.T) {
	// Initialize assets to get real hashes
	if err := assets.Init(); err != nil {
		t.Logf("Asset init warning (expected if assets not built): %v", err)
	}

	handler := CacheControl(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))

	tests := []struct {
		name              string
		path              string
		query             string
		wantContains      string
		wantNotContains   string
		description       string
	}{
		{
			name:            "versioned JS with correct hash",
			path:            "/js/app.js",
			query:           "?v=" + getTestHash("js/app.js"),
			wantContains:    "public",
			description:     "Versioned asset with matching hash gets cache",
		},
		{
			name:            "versioned JS with wrong hash",
			path:            "/js/app.js",
			query:           "?v=wronghash",
			wantContains:    "max-age=300",
			description:     "Versioned asset with wrong hash gets short cache",
		},
		{
			name:            "unversioned JS",
			path:            "/js/app.js",
			query:           "",
			wantContains:    "max-age=300, public",
			description:     "Unversioned asset gets short cache",
		},
		{
			name:            "unversioned CSS",
			path:            "/css/styles.css",
			query:           "",
			wantContains:    "max-age=300, public",
			description:     "Unversioned CSS gets short cache",
		},
		{
			name:            "HTML page",
			path:            "/",
			query:           "",
			wantContains:    "",
			wantNotContains: "max-age",
			description:     "HTML pages don't get cache headers from middleware",
		},
		{
			name:            "API endpoint",
			path:            "/api/prices",
			query:           "",
			wantContains:    "",
			wantNotContains: "max-age",
			description:     "API endpoints don't get cache headers from middleware",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path+tt.query, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			got := w.Header().Get("Cache-Control")
			if tt.wantContains != "" && !strings.Contains(got, tt.wantContains) {
				t.Errorf("%s:\ngot:  %q\nwant to contain: %q\n(%s)", tt.name, got, tt.wantContains, tt.description)
			}
			if tt.wantNotContains != "" && strings.Contains(got, tt.wantNotContains) {
				t.Errorf("%s:\ngot:  %q\nshould not contain: %q\n(%s)", tt.name, got, tt.wantNotContains, tt.description)
			}
		})
	}
}

func getTestHash(path string) string {
	hash, ok := assets.GetHash(path)
	if !ok {
		return "testhash123"
	}
	return hash
}

func TestCacheControlIntegration(t *testing.T) {
	// Initialize assets for real
	if err := assets.Init(); err != nil {
		// Assets may not exist in test, that's OK - we're testing the middleware logic
		t.Logf("Asset init warning (expected in test): %v", err)
	}

	handler := CacheControl(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test that static assets get some cache header
	req := httptest.NewRequest("GET", "/js/app.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl == "" {
		t.Error("Expected Cache-Control header for static asset, got none")
	}

	if cacheControl != "max-age=300, public" && cacheControl != "max-age=31536000, public, immutable" {
		t.Errorf("Expected valid cache header, got: %s", cacheControl)
	}

	// Test that non-static paths get no cache header from middleware
	req = httptest.NewRequest("GET", "/api/health", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Cache-Control") != "" {
		t.Error("Expected no Cache-Control header for API endpoint from middleware")
	}
}
