package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"live-oil-prices-go/internal/models"
)

func TestGetPricesHandler(t *testing.T) {
	// Create a fake MarketDataClient
	mockClient := &mockMarketDataClient{}
	handler := NewAPI(mockClient, nil)

	req := httptest.NewRequest("GET", "/api/prices", nil)
	w := httptest.NewRecorder()
	handler.GetPrices(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

func TestGetChartDataHandler(t *testing.T) {
	mockClient := &mockMarketDataClient{}
	handler := NewAPI(mockClient, nil)

	req := httptest.NewRequest("GET", "/api/charts/WTI?days=10", nil)
	w := httptest.NewRecorder()
	handler.GetChartData(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

// Mock MarketDataClient implementation

type mockMarketDataClient struct{}

func (m *mockMarketDataClient) GetPrices() []models.Price {
	return []models.Price{
		{Symbol: "WTI", Name: "WTI Crude Oil", Price: 70, UpdatedAt: "2026-03-10T00:00:00Z"},
	}
}

func (m *mockMarketDataClient) GetChartData(symbol string, days int, interval string) models.ChartData {
	return models.ChartData{
		Symbol: symbol,
		Name: "WTI Crude Oil",
		Interval: "1d",
		Data: []models.OHLCV{},
	}
}

func (m *mockMarketDataClient) GetPredictions() []models.Prediction {
	return nil
}

func (m *mockMarketDataClient) GetAnalysis() models.MarketAnalysis {
	return models.MarketAnalysis{}
}
