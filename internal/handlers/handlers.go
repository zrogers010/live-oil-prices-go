package handlers

import (
	"encoding/json"
	"live-oil-prices-go/internal/middleware"
	"live-oil-prices-go/internal/services"
	"net/http"
	"strconv"
)

type API struct {
	market *services.MarketDataService
	news   *services.NewsFeedService
}

func NewAPI(market *services.MarketDataService, news *services.NewsFeedService) *API {
	return &API{market: market, news: news}
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/prices", middleware.JSON(a.GetPrices))
	mux.HandleFunc("GET /api/charts/{symbol}", middleware.JSON(a.GetChartData))
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

func (a *API) GetPredictions(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.market.GetPredictions())
}

func (a *API) GetAnalysis(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.market.GetAnalysis())
}

func (a *API) HealthCheck(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
