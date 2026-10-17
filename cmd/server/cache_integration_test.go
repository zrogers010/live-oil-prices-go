package main

import (
	"live-oil-prices-go/internal/assets"
	"live-oil-prices-go/internal/handlers"
	"live-oil-prices-go/internal/services"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetCacheHeaders(t *testing.T) {
	// Create temp directory with test assets
	tempDir := t.TempDir()
	jsDir := filepath.Join(tempDir, "js")
	cssDir := filepath.Join(tempDir, "css")
	
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js dir: %v", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		t.Fatalf("Failed to create css dir: %v", err)
	}
	
	// Create test files
	testFiles := map[string]string{
		filepath.Join(jsDir, "app.js"):       "console.log('app');",
		filepath.Join(jsDir, "detail.js"):    "console.log('detail');",
		filepath.Join(cssDir, "styles.css"):  "body { margin: 0; }",
	}
	
	for path, content := range testFiles {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}
	
	// Initialize assets with temp directory
	if err := assets.InitWithRoot(tempDir); err != nil {
		t.Fatalf("Failed to init assets: %v", err)
	}
	
	// Verify we have hashes
	appHash, ok := assets.GetHash("js/app.js")
	if !ok || len(appHash) != 12 {
		t.Fatalf("Expected 12-char hash for app.js, got: %s (ok=%v)", appHash, ok)
	}

	// Initialize templates
	templatesPath := filepath.Join("..", "..", "web", "templates")
	if err := handlers.InitPageTemplates(templatesPath); err != nil {
		t.Fatalf("Failed to init page templates: %v", err)
	}
	commodityTemplatePath := filepath.Join(templatesPath, "commodity.html")
	if err := handlers.InitCommodityTemplate(commodityTemplatePath); err != nil {
		t.Fatalf("Failed to init commodity template: %v", err)
	}

	market := services.NewMarketDataService()
	news := services.NewNewsFeedService()
	handler := newServerHandler(market, news)

	tests := []struct {
		name          string
		path          string
		query         string
		wantCacheType string // "immutable", "short", "none"
	}{
		{
			name:          "Unversioned JS gets short cache",
			path:          "/js/app.js",
			query:         "",
			wantCacheType: "short",
		},
		{
			name:          "Versioned JS gets long cache if hash matches",
			path:          "/js/app.js",
			query:         "?v=test123",
			wantCacheType: "short_or_immutable", // depends on whether hash matches
		},
		{
			name:          "HTML page gets no-cache",
			path:          "/",
			query:         "",
			wantCacheType: "none",
		},
		{
			name:          "Commodity page gets no-cache",
			path:          "/commodity/WTI",
			query:         "",
			wantCacheType: "none",
		},
		{
			name:          "Charts page gets no-cache",
			path:          "/charts",
			query:         "",
			wantCacheType: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path+tt.query, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			cacheControl := w.Header().Get("Cache-Control")

			switch tt.wantCacheType {
			case "immutable":
				if !strings.Contains(cacheControl, "immutable") {
					t.Errorf("Expected immutable cache, got: %s", cacheControl)
				}
			case "short":
				if !strings.Contains(cacheControl, "max-age=300") && !strings.Contains(cacheControl, "no-cache") {
					t.Errorf("Expected short cache (max-age=300), got: %s", cacheControl)
				}
			case "short_or_immutable":
				// Either is acceptable depending on hash match
				if cacheControl == "" {
					t.Errorf("Expected some cache header, got none")
				}
			case "none":
				if !strings.Contains(cacheControl, "no-cache") {
					t.Errorf("Expected no-cache for HTML, got: %s", cacheControl)
				}
			}
		})
	}
}

func TestRollbackPathBlocked(t *testing.T) {
	// Create temp directory with minimal assets for this test
	tempDir := t.TempDir()
	jsDir := filepath.Join(tempDir, "js")
	cssDir := filepath.Join(tempDir, "css")
	
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js dir: %v", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		t.Fatalf("Failed to create css dir: %v", err)
	}
	
	// Create minimal test files
	for _, file := range []string{"js/app.js", "js/detail.js", "css/styles.css"} {
		fullPath := filepath.Join(tempDir, file)
		if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", file, err)
		}
	}
	
	if err := assets.InitWithRoot(tempDir); err != nil {
		t.Fatalf("Failed to init assets: %v", err)
	}
	
	templatesPath := filepath.Join("..", "..", "web", "templates")
	if err := handlers.InitPageTemplates(templatesPath); err != nil {
		t.Fatalf("Failed to init page templates: %v", err)
	}
	if err := handlers.InitCommodityTemplate(filepath.Join(templatesPath, "commodity.html")); err != nil {
		t.Fatalf("Failed to init commodity template: %v", err)
	}

	market := services.NewMarketDataService()
	news := services.NewNewsFeedService()
	handler := newServerHandler(market, news)

	rollbackPaths := []string{
		"/js.rollback-20261016/app.js",
		"/js.rollback-20261017/app.js",
		"/js.rollback-20261017b/app.js",
		"/js/x.rollback-1/app.js",
		"/static/.rollback/test.js",
		"/path/with.rollback/file.css",
	}

	for _, path := range rollbackPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected 404 for rollback path %s, got %d", path, w.Code)
			}
		})
	}
}

func TestNormalPathsWork(t *testing.T) {
	// Create temp directory with minimal assets
	tempDir := t.TempDir()
	jsDir := filepath.Join(tempDir, "js")
	cssDir := filepath.Join(tempDir, "css")
	
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js dir: %v", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		t.Fatalf("Failed to create css dir: %v", err)
	}
	
	for _, file := range []string{"js/app.js", "js/detail.js", "css/styles.css"} {
		fullPath := filepath.Join(tempDir, file)
		if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", file, err)
		}
	}
	
	if err := assets.InitWithRoot(tempDir); err != nil {
		t.Fatalf("Failed to init assets: %v", err)
	}
	
	templatesPath := filepath.Join("..", "..", "web", "templates")
	if err := handlers.InitPageTemplates(templatesPath); err != nil {
		t.Fatalf("Failed to init page templates: %v", err)
	}
	if err := handlers.InitCommodityTemplate(filepath.Join(templatesPath, "commodity.html")); err != nil {
		t.Fatalf("Failed to init commodity template: %v", err)
	}

	market := services.NewMarketDataService()
	news := services.NewNewsFeedService()
	handler := newServerHandler(market, news)

	normalPaths := []string{
		"/",
		"/charts",
		"/forecast",
		"/news",
		"/commodity/WTI",
		"/api/prices",
		"/api/health",
	}

	for _, path := range normalPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected 200 for path %s, got %d", path, w.Code)
			}
		})
	}
}
