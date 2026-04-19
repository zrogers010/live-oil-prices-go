package services

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func newDeterministicMarketDataService() *MarketDataService {
	return &MarketDataService{
		rng: rand.New(rand.NewSource(42)),
		basePrices: map[string]float64{
			"WTI":    72.45,
			"BRENT":  76.82,
			"NATGAS": 3.24,
			"HEATING": 2.35,
			"RBOB":   2.18,
			"OPEC":   74.50,
			"DUBAI":  75.10,
			"MURBAN": 76.30,
			"WCS":    58.20,
			"GASOIL": 685.50,
		},
	}
}

func TestGetPricesReturnsAllCommodities(t *testing.T) {
	svc := newDeterministicMarketDataService()
	prices := svc.GetPrices()

	if len(prices) != len(allCommodities) {
		t.Fatalf("expected %d prices, got %d", len(allCommodities), len(prices))
	}

	for _, p := range prices {
		if p.Symbol == "" {
			t.Fatalf("price symbol should not be empty")
		}
		if p.Name == "" {
			t.Fatalf("price name should not be empty")
		}
		if p.UpdatedAt == "" {
			t.Fatalf("updatedAt should be set")
		}
		if math.IsNaN(p.Price) || math.IsNaN(p.Change) || math.IsNaN(p.ChangePct) {
			t.Fatalf("price fields should be numeric: %#v", p)
		}
	}
}

func TestGetChartDataDefaultsAndOverrides(t *testing.T) {
	svc := newDeterministicMarketDataService()

	short := svc.GetChartData("WTI", 5, "")
	if short.Symbol != "WTI" {
		t.Fatalf("expected symbol WTI, got %q", short.Symbol)
	}
	if short.Name != "WTI Crude Oil" {
		t.Fatalf("expected WTI name, got %q", short.Name)
	}
	if short.Interval != "2h" {
		t.Fatalf("expected 2h interval for 5 days, got %q", short.Interval)
	}
	if got := len(short.Data); got == 0 || got > 60 {
		t.Fatalf("expected short chart data length between 1 and 60, got %d", got)
	}

	medium := svc.GetChartData("BRENT", 30, "")
	if medium.Interval != "4h" {
		t.Fatalf("expected 4h interval for 30 days, got %q", medium.Interval)
	}
	if len(medium.Data) == 0 {
		t.Fatalf("expected chart data for 30 days")
	}

	long := svc.GetChartData("NATGAS", 120, "")
	if long.Interval != "1d" {
		t.Fatalf("expected 1d interval for 120 days, got %q", long.Interval)
	}
	if len(long.Data) == 0 {
		t.Fatalf("expected long chart data")
	}

	override := svc.GetChartData("GASOIL", 120, "2h")
	if override.Interval != "2h" {
		t.Fatalf("expected explicit interval override to win, got %q", override.Interval)
	}
}

func TestGetChartDataUnknownSymbolFallsBack(t *testing.T) {
	svc := newDeterministicMarketDataService()
	data := svc.GetChartData("UNKNOWN", 7, "")

	if data.Symbol != "UNKNOWN" {
		t.Fatalf("expected symbol to remain UNKNOWN, got %q", data.Symbol)
	}
	if data.Name != "UNKNOWN" {
		t.Fatalf("expected unknown symbol to fallback to symbol name, got %q", data.Name)
	}

	if data.Interval != "2h" {
		t.Fatalf("expected interval to remain valid for 7 days, got %q", data.Interval)
	}
	if len(data.Data) == 0 {
		t.Fatalf("expected data for unknown symbol")
	}
}

func TestGenerateDailySkipsWeekends(t *testing.T) {
	svc := newDeterministicMarketDataService()
	data := svc.generateDaily(rand.New(rand.NewSource(42)), 100, 30)

	if len(data) == 0 || len(data) > 30 {
		t.Fatalf("expected 1..30 daily points, got %d", len(data))
	}

	for _, candle := range data {
		wd := time.Unix(candle.Time, 0).Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			t.Fatalf("did not expect weekend candle at %v", wd)
		}
	}
}

// TestGetChartDataIsStableAcrossCalls is a regression guard for the bug
// where flipping back and forth between commodity tabs returned a freshly
// randomised series each time. With the deterministic seed (or real Yahoo
// data), two consecutive identical requests must return identical bars.
func TestGetChartDataIsStableAcrossCalls(t *testing.T) {
	svc := newDeterministicMarketDataService()

	first := svc.GetChartData("OPEC", 30, "")
	second := svc.GetChartData("OPEC", 30, "")
	third := svc.GetChartData("OPEC", 30, "")

	if len(first.Data) == 0 {
		t.Fatalf("expected non-empty chart data")
	}
	if len(first.Data) != len(second.Data) || len(second.Data) != len(third.Data) {
		t.Fatalf("inconsistent bar counts across calls: %d / %d / %d",
			len(first.Data), len(second.Data), len(third.Data))
	}
	for i := range first.Data {
		if first.Data[i] != second.Data[i] || second.Data[i] != third.Data[i] {
			t.Fatalf("bar %d differs across calls: first=%+v second=%+v third=%+v",
				i, first.Data[i], second.Data[i], third.Data[i])
		}
	}
}

func TestGetPredictionsTrackCurrentPrices(t *testing.T) {
	svcForCurrent := newDeterministicMarketDataService()
	expected := map[string]float64{}
	for _, p := range svcForCurrent.GetPrices() {
		expected[p.Symbol] = p.Price
	}

	svc := newDeterministicMarketDataService()
	predictions := svc.GetPredictions()
	if len(predictions) != 4 {
		t.Fatalf("expected 4 predictions, got %d", len(predictions))
	}

	for _, p := range predictions {
		current, ok := expected[p.Symbol]
		if !ok {
			t.Fatalf("prediction for unexpected symbol %q", p.Symbol)
		}
		if p.Current != current {
			t.Fatalf("prediction current mismatch for %s: expected %.2f got %.2f", p.Symbol, current, p.Current)
		}
		if p.Predicted <= 0 {
			t.Fatalf("predicted price should be positive for %s", p.Symbol)
		}
	}
}

func TestGetAnalysisIncludesCoreFields(t *testing.T) {
	svc := newDeterministicMarketDataService()
	analysis := svc.GetAnalysis()

	if analysis.Sentiment != "bullish" {
		t.Fatalf("expected bullish sentiment, got %q", analysis.Sentiment)
	}
	if analysis.Score < 0 || analysis.Score > 100 {
		t.Fatalf("expected score between 0 and 100, got %.2f", analysis.Score)
	}
	if len(analysis.KeyPoints) == 0 {
		t.Fatalf("expected analysis key points")
	}
	if analysis.Technical.Trend == "" {
		t.Fatalf("expected technical trend")
	}
	if analysis.UpdatedAt == "" {
		t.Fatalf("expected updatedAt")
	}
}
