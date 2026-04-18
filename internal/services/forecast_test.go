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
	if f.Confidence < 0.3 || f.Confidence > 0.9 {
		t.Fatalf("expected confidence in [0.3, 0.9], got %v", f.Confidence)
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
