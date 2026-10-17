package services

import (
	"live-oil-prices-go/internal/models"
	"math"
	"testing"
)

func fakeYahooWithHistory() *YahooFinanceService {
	y := &YahooFinanceService{
		prices:      map[string]models.Price{},
		history:     map[string][]float64{},
		historyOHLC: map[string][]models.OHLCV{},
		intraday:    map[string]intradayBars{},
	}
	for sym, base := range map[string]float64{"WTI": 90, "BRENT": 100} {
		h := make([]float64, 200)
		for i := range h {
			h[i] = base + 0.03*float64(i) + 2*math.Sin(float64(i)/5)
		}
		y.history[sym] = h
		cur := h[len(h)-1] + 0.5
		y.prices[sym] = models.Price{Symbol: sym, Name: sym, Price: cur, Source: "yahoo", Available: true}
	}
	return y
}

// TestGetPredictionsTrackCurrentPricesReal: with real (fake-injected) Yahoo
// prices and history, predictions are produced only for available symbols,
// use the live price as Current, and come from the model (not the fallback).
func TestGetPredictionsTrackCurrentPricesReal(t *testing.T) {
	svc := newDeterministicMarketDataService()
	svc.yahoo = fakeYahooWithHistory()
	want := map[string]float64{}
	for _, p := range svc.GetPrices() {
		if p.Available {
			want[p.Symbol] = p.Price
		}
	}
	if len(want) != 2 {
		t.Fatalf("expected 2 available prices (WTI, BRENT), got %d", len(want))
	}

	preds := svc.GetPredictions()
	if len(preds) != 2 {
		t.Fatalf("expected 2 predictions (unavailable NATGAS/HEATING skipped), got %d", len(preds))
	}
	for _, p := range preds {
		cur, ok := want[p.Symbol]
		if !ok {
			t.Fatalf("unexpected prediction for unavailable symbol %s", p.Symbol)
		}
		if math.Abs(p.Current-math.Round(cur*100)/100) > 0.005 {
			t.Errorf("%s: current %.2f != live price %.2f", p.Symbol, p.Current, cur)
		}
		if p.Predicted <= 0 || p.Model == "fallback" || p.Source != "yahoo" {
			t.Errorf("%s: expected model forecast, got predicted=%.2f model=%q source=%q", p.Symbol, p.Predicted, p.Model, p.Source)
		}
	}
}

// TestGetPredictionsEmptyWhenNoData: no feed => no predictions at all.
func TestGetPredictionsEmptyWhenNoData(t *testing.T) {
	if preds := newDeterministicMarketDataService().GetPredictions(); len(preds) != 0 {
		t.Fatalf("expected no predictions without data, got %d", len(preds))
	}
}
