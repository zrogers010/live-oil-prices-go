package handlers

import (
	"live-oil-prices-go/internal/models"
	"net/http/httptest"
	"strings"
	"testing"
)

func yahooPrices() []models.Price {
	return []models.Price{
		{Symbol: "WTI", Name: "WTI Crude Oil", Price: 99.02, Change: 1.05, ChangePct: 1.07, High: 101.9, Low: 96.85,
			UpdatedAt: "2026-10-16T20:59:58Z", Contract: "Nov 2026 Contract", Source: "yahoo", Available: true},
		{Symbol: "BRENT", Name: "Brent Crude Oil", Price: 109.86, Change: 3.94, ChangePct: 3.72, High: 110, Low: 104.92,
			UpdatedAt: "2026-10-16T20:59:31Z", Contract: "Dec 2026 Contract", Source: "yahoo", Available: true},
	}
}

func renderHome(t *testing.T, market MarketDataClient) string {
	t.Helper()
	if err := InitPageTemplates("../../web/templates"); err != nil {
		t.Fatalf("init templates: %v", err)
	}
	w := httptest.NewRecorder()
	NewAPI(market, &fakeNewsFeedService{}).ServeHome(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatal("page did not render completely")
	}
	return body
}

// The hero tagline must describe the data on the page, never a
// "connecting to the feed" placeholder, and never claim Pyth when every
// price comes from Yahoo.
func TestHomeHeroStatusAndSourcesWithYahooOnly(t *testing.T) {
	body := renderHome(t, &fakeMarketDataService{getPricesFunc: yahooPrices})
	if strings.Contains(strings.ToLower(body), "connecting to") {
		t.Error("home page still shows a 'connecting to' placeholder")
	}
	if !strings.Contains(body, `id="heroTaglineAccent">delayed futures quotes via Yahoo Finance<`) {
		t.Error("hero tagline should say the quotes are delayed Yahoo data")
	}
	if strings.Contains(body, "pyth.network") || strings.Contains(body, "Pyth Network") {
		t.Error("page credits Pyth although no price comes from Pyth")
	}
	if !strings.Contains(body, "Futures prices via Yahoo Finance") {
		t.Error("footer should attribute prices to Yahoo Finance")
	}
	if !strings.Contains(body, "Dec 2026 Contract &mdash; Front Month") {
		t.Error("hero should render the Brent contract label from the data")
	}
	if !strings.Contains(body, `<link rel="canonical" href="https://www.liveoilprices.com/">`) {
		t.Error("canonical should point at the www host")
	}
}

func TestHomeCreditsPythOnlyWhenInUse(t *testing.T) {
	body := renderHome(t, &fakeMarketDataService{getPricesFunc: func() []models.Price {
		p := yahooPrices()
		p[0].Source = "pyth"
		return p
	}})
	if !strings.Contains(body, "pyth.network") {
		t.Error("footer should credit Pyth when a price comes from Pyth")
	}
}

func TestHomeEmptyStates(t *testing.T) {
	body := renderHome(t, &fakeMarketDataService{})
	if !strings.Contains(body, `id="heroTaglineAccent">prices temporarily unavailable<`) {
		t.Error("hero tagline should say prices are unavailable")
	}
	if !strings.Contains(body, "market-table-empty") {
		t.Error("market table should render an explicit empty state")
	}
}

// A no-edge prediction (the prod case: predicted == current) must render a
// clear "no forecast" state, not "$99.02 (+0.00%)".
func TestHomeForecastNoEdgeState(t *testing.T) {
	body := renderHome(t, &fakeMarketDataService{
		getPricesFunc: yahooPrices,
		getPredictionsFunc: func() []models.Prediction {
			return []models.Prediction{
				{Symbol: "WTI", Name: "WTI Crude Oil", Current: 99.02, Timeframe: "7 days", Direction: "neutral",
					Status: "no-edge", Model: "holt-damped+backtest+rsi/macd", Source: "yahoo",
					Analysis: "WTI Crude Oil: no 7-day forecast.", MAPE: 0.0811, NaiveMAPE: 0.077, Skill: -0.053, BacktestSteps: 30},
				{Symbol: "BRENT", Name: "Brent Crude Oil", Current: 109.86, Predicted: 111.2, PredictedLow: 104, PredictedHigh: 118,
					Timeframe: "7 days", Direction: "bullish", Confidence: 0.55, Status: "forecast", Model: "holt-damped+backtest+rsi/macd",
					Source: "yahoo", Analysis: "Brent forecast.", MAPE: 0.05, NaiveMAPE: 0.06, Skill: 0.16, BacktestSteps: 30},
			}
		},
	})
	// html/template escapes "+" as "&#43;" inside the projection span.
	if strings.Contains(body, "+0.00%") || strings.Contains(body, "&#43;0.00%") {
		t.Error("forecast area shows a fake +0.00% projection")
	}
	if !strings.Contains(body, "No reliable forecast") {
		t.Error("no-edge card should explain there is no reliable forecast")
	}
	if !strings.Contains(body, "$111.20 (&#43;1.22%)") {
		t.Error("a real forecast should still render its projection")
	}
}

func TestForecastViewInsufficientData(t *testing.T) {
	v := toForecastView(models.Prediction{Symbol: "WTI", Current: 99, Timeframe: "7 days", Status: "insufficient-data", Source: "yahoo"})
	if v.HasForecast || v.NoForecastTitle != "Not enough data to forecast" || v.ConfidencePct != 0 {
		t.Fatalf("unexpected view for insufficient data: %+v", v)
	}
	// Legacy payloads without a status but with a real predicted price still render.
	v = toForecastView(models.Prediction{Symbol: "WTI", Current: 99, Predicted: 100, Timeframe: "7 days"})
	if !v.HasForecast {
		t.Fatal("legacy prediction with a predicted price should render as a forecast")
	}
}
