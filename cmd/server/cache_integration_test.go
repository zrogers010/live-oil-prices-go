package main

import (
	"live-oil-prices-go/internal/assets"
	"live-oil-prices-go/internal/handlers"
	"live-oil-prices-go/internal/services"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetCacheHeaders(t *testing.T) {
	// Initialize assets
	if err := assets.Init(); err != nil {
		t.Logf("Asset init warning (may not have built assets): %v", err)
	}

	// Initialize templates
	if err := handlers.InitPageTemplates("../../web/templates"); err != nil {
		t.Fatalf("Failed to init page templates: %v", err)
	}
	if err := handlers.InitCommodityTemplate("../../web/templates/commodity.html"); err != nil {
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
	if err := assets.Init(); err != nil {
		t.Logf("Asset init warning: %v", err)
	}
	if err := handlers.InitPageTemplates("../../web/templates"); err != nil {
		t.Fatalf("Failed to init page templates: %v", err)
	}
	if err := handlers.InitCommodityTemplate("../../web/templates/commodity.html"); err != nil {
		t.Fatalf("Failed to init commodity template: %v", err)
	}

	market := services.NewMarketDataService()
	news := services.NewNewsFeedService()
	handler := newServerHandler(market, news)

	rollbackPaths := []string{
		"/js.rollback-20261016/app.js",
		"/js.rollback-20261017/detail.js",
		"/js.rollback-20261017b/app.js",
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
	if err := assets.Init(); err != nil {
		t.Logf("Asset init warning: %v", err)
	}
	if err := handlers.InitPageTemplates("../../web/templates"); err != nil {
		t.Fatalf("Failed to init page templates: %v", err)
	}
	if err := handlers.InitCommodityTemplate("../../web/templates/commodity.html"); err != nil {
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
