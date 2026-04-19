package services

import (
	"math"
	"testing"
)

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestSMA(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if got := SMA(values, 5); !approxEqual(got, 8.0, 1e-9) {
		t.Fatalf("SMA(5) over 1..10 expected 8, got %v", got)
	}
	if got := SMA(values, 10); !approxEqual(got, 5.5, 1e-9) {
		t.Fatalf("SMA(10) over 1..10 expected 5.5, got %v", got)
	}
	if got := SMA(values, 100); got != 0 {
		t.Fatalf("SMA with insufficient data should be 0, got %v", got)
	}
}

func TestEMARisesTowardConstantInput(t *testing.T) {
	// A constant series should yield EMA == constant once the seed has settled.
	values := make([]float64, 50)
	for i := range values {
		values[i] = 100
	}
	ema := EMA(values, 10)
	last := ema[len(ema)-1]
	if !approxEqual(last, 100, 1e-9) {
		t.Fatalf("EMA of constant 100 expected 100, got %v", last)
	}
}

func TestRSIBoundedAndDirectional(t *testing.T) {
	// Strictly increasing prices → RSI should be very high (100 with no losses).
	up := make([]float64, 30)
	for i := range up {
		up[i] = float64(50 + i)
	}
	if rsi := RSI(up, 14); rsi < 95 {
		t.Fatalf("strictly increasing series should have RSI ~100, got %v", rsi)
	}

	// Strictly decreasing → RSI should be very low.
	down := make([]float64, 30)
	for i := range down {
		down[i] = float64(100 - i)
	}
	if rsi := RSI(down, 14); rsi > 5 {
		t.Fatalf("strictly decreasing series should have RSI ~0, got %v", rsi)
	}

	// Insufficient data falls back to neutral 50.
	if rsi := RSI([]float64{1, 2, 3}, 14); rsi != 50 {
		t.Fatalf("insufficient data should yield neutral RSI=50, got %v", rsi)
	}
}

func TestMACDSignsForTrend(t *testing.T) {
	// Strong uptrend → fast EMA above slow EMA → positive MACD line.
	// (A perfectly linear series produces a constant MACD line and zero
	// histogram in steady state, so we test the line itself for direction.)
	up := make([]float64, 100)
	for i := range up {
		up[i] = float64(50 + i)
	}
	macd, _, _ := MACD(up, 12, 26, 9)
	if macd <= 0 {
		t.Fatalf("uptrend MACD line should be positive, got %v", macd)
	}

	down := make([]float64, 100)
	for i := range down {
		down[i] = float64(200 - i)
	}
	macd, _, _ = MACD(down, 12, 26, 9)
	if macd >= 0 {
		t.Fatalf("downtrend MACD line should be negative, got %v", macd)
	}

	// Accelerating uptrend → histogram should be positive (MACD accelerating).
	accel := make([]float64, 200)
	for i := range accel {
		accel[i] = 50 + 0.01*float64(i*i)
	}
	_, _, hist := MACD(accel, 12, 26, 9)
	if hist <= 0 {
		t.Fatalf("accelerating uptrend MACD histogram should be positive, got %v", hist)
	}
}

func TestHoltLinearProjectsTrend(t *testing.T) {
	// Pure linear series y = 2x + 10 → forecast should extrapolate the trend.
	values := make([]float64, 60)
	for i := range values {
		values[i] = 2*float64(i) + 10
	}
	level, trend, _, _, residStd := fitHoltLinear(values)

	// On a noiseless linear series the residual std should be tiny.
	if residStd > 0.5 {
		t.Fatalf("expected near-zero residual on linear input, got %v", residStd)
	}
	// Level should be close to the last value, trend close to 2.
	if !approxEqual(level, values[len(values)-1], 1.0) {
		t.Fatalf("level should be near last value %.2f, got %v", values[len(values)-1], level)
	}
	if !approxEqual(trend, 2.0, 0.2) {
		t.Fatalf("trend should be near 2.0, got %v", trend)
	}
}

func TestForecastBullishOnUptrend(t *testing.T) {
	// 200 days of a noisy uptrend.
	closes := make([]float64, 200)
	for i := range closes {
		closes[i] = 50 + float64(i)*0.2 + math.Sin(float64(i)/5)*0.5
	}
	f, err := Forecast(closes, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Predicted <= f.Current {
		t.Fatalf("expected predicted > current on uptrend, got predicted=%v current=%v", f.Predicted, f.Current)
	}
	if f.Direction != "bullish" {
		t.Fatalf("expected bullish direction on uptrend, got %q", f.Direction)
	}
	if f.Confidence < 0.2 || f.Confidence > 0.95 {
		t.Fatalf("expected confidence in [0.2, 0.95], got %v", f.Confidence)
	}
	if f.Low >= f.Predicted || f.High <= f.Predicted {
		t.Fatalf("prediction interval should bracket the point forecast: low=%v predicted=%v high=%v", f.Low, f.Predicted, f.High)
	}
	if f.Trend != "uptrend" {
		t.Fatalf("expected trend label uptrend, got %q", f.Trend)
	}
}

func TestForecastBearishOnDowntrend(t *testing.T) {
	closes := make([]float64, 200)
	for i := range closes {
		closes[i] = 100 - float64(i)*0.15 + math.Sin(float64(i)/5)*0.4
	}
	f, err := Forecast(closes, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Predicted >= f.Current {
		t.Fatalf("expected predicted < current on downtrend, got predicted=%v current=%v", f.Predicted, f.Current)
	}
	if f.Direction != "bearish" {
		t.Fatalf("expected bearish direction on downtrend, got %q", f.Direction)
	}
}

func TestForecastErrorsOnTooFewPoints(t *testing.T) {
	if _, err := Forecast([]float64{1, 2, 3}, 7); err == nil {
		t.Fatalf("expected error with too few points")
	}
	if _, err := Forecast(make([]float64, 100), 0); err == nil {
		t.Fatalf("expected error with zero horizon")
	}
}

func TestDampedHoltMatchesLinearWhenPhiIsOne(t *testing.T) {
	// With phi exactly 1.0 the damped recursion is identical to the linear
	// Holt recursion, so a forecast at phi=1 should match the same series
	// extrapolated by linear Holt fitted with the same (alpha, beta).
	values := make([]float64, 80)
	for i := range values {
		values[i] = 50 + 0.3*float64(i)
	}
	lvl1, tr1, _ := holtLinearFit(values, 0.5, 0.2)
	lvl2, tr2, _ := holtDampedFit(values, 0.5, 0.2, 1.0)
	if !approxEqual(lvl1, lvl2, 1e-9) || !approxEqual(tr1, tr2, 1e-9) {
		t.Fatalf("damped (phi=1) should equal linear; got linear=(%v,%v) damped=(%v,%v)", lvl1, tr1, lvl2, tr2)
	}
}

func TestDampedHoltDampsTrendForLargeHorizon(t *testing.T) {
	// At phi=0.9, the trend contribution to a 30-step forecast is
	// sum_{i=1..30} 0.9^i ≈ 8.51, vs 30 for linear. So a damped forecast
	// should sit closer to the level than a linear extrapolation.
	level, trend, phi := 100.0, 1.0, 0.9
	damped30 := dampedHoltForecast(level, trend, phi, 30)
	linear30 := dampedHoltForecast(level, trend, 1.0, 30)
	if damped30 >= linear30 {
		t.Fatalf("damped should extrapolate less than linear: damped=%v linear=%v", damped30, linear30)
	}
	// Damped contribution ≈ 8.51, so forecast ≈ 108.51.
	if damped30 < 107 || damped30 > 110 {
		t.Fatalf("damped 30-step forecast should land near 108.5, got %v", damped30)
	}
}

func TestBacktestMAPEIsLowOnCleanLinearSeries(t *testing.T) {
	// A near-noiseless linear series is exactly what damped Holt is meant
	// for; the rolling-origin MAPE should be small AND beat naive.
	values := make([]float64, 200)
	for i := range values {
		values[i] = 50 + 0.2*float64(i) + math.Sin(float64(i)/7)*0.1
	}
	mape, naive, n := backtestMAPEWithBaseline(values, 7, 30)
	if n < 20 {
		t.Fatalf("expected at least 20 backtest steps on a 200-point series, got %d", n)
	}
	if mape > 0.02 {
		t.Fatalf("expected MAPE < 2%% on a clean linear series, got %.4f", mape)
	}
	if mape >= naive {
		t.Fatalf("damped Holt should beat naive on a strongly trending series; model=%.4f naive=%.4f", mape, naive)
	}
}

func TestBacktestMAPEIsHigherOnNoisySeries(t *testing.T) {
	// Same length series but with much larger noise relative to drift —
	// model MAPE should be materially higher than the clean case.
	noisy := make([]float64, 200)
	rng := newDeterministicRNG(42)
	for i := range noisy {
		noisy[i] = 50 + 0.05*float64(i) + (rng()-0.5)*8.0
	}
	clean := make([]float64, 200)
	for i := range clean {
		clean[i] = 50 + 0.2*float64(i)
	}
	noisyMAPE, _, _ := backtestMAPEWithBaseline(noisy, 7, 30)
	cleanMAPE, _, _ := backtestMAPEWithBaseline(clean, 7, 30)
	if noisyMAPE <= cleanMAPE {
		t.Fatalf("noisy series should produce a larger MAPE than the clean one; noisy=%v clean=%v", noisyMAPE, cleanMAPE)
	}
}

func TestConfidenceDifferentiatesByMAPE(t *testing.T) {
	// Same direction + identical indicator agreement; the only difference
	// is the empirical MAPE. The confidence ordering must follow MAPE.
	naive := 0.06 // typical 7-day naive baseline for oil
	good := computeConfidence(0.020, naive, 30, 60, 1.0, "uptrend", "bullish")
	mid := computeConfidence(0.040, naive, 30, 60, 1.0, "uptrend", "bullish")
	bad := computeConfidence(0.090, naive, 30, 60, 1.0, "uptrend", "bullish")
	if !(good > mid && mid > bad) {
		t.Fatalf("expected confidence to decline as MAPE grows: good=%v mid=%v bad=%v", good, mid, bad)
	}
	if good < 0.80 {
		t.Fatalf("expected good MAPE + full agreement to read >=0.80, got %v", good)
	}
	if bad > 0.45 {
		t.Fatalf("expected 9%% MAPE to land near the floor, got %v", bad)
	}
}

func TestConfidenceFallsBackWhenNoBacktest(t *testing.T) {
	// Empty backtest (samples=0) must return a deliberately-low score
	// rather than pretending to be confident.
	c := computeConfidence(0, 0, 0, 60, 1.0, "uptrend", "bullish")
	if c > 0.5 {
		t.Fatalf("expected fallback confidence below 0.5, got %v", c)
	}
}

func TestBackAdjustRollsRemovesContractGaps(t *testing.T) {
	// Three-bar series with a 12% downward roll between bars 1 and 2.
	// After back-adjustment, bar 0 and bar 1 should be lifted down so the
	// step-to-step ratio is continuous (~1.0).
	raw := []float64{100, 100, 88}
	adj := backAdjustRolls(raw, 0.07)
	if math.Abs(adj[2]-88) > 1e-9 {
		t.Fatalf("most recent value must be preserved, got %v", adj[2])
	}
	// After adjustment, adj[1]/adj[0] should be ~1.0 (no roll), and
	// adj[2]/adj[1] should be ~1.0 (gap closed).
	if r := adj[2] / adj[1]; math.Abs(r-1.0) > 0.001 {
		t.Fatalf("expected adjusted series to be continuous through the roll, ratio=%v", r)
	}
	// Adjusted bar 0 should be ~88 (lifted down by the 12% gap).
	if math.Abs(adj[0]-88) > 0.5 {
		t.Fatalf("expected back-adjusted prior bar near 88, got %v", adj[0])
	}
}

func TestBackAdjustRollsLeavesCleanSeriesAlone(t *testing.T) {
	// A series with only normal day-to-day moves should pass through
	// unchanged.
	raw := []float64{100, 101, 102, 100.5, 99}
	adj := backAdjustRolls(raw, 0.07)
	for i := range raw {
		if math.Abs(raw[i]-adj[i]) > 1e-9 {
			t.Fatalf("expected clean series to pass through unchanged at i=%d: raw=%v adj=%v", i, raw[i], adj[i])
		}
	}
}

// newDeterministicRNG returns a closure yielding pseudo-random floats in [0, 1).
// Avoids pulling in math/rand at the top of the file just for tests.
func newDeterministicRNG(seed uint64) func() float64 {
	state := seed
	return func() float64 {
		// xorshift64 — small, deterministic, good enough for tests.
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return float64(state%1_000_000) / 1_000_000.0
	}
}
