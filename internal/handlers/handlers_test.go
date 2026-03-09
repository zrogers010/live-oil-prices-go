package handlers

import (
	"encoding/json"
	"live-oil-prices-go/internal/models"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeMarketDataService struct {
	getPricesFunc     func() []models.Price
	getChartDataFunc  func(symbol string, days int, interval string) models.ChartData
	getPredictionsFunc func() []models.Prediction
	getAnalysisFunc    func() models.MarketAnalysis
}

func (f *fakeMarketDataService) GetPrices() []models.Price {
	if f.getPricesFunc == nil {
		return nil
	}
	return f.getPricesFunc()
}

func (f *fakeMarketDataService) GetChartData(symbol string, days int, interval string) models.ChartData {
	if f.getChartDataFunc == nil {
		return models.ChartData{}
	}
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

func setupMux(api *API) *http.ServeMux {
	mux := http.NewServeMux()
	api.RegisterRoutes(mux)
	return mux
}

func TestHealthCheck(t *testing.T) {
	api := NewAPI(&fakeMarketDataService{}, &fakeNewsFeedService{})
	mux := setupMux(api)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestRegisterRoutesPriceAndAnalysis(t *testing.T) {
	api := NewAPI(
		&fakeMarketDataService{
			getPricesFunc: func() []models.Price {
				return []models.Price{{Symbol: "WTI", Name: "WTI Crude Oil", Price: 72.1, Change: 1.1, ChangePct: 1.5, High: 73, Low: 70, Volume: 1000000, UpdatedAt: "2024-01-01T00:00:00Z"}}
			},
			getPredictionsFunc: func() []models.Prediction {
				return []models.Prediction{{Symbol: "WTI", Name: "WTI Crude Oil", Current: 72.1, Predicted: 73.0, Timeframe: "7 days", Confidence: 0.8, Direction: "bullish", Analysis: "Test"}}
			},
			getAnalysisFunc: func() models.MarketAnalysis {
				return models.MarketAnalysis{Sentiment: "bullish", Score: 70, Summary: "Stable", KeyPoints: []string{"A"}, UpdatedAt: "2024-01-01T00:00:00Z"}
			},
		},
		&fakeNewsFeedService{
			getNewsFunc: func() []models.NewsArticle {
				return []models.NewsArticle{{ID: "a", Slug: "slug", Title: "News", Source: "Reuters"}}
			},
			getNewsByIDFunc: func(id string) *models.NewsArticle {
				if id == "a" {
					return &models.NewsArticle{ID: "a", Title: "News", Source: "Reuters"}
				}
				return nil
			},
		},
	)

	mux := setupMux(api)

	pricesReq := httptest.NewRequest(http.MethodGet, "/api/prices", nil)
	pricesRes := httptest.NewRecorder()
	mux.ServeHTTP(pricesRes, pricesReq)
	if pricesRes.Code != http.StatusOK {
		t.Fatalf("expected prices 200, got %d", pricesRes.Code)
	}
	var prices []models.Price
	if err := json.Unmarshal(pricesRes.Body.Bytes(), &prices); err != nil {
		t.Fatalf("invalid prices response: %v", err)
	}
	if len(prices) != 1 || prices[0].Symbol != "WTI" {
		t.Fatalf("unexpected prices payload: %v", prices)
	}

	predReq := httptest.NewRequest(http.MethodGet, "/api/predictions", nil)
	predRes := httptest.NewRecorder()
	mux.ServeHTTP(predRes, predReq)
	var predictions []models.Prediction
	if err := json.Unmarshal(predRes.Body.Bytes(), &predictions); err != nil {
		t.Fatalf("invalid predictions response: %v", err)
	}
	if len(predictions) != 1 {
		t.Fatalf("expected one prediction, got %d", len(predictions))
	}

	analysisReq := httptest.NewRequest(http.MethodGet, "/api/analysis", nil)
	analysisRes := httptest.NewRecorder()
	mux.ServeHTTP(analysisRes, analysisReq)
	var analysis models.MarketAnalysis
	if err := json.Unmarshal(analysisRes.Body.Bytes(), &analysis); err != nil {
		t.Fatalf("invalid analysis response: %v", err)
	}
	if analysis.Score != 70 {
		t.Fatalf("unexpected analysis score: %v", analysis.Score)
	}
}

func TestChartEndpointRespectsInputAndDefaults(t *testing.T) {
	var gotSymbol string
	var gotDays int
	var gotInterval string
	api := NewAPI(
		&fakeMarketDataService{
			getChartDataFunc: func(symbol string, days int, interval string) models.ChartData {
				gotSymbol = symbol
				gotDays = days
				gotInterval = interval
				return models.ChartData{Symbol: symbol, Name: symbol, Interval: interval, Data: []models.OHLCV{{Time: 1, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 1}}}
			},
		},
		&fakeNewsFeedService{},
	)
	mux := setupMux(api)

	req := httptest.NewRequest(http.MethodGet, "/api/charts/WTI?days=9999", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if gotSymbol != "WTI" {
		t.Fatalf("expected symbol WTI, got %q", gotSymbol)
	}
	if gotDays != 90 {
		t.Fatalf("expected invalid days to default to 90, got %d", gotDays)
	}
	if gotInterval != "" {
		t.Fatalf("expected interval to pass through when omitted: %q", gotInterval)
	}
}

func TestChartEndpointAcceptsExplicitInterval(t *testing.T) {
	var gotInterval string
	api := NewAPI(
		&fakeMarketDataService{
			getChartDataFunc: func(symbol string, days int, interval string) models.ChartData {
				gotInterval = interval
				return models.ChartData{Symbol: symbol, Name: symbol, Interval: interval}
			},
		},
		&fakeNewsFeedService{},
	)
	mux := setupMux(api)

	req := httptest.NewRequest(http.MethodGet, "/api/charts/WTI?days=3&interval=4h", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if gotInterval != "4h" {
		t.Fatalf("expected interval 4h, got %q", gotInterval)
	}
}

func TestNewsArticleNotFound(t *testing.T) {
	api := NewAPI(
		&fakeMarketDataService{},
		&fakeNewsFeedService{
			getNewsByIDFunc: func(id string) *models.NewsArticle { return nil },
		},
	)
	mux := setupMux(api)

	req := httptest.NewRequest(http.MethodGet, "/api/news/does-not-exist", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestNewsArticleFound(t *testing.T) {
	api := NewAPI(
		&fakeMarketDataService{},
		&fakeNewsFeedService{
			getNewsByIDFunc: func(id string) *models.NewsArticle {
				if id == "a" {
					return &models.NewsArticle{
						ID:    "a",
						Title: "Title A",
						Source: "Reuters",
					}
				}
				return nil
			},
		},
	)
	mux := setupMux(api)

	req := httptest.NewRequest(http.MethodGet, "/api/news/a", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	var article models.NewsArticle
	if err := json.Unmarshal(res.Body.Bytes(), &article); err != nil {
		t.Fatalf("invalid article response: %v", err)
	}
	if article.ID != "a" {
		t.Fatalf("unexpected article: %v", article)
	}
}
