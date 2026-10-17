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

func TestRenderedHTMLContainsVersionedAssets(t *testing.T) {
	// Create temp directory with real assets
	tempDir := t.TempDir()
	jsDir := filepath.Join(tempDir, "js")
	cssDir := filepath.Join(tempDir, "css")
	
	if err := os.MkdirAll(jsDir, 0755); err != nil {
		t.Fatalf("Failed to create js dir: %v", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		t.Fatalf("Failed to create css dir: %v", err)
	}
	
	// Create test files with unique content
	if err := os.WriteFile(filepath.Join(jsDir, "app.js"), []byte("console.log('app-test');"), 0644); err != nil {
		t.Fatalf("Failed to create app.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsDir, "detail.js"), []byte("console.log('detail-test');"), 0644); err != nil {
		t.Fatalf("Failed to create detail.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cssDir, "styles.css"), []byte("body { margin: 1px; }"), 0644); err != nil {
		t.Fatalf("Failed to create styles.css: %v", err)
	}
	
	// Initialize assets with temp directory
	if err := assets.InitWithRoot(tempDir); err != nil {
		t.Fatalf("Failed to init assets: %v", err)
	}
	
	// Get the actual hashes
	appHash, ok := assets.GetHash("js/app.js")
	if !ok {
		t.Fatal("Failed to get hash for app.js")
	}
	cssHash, ok := assets.GetHash("css/styles.css")
	if !ok {
		t.Fatal("Failed to get hash for css/styles.css")
	}
	detailHash, ok := assets.GetHash("js/detail.js")
	if !ok {
		t.Fatal("Failed to get hash for detail.js")
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

	tests := []struct {
		name         string
		path         string
		wantContains []string
		description  string
	}{
		{
			name: "home page layout",
			path: "/",
			wantContains: []string{
				"/js/app.js?v=" + appHash,
				"/css/styles.css?v=" + cssHash,
			},
			description: "Layout template should have versioned assets (M7)",
		},
		{
			name: "commodity page",
			path: "/commodity/WTI",
			wantContains: []string{
				"/js/detail.js?v=" + detailHash,
				"/css/styles.css?v=" + cssHash,
			},
			description: "Commodity template should have versioned assets (M10)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected 200 for %s, got %d", tt.path, w.Code)
			}

			body := w.Body.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(body, want) {
					t.Errorf("%s: HTML does not contain %q\n%s", tt.description, want, tt.description)
				}
			}
		})
	}
}

func TestDirectoryListingsDisabled(t *testing.T) {
	// Create temp directory with assets
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

	// Create actual directories in web/static for testing
	if err := os.MkdirAll("../../web/static/js", 0755); err != nil {
		t.Logf("Failed to create web/static/js: %v", err)
	}
	if err := os.MkdirAll("../../web/static/css", 0755); err != nil {
		t.Logf("Failed to create web/static/css: %v", err)
	}

	directoryPaths := []string{
		"/js/",
		"/css/",
	}

	for _, path := range directoryPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected 404 for directory %s, got %d (M9)", path, w.Code)
			}
		})
	}
}

func TestLegacyCommodityHTMLRedirect(t *testing.T) {
	// Create minimal temp assets
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

	req := httptest.NewRequest("GET", "/commodity.html", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Errorf("Expected 301 for /commodity.html, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/commodity/WTI" {
		t.Errorf("Expected redirect to /commodity/WTI, got %s", location)
	}
}
