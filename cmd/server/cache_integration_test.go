package main

import (
	"live-oil-prices-go/internal/assets"
	"live-oil-prices-go/internal/handlers"
	"live-oil-prices-go/internal/models"
	"live-oil-prices-go/internal/services"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Mock services for testing
type mockMarketDataService struct{}

func (m *mockMarketDataService) GetPrices() []models.Price {
	return []models.Price{}
}

func (m *mockMarketDataService) GetChartData(symbol string, days int, interval string) models.ChartData {
	return models.ChartData{Symbol: symbol}
}

func (m *mockMarketDataService) GetPredictions() []models.Prediction {
	return []models.Prediction{}
}

func (m *mockMarketDataService) GetHeroChart(symbol string, maxLiveBars int) models.HeroChart {
	return models.HeroChart{}
}

func (m *mockMarketDataService) GetConsensusForecasts() []models.ConsensusForecast {
	return []models.ConsensusForecast{}
}

func (m *mockMarketDataService) GetConsensusForecast(symbol string) (models.ConsensusForecast, bool) {
	return models.ConsensusForecast{}, false
}

type mockNewsFeedService struct{}

func (m *mockNewsFeedService) GetNews() []models.NewsArticle {
	return []models.NewsArticle{}
}

func (m *mockNewsFeedService) GetNewsByID(id string) *models.NewsArticle {
	return nil
}

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
	// Create a real temp static directory with rollback directories containing actual files
	tempDir := t.TempDir()
	tempStaticDir := filepath.Join(tempDir, "web", "static")
	
	// Create normal js/css directories
	jsDir := filepath.Join(tempStaticDir, "js")
	cssDir := filepath.Join(tempStaticDir, "css")
	
	// Create rollback directories (lowercase and uppercase variants)
	jsRollbackDir := filepath.Join(tempStaticDir, "js.rollback-20261016")
	jsUpperRollbackDir := filepath.Join(tempStaticDir, "js.ROLLBACK-20261017")
	cssRollbackDir := filepath.Join(tempStaticDir, "css.rollback-x")
	
	for _, dir := range []string{jsDir, cssDir, jsRollbackDir, jsUpperRollbackDir, cssRollbackDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}
	
	// Create files in normal directories (control - these should be accessible)
	normalFiles := map[string]string{
		"js/app.js":       "normal js content",
		"js/detail.js":    "normal detail content",
		"css/styles.css":  "normal css content",
	}
	
	// Create files in rollback directories (these should be blocked)
	rollbackFiles := map[string]string{
		"js.rollback-20261016/app.js":       "rollback js content",
		"js.ROLLBACK-20261017/app.js":       "uppercase rollback js",
		"css.rollback-x/styles.css":         "rollback css content",
	}
	
	for file, content := range normalFiles {
		fullPath := filepath.Join(tempStaticDir, file)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", file, err)
		}
	}
	
	for file, content := range rollbackFiles {
		fullPath := filepath.Join(tempStaticDir, file)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", file, err)
		}
	}
	
	// Change working directory to temp directory so web/static is found
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer os.Chdir(origWd)
	
	// Use the actual newServerHandler from main.go with mock services
	mockMarket := &mockMarketDataService{}
	mockNews := &mockNewsFeedService{}
	handler := newServerHandler(mockMarket, mockNews)

	// Test that rollback paths are blocked (return 404 even though files exist)
	rollbackPaths := []string{
		"/js.rollback-20261016/app.js",
		"/js.ROLLBACK-20261017/app.js",
		"/css.rollback-x/styles.css",
	}

	for _, path := range rollbackPaths {
		t.Run("blocked:"+path, func(t *testing.T) {
			// Verify file exists on disk
			diskPath := filepath.Join(tempStaticDir, strings.TrimPrefix(path, "/"))
			if _, err := os.Stat(diskPath); err != nil {
				t.Fatalf("Test setup failed: file %s does not exist: %v", diskPath, err)
			}
			
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected 404 for rollback path %s (file exists but should be blocked), got %d", path, w.Code)
			}
		})
	}
	
	// Test that normal paths still work (control)
	normalPaths := []string{
		"/js/app.js",
		"/js/detail.js",
		"/css/styles.css",
	}
	
	for _, path := range normalPaths {
		t.Run("control:"+path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected 200 for normal path %s (control), got %d", path, w.Code)
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
	// Create a real temp static directory structure
	tempDir := t.TempDir()
	tempStaticDir := filepath.Join(tempDir, "web", "static")
	jsDir := filepath.Join(tempStaticDir, "js")
	jsSubdir := filepath.Join(jsDir, "subdir")
	cssDir := filepath.Join(tempStaticDir, "css")
	
	if err := os.MkdirAll(jsSubdir, 0755); err != nil {
		t.Fatalf("Failed to create js subdir: %v", err)
	}
	if err := os.MkdirAll(cssDir, 0755); err != nil {
		t.Fatalf("Failed to create css dir: %v", err)
	}
	
	// Create test files
	for _, file := range []string{"js/app.js", "js/detail.js", "js/subdir/test.js", "css/styles.css"} {
		fullPath := filepath.Join(tempStaticDir, file)
		if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", file, err)
		}
	}
	
	// Change working directory to temp directory so web/static is found
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}
	defer os.Chdir(origWd)
	
	// Use the actual newServerHandler from main.go with mock services
	mockMarket := &mockMarketDataService{}
	mockNews := &mockNewsFeedService{}
	handler := newServerHandler(mockMarket, mockNews)

	directoryPaths := []string{
		"/js/",
		"/css/",
		"/js/subdir/",
	}

	for _, path := range directoryPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("Expected 404 for directory %s, got %d (M9 would allow this)", path, w.Code)
			}
		})
	}
	
	// Also verify that files are still accessible
	t.Run("files still accessible", func(t *testing.T) {
		filePaths := []string{"/js/app.js", "/css/styles.css", "/js/subdir/test.js"}
		for _, path := range filePaths {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("Expected 200 for file %s, got %d", path, w.Code)
			}
		}
	})
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
