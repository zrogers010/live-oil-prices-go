package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"live-oil-prices-go/internal/models"
)

type fakeMarketDataService struct {
	getPricesFunc     func() []models.Price
	getChartDataFunc  func(symbol string, days int, interval string) models.ChartData
	getPredictionsFunc func() []models.Prediction
	getAnalysisFunc    func() models.MarketAnalysis
}

func (f *fakeMarketDataService) GetPrices() []models.Price {
	return f.getPricesFunc()
}

func (f *fakeMarketDataService) GetChartData(symbol string, days int, interval string) models.ChartData {
	return f.getChartDataFunc(symbol, days, interval)
}

func (f *fakeMarketDataService) GetPredictions() []models.Prediction {
	if f.getPredictionsFunc == nil {
		return nil
	}
	return f.getPredictionsFunc()
}

func (f *fakeMarketDataService) GetAnalysis() models.MarketAnalysis {
	if f.getAnalysisFunc == nil {
		return models.MarketAnalysis{}
	}
	return f.getAnalysisFunc()
}

func (f *fakeMarketDataService) GetHeroChart(symbol string, maxLiveBars int) models.HeroChart {
	return models.HeroChart{Symbol: symbol, Mode: "warming-up", Bars: []models.PythCandle{}}
}

type fakeNewsFeedService struct {
	getNewsFunc     func() []models.NewsArticle
	getNewsByIDFunc func(id string) *models.NewsArticle
}

func (f *fakeNewsFeedService) GetNews() []models.NewsArticle {
	if f.getNewsFunc == nil {
		return nil
	}
	return f.getNewsFunc()
}

func (f *fakeNewsFeedService) GetNewsByID(id string) *models.NewsArticle {
	if f.getNewsByIDFunc == nil {
		return nil
	}
	return f.getNewsByIDFunc(id)
}

func TestNewServerHandlerWiresRoutesAndMiddleware(t *testing.T) {
	server := newServerHandler(
		&fakeMarketDataService{
			getPricesFunc: func() []models.Price {
				return []models.Price{
					{Symbol: "WTI", Name: "WTI Crude Oil", Price: 72.5, Change: 0.12, ChangePct: 0.2, High: 73.0, Low: 71.0, Volume: 12345, UpdatedAt: "2026-03-09T00:00:00Z"},
				}
			},
			getChartDataFunc: func(symbol string, days int, interval string) models.ChartData {
				return models.ChartData{
					Symbol:   symbol,
					Name:     symbol + " Crude Oil",
					Interval: interval,
					Data: []models.OHLCV{
						{Time: 1, Open: 70, High: 72, Low: 69, Close: 71, Volume: 1000},
						{Time: 2, Open: 71, High: 73, Low: 70, Close: 72, Volume: 1200},
					},
				}
			},
			getAnalysisFunc: func() models.MarketAnalysis {
				return models.MarketAnalysis{
					Sentiment:  "neutral",
					Score:      60,
					Summary:    "Test",
					KeyPoints:  []string{"point"},
					Technical:  models.TechnicalSignals{RSI: 45.0},
					UpdatedAt:  "2026-03-09T00:00:00Z",
				}
			},
		},
		&fakeNewsFeedService{
			getNewsFunc: func() []models.NewsArticle {
				return []models.NewsArticle{
					{ID: "a", Slug: "slug-a", Title: "First News", Category: "Oil Markets", PublishedAt: "2026-03-09T00:00:00Z"},
				}
			},
			getNewsByIDFunc: func(id string) *models.NewsArticle {
				if id == "a" {
					return &models.NewsArticle{ID: "a", Slug: "slug-a", Title: "First News"}
				}
				return nil
			},
		},
	)

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{"health", http.MethodGet, "/api/health", http.StatusOK},
		{"prices", http.MethodGet, "/api/prices", http.StatusOK},
		{"analysis", http.MethodGet, "/api/analysis", http.StatusOK},
		{"predictions", http.MethodGet, "/api/predictions", http.StatusOK},
		{"news", http.MethodGet, "/api/news", http.StatusOK},
		{"chart", http.MethodGet, "/api/charts/WTI?days=7&interval=2h", http.StatusOK},
		{"newsArticleFound", http.MethodGet, "/api/news/a", http.StatusOK},
		{"newsArticleMissing", http.MethodGet, "/api/news/nope", http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, nil)
			res := httptest.NewRecorder()
			server.ServeHTTP(res, req)
			if res.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d for %s", tc.wantStatus, res.Code, tc.target)
			}
			if got := res.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("expected CORS header on %s got %q", tc.target, got)
			}
			if got := res.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("expected JSON response for %s, got %q", tc.target, got)
			}
			if tc.target == "/api/news/nope" {
				if !strings.Contains(res.Body.String(), "not found") {
					t.Fatalf("expected not-found payload, got %s", res.Body.String())
				}
			}
		})
	}

	priceReq := httptest.NewRequest(http.MethodGet, "/api/prices", nil)
	priceRes := httptest.NewRecorder()
	server.ServeHTTP(priceRes, priceReq)
	var prices []models.Price
	if err := json.Unmarshal(priceRes.Body.Bytes(), &prices); err != nil {
		t.Fatalf("invalid prices response: %v", err)
	}
	if len(prices) != 1 || prices[0].Symbol != "WTI" {
		t.Fatalf("unexpected prices payload: %#v", prices)
	}
}
