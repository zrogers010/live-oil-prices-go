package handlers

import (
	"live-oil-prices-go/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockMarketService returns all unavailable prices
type mockMarketService struct{}

func (m *mockMarketService) GetPrices() []models.Price {
	return []models.Price{
		{Symbol: "WTI", Name: "WTI Crude Oil", Available: false},
		{Symbol: "BRENT", Name: "Brent Crude", Available: false},
		{Symbol: "NATGAS", Name: "Natural Gas", Available: false},
		{Symbol: "HEATING", Name: "Heating Oil", Available: false},
		{Symbol: "RBOB", Name: "RBOB Gasoline", Available: false},
		{Symbol: "OPEC", Name: "OPEC Basket", Available: false},
	}
}

func (m *mockMarketService) GetChartData(symbol string, days int, interval string) models.ChartData {
	return models.ChartData{
		Symbol:   symbol,
		Name:     symbol,
		Data:     []models.OHLCV{},
		Interval: "",
	}
}

func (m *mockMarketService) GetPredictions() []models.Prediction {
	return []models.Prediction{}
}

func (m *mockMarketService) GetAnalysis() models.MarketAnalysis {
	return models.MarketAnalysis{}
}

func (m *mockMarketService) GetHeroChart(symbol string, maxLiveBars int) models.HeroChart {
	return models.HeroChart{}
}

func (m *mockMarketService) GetConsensusForecasts() []models.ConsensusForecast {
	return []models.ConsensusForecast{}
}

func (m *mockMarketService) GetConsensusForecast(symbol string) (models.ConsensusForecast, bool) {
	return models.ConsensusForecast{}, false
}

// mockNewsService returns empty news
type mockNewsService struct{}

func (m *mockNewsService) GetNews() []models.NewsArticle {
	return []models.NewsArticle{}
}

func (m *mockNewsService) GetNewsByID(id string) *models.NewsArticle {
	return nil
}

// TestOfflineHomePageNoZeroPrices verifies that with all prices unavailable,
// the home page renders no $0.00 or 0.00% artifacts.
func TestOfflineHomePageNoZeroPrices(t *testing.T) {
	api := NewAPI(&mockMarketService{}, &mockNewsService{})

	if err := InitPageTemplates("../../web/templates"); err != nil {
		t.Fatalf("failed to init templates: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	api.ServeHome(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Debug: print first occurrence
	if idx := strings.Index(body, "$0.00"); idx >= 0 {
		start := idx - 50
		if start < 0 {
			start = 0
		}
		end := idx + 100
		if end > len(body) {
			end = len(body)
		}
		t.Logf("Found $0.00 at position %d: %s", idx, body[start:end])
	}
	if idx := strings.Index(body, "0.00%"); idx >= 0 {
		start := idx - 50
		if start < 0 {
			start = 0
		}
		end := idx + 100
		if end > len(body) {
			end = len(body)
		}
		t.Logf("Found 0.00%% at position %d: %s", idx, body[start:end])
	}

	// Check for $0.00
	if strings.Contains(body, "$0.00") {
		t.Errorf("home page contains $0.00 with unavailable prices")
	}

	// Check for 0.00%
	if strings.Contains(body, "0.00%") {
		t.Errorf("home page contains 0.00%% with unavailable prices")
	}

	// Should have dashes for unavailable prices
	if !strings.Contains(body, "—") {
		t.Errorf("home page missing dash placeholders for unavailable prices")
	}
}

// TestOfflineCommodityPageNoZeroPrices verifies that with unavailable price,
// the commodity page renders no $0.00 or 0.00% and no price in title/JSON-LD.
func TestOfflineCommodityPageNoZeroPrices(t *testing.T) {
	api := NewAPI(&mockMarketService{}, &mockNewsService{})

	if err := InitCommodityTemplate("../../web/templates/commodity.html"); err != nil {
		t.Fatalf("failed to init commodity template: %v", err)
	}

	req := httptest.NewRequest("GET", "/commodity/WTI", nil)
	w := httptest.NewRecorder()

	api.ServeCommodityPage(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body := w.Body.String()

	// Check for $0.00
	if strings.Contains(body, "$0.00") {
		t.Errorf("commodity page contains $0.00 with unavailable price")
	}

	// Check for 0.00%
	if strings.Contains(body, "0.00%") {
		t.Errorf("commodity page contains 0.00%% with unavailable price")
	}

	// Must end with </html>
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("commodity page does not end with </html>")
	}
}

// TestOfflineChartDataUnavailable verifies the hero shows 'Chart data unavailable'
// when there's no price data, not 'Loading chart data'.
func TestOfflineChartDataUnavailable(t *testing.T) {
	api := NewAPI(&mockMarketService{}, &mockNewsService{})

	if err := InitPageTemplates("../../web/templates"); err != nil{
		t.Fatalf("failed to init templates: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	api.ServeHome(w, req)

	body := w.Body.String()

	// Should say 'Chart data unavailable', not 'Loading chart data'
	if strings.Contains(body, "Loading chart data") {
		t.Errorf("hero shows 'Loading chart data' instead of 'Chart data unavailable'")
	}

	if !strings.Contains(body, "Chart data unavailable") {
		t.Errorf("hero missing 'Chart data unavailable' message")
	}
}
