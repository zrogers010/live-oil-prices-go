package handlers

import (
	"encoding/json"
	"live-oil-prices-go/internal/middleware"
	"live-oil-prices-go/internal/models"
	"net/http"
	"strconv"
	"strings"
)

type MarketDataClient interface {
	GetPrices() []models.Price
	GetChartData(symbol string, days int, interval string) models.ChartData
	GetPredictions() []models.Prediction
	GetAnalysis() models.MarketAnalysis
	GetHeroChart(symbol string, maxLiveBars int) models.HeroChart
}

type NewsClient interface {
	GetNews() []models.NewsArticle
	GetNewsByID(id string) *models.NewsArticle
}

type API struct {
	market MarketDataClient
	news   NewsClient
}

func NewAPI(market MarketDataClient, news NewsClient) *API {
	return &API{market: market, news: news}
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/prices", middleware.JSON(a.GetPrices))
	mux.HandleFunc("GET /api/charts/{symbol}", middleware.JSON(a.GetChartData))
	mux.HandleFunc("GET /api/hero/{symbol}", middleware.JSON(a.GetHeroChart))
	mux.HandleFunc("GET /api/news", middleware.JSON(a.GetNews))
	mux.HandleFunc("GET /api/news/{id}", middleware.JSON(a.GetNewsArticle))
	mux.HandleFunc("GET /api/predictions", middleware.JSON(a.GetPredictions))
	mux.HandleFunc("GET /api/analysis", middleware.JSON(a.GetAnalysis))
	mux.HandleFunc("GET /api/health", middleware.JSON(a.HealthCheck))
}

func (a *API) GetPrices(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.market.GetPrices())
}

func (a *API) GetChartData(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	if symbol == "" {
		symbol = "WTI"
	}

	days := 90
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}

	interval := r.URL.Query().Get("interval")

	data := a.market.GetChartData(symbol, days, interval)
	json.NewEncoder(w).Encode(data)
}

func (a *API) GetNews(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.news.GetNews())
}

func (a *API) GetNewsArticle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	article := a.news.GetNewsByID(id)
	if article == nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "article not found"})
		return
	}
	json.NewEncoder(w).Encode(article)
}

// GetHeroChart returns the homepage hero chart payload, choosing
// automatically between live streaming Pyth candles and a fallback view of
// the most recent complete trading day's intraday Yahoo bars.
//
// Query params:
//   - max: cap the number of LIVE bars returned (default 360 = 6 hours).
//     Hard ceiling 720 (12 hours). Ignored in prior-session mode, which
//     always returns the full session.
func (a *API) GetHeroChart(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	if symbol == "" {
		symbol = "WTI"
	}

	max := 360
	if v := r.URL.Query().Get("max"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 720 {
			max = parsed
		}
	}

	json.NewEncoder(w).Encode(a.market.GetHeroChart(symbol, max))
}

func (a *API) GetPredictions(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.market.GetPredictions())
}

func (a *API) GetAnalysis(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.market.GetAnalysis())
}

func (a *API) HealthCheck(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
