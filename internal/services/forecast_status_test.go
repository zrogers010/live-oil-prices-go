package services

import (
	"live-oil-prices-go/internal/models"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// randomWalk returns a deterministic driftless random walk. On such a
// series "no change" is the optimal forecast, so the damped-Holt model
// should not beat the naive baseline out of sample.
func randomWalk(n int, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	out := make([]float64, n)
	v := 90.0
	for i := range out {
		v *= 1 + 0.02*r.NormFloat64()
		out[i] = v
	}
	return out
}

func trendingSeries(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 60 + 0.25*float64(i) + 0.3*math.Sin(float64(i))
	}
	return out
}

func TestForecastStatusOKWhenModelBeatsNaive(t *testing.T) {
	f, err := Forecast(trendingSeries(300), 7)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != ForecastStatusOK {
		t.Fatalf("status = %q, want %q (mape=%.4f naive=%.4f)", f.Status, ForecastStatusOK, f.MAPE, f.NaiveMAPE)
	}
	if f.MAPE >= f.NaiveMAPE {
		t.Fatalf("status OK requires mape < naive, got %.4f >= %.4f", f.MAPE, f.NaiveMAPE)
	}
	if f.Predicted <= f.Current {
		t.Fatalf("expected an upward forecast on an uptrend, got %.2f -> %.2f", f.Current, f.Predicted)
	}
}

func TestForecastStatusNoEdgeOnRandomWalk(t *testing.T) {
	found := 0
	for seed := int64(1); seed <= 20; seed++ {
		f, err := Forecast(randomWalk(500, seed), 7)
		if err != nil {
			t.Fatal(err)
		}
		switch f.Status {
		case ForecastStatusNoEdge:
			found++
			if f.MAPE < f.NaiveMAPE {
				t.Fatalf("seed %d: no-edge but mape %.4f < naive %.4f", seed, f.MAPE, f.NaiveMAPE)
			}
			if f.Predicted != f.Current || f.Direction != "neutral" {
				t.Fatalf("seed %d: no-edge must not carry a directional forecast, got predicted=%.4f current=%.4f dir=%s", seed, f.Predicted, f.Current, f.Direction)
			}
		case ForecastStatusOK:
			if f.MAPE >= f.NaiveMAPE {
				t.Fatalf("seed %d: status OK but mape %.4f >= naive %.4f", seed, f.MAPE, f.NaiveMAPE)
			}
		default:
			t.Fatalf("seed %d: unexpected status %q with 500 closes", seed, f.Status)
		}
	}
	if found == 0 {
		t.Fatal("expected the model to lack an edge on at least one random walk")
	}
}

func TestForecastStatusInsufficientOnShortHistory(t *testing.T) {
	// 70 closes is enough to fit (>= 30) but leaves only 3 held-out
	// forecasts, too few to judge the model against naive.
	f, err := Forecast(trendingSeries(70), 7)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != ForecastStatusInsufficient {
		t.Fatalf("status = %q, want %q (backtest steps %d)", f.Status, ForecastStatusInsufficient, f.BacktestSteps)
	}
	if f.Predicted != f.Current {
		t.Fatalf("insufficient data must not carry a forecast, got %.2f vs %.2f", f.Predicted, f.Current)
	}
}

func TestSpliceCurrentClose(t *testing.T) {
	hist := []float64{10, 11, 12}
	// Yahoo daily bars are stamped at 00:00 ET of the trading day.
	barOct16 := time.Date(2026, 10, 16, 4, 0, 0, 0, time.UTC).Unix()
	quoteOct16 := time.Date(2026, 10, 16, 20, 59, 0, 0, time.UTC)
	quoteOct19 := time.Date(2026, 10, 19, 14, 0, 0, 0, time.UTC)

	got := spliceCurrentClose(hist, barOct16, true, 12.5, quoteOct16)
	if len(got) != 3 || got[2] != 12.5 {
		t.Fatalf("same-day quote should replace the last bar, got %v", got)
	}
	got = spliceCurrentClose(hist, barOct16, true, 12.5, quoteOct19)
	if len(got) != 4 || got[3] != 12.5 || got[2] != 12 {
		t.Fatalf("next-day quote should append, got %v", got)
	}
	got = spliceCurrentClose(hist, 0, false, 12.5, quoteOct16)
	if len(got) != 4 || got[3] != 12.5 {
		t.Fatalf("without bar timestamps we append, got %v", got)
	}
	if hist[2] != 12 || len(hist) != 3 {
		t.Fatalf("input slice was mutated: %v", hist)
	}
}

func fakeYahooSeries(series map[string][]float64) *YahooFinanceService {
	y := &YahooFinanceService{
		prices:      map[string]models.Price{},
		history:     map[string][]float64{},
		historyOHLC: map[string][]models.OHLCV{},
		intraday:    map[string]intradayBars{},
	}
	for sym, h := range series {
		y.history[sym] = h
		cur := 0.0
		if len(h) > 0 {
			cur = h[len(h)-1]
		}
		y.prices[sym] = models.Price{Symbol: sym, Name: sym, Price: cur, Source: "yahoo", Available: true,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	}
	return y
}

// TestPredictionsNoEdgeCarryNoFakeForecast reproduces the prod symptom
// (predicted == current, "+0.00%"): when the model can't beat naive the
// prediction must say so and carry no predicted price or confidence.
func TestPredictionsNoEdgeCarryNoFakeForecast(t *testing.T) {
	var rw []float64
	for seed := int64(1); seed <= 50 && rw == nil; seed++ {
		cand := randomWalk(500, seed)
		if f, _ := Forecast(cand, 7); f.Status == ForecastStatusNoEdge {
			rw = cand
		}
	}
	if rw == nil {
		t.Fatal("could not find a no-edge random walk")
	}
	svc := newDeterministicMarketDataService()
	svc.yahoo = fakeYahooSeries(map[string][]float64{"WTI": rw, "BRENT": trendingSeries(300)})

	byID := map[string]models.Prediction{}
	for _, p := range svc.GetPredictions() {
		byID[p.Symbol] = p
	}

	wti, ok := byID["WTI"]
	if !ok {
		t.Fatal("missing WTI prediction")
	}
	if wti.Status != ForecastStatusNoEdge {
		t.Fatalf("WTI status = %q, want no-edge", wti.Status)
	}
	if wti.Predicted != 0 || wti.PredictedLow != 0 || wti.PredictedHigh != 0 || wti.Confidence != 0 {
		t.Fatalf("no-edge prediction must not carry numbers: %+v", wti)
	}
	if wti.Current <= 0 || wti.MAPE <= 0 || wti.NaiveMAPE <= 0 || wti.BacktestSteps < minBacktestSteps {
		t.Fatalf("no-edge prediction should still report the real backtest: %+v", wti)
	}
	if strings.Contains(wti.Analysis, "+0.00%") || !strings.Contains(wti.Analysis, "no 7-day forecast") {
		t.Fatalf("analysis should state there is no forecast, got %q", wti.Analysis)
	}

	brent := byID["BRENT"]
	if brent.Status != ForecastStatusOK || brent.Predicted <= 0 || brent.Predicted == brent.Current {
		t.Fatalf("trending BRENT should carry a real forecast, got %+v", brent)
	}
}

func TestPredictionsInsufficientHistory(t *testing.T) {
	svc := newDeterministicMarketDataService()
	svc.yahoo = fakeYahooSeries(map[string][]float64{"WTI": trendingSeries(12)})
	preds := svc.GetPredictions()
	if len(preds) != 1 {
		t.Fatalf("expected 1 prediction, got %d", len(preds))
	}
	p := preds[0]
	if p.Status != ForecastStatusInsufficient || p.Predicted != 0 || p.Confidence != 0 {
		t.Fatalf("expected insufficient-data with no forecast, got %+v", p)
	}
	if p.Source != "yahoo" || p.Current <= 0 {
		t.Fatalf("insufficient prediction should keep the real spot and source, got %+v", p)
	}
}

func TestHeroChartDelayedWithoutPyth(t *testing.T) {
	svc := newDeterministicMarketDataService() // pyth == nil
	if hc := svc.GetHeroChart("WTI", 0); !hc.Delayed {
		t.Fatal("hero chart should be marked delayed when no real-time feed is configured")
	}
	svc.pyth = &PythService{apiKey: "configured"}
	if hc := svc.GetHeroChart("WTI", 0); hc.Delayed {
		t.Fatal("hero chart should not be marked delayed when Pyth is configured")
	}
}
