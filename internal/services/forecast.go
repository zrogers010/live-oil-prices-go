package services

import (
	"fmt"
	"math"
)

// ForecastResult bundles a point forecast with diagnostic indicators that
// can be used to label direction, confidence, and produce analysis prose.
type ForecastResult struct {
	Current     float64
	Predicted   float64
	Low         float64
	High        float64
	Direction   string  // "bullish" | "bearish" | "neutral"
	Confidence  float64 // 0..1
	RSI14       float64
	MACD        float64
	MACDSignal  float64
	MACDHist    float64
	MA50        float64
	MA200       float64
	Trend       string // "uptrend" | "downtrend" | "sideways"
	HorizonDays int
	Samples     int
}

// Forecast produces a point forecast `horizonDays` steps ahead from `closes`,
// along with technical indicator readings and a heuristic confidence score.
//
// The model is a Holt linear (double exponential smoothing) forecast with
// prediction intervals derived from the residual standard deviation. We pick
// alpha/beta with a small grid search minimising in-sample SSE.
func Forecast(closes []float64, horizonDays int) (ForecastResult, error) {
	if horizonDays <= 0 {
		return ForecastResult{}, fmt.Errorf("horizonDays must be > 0")
	}
	if len(closes) < 30 {
		return ForecastResult{}, fmt.Errorf("need at least 30 closes, got %d", len(closes))
	}

	current := closes[len(closes)-1]

	level, trend, alpha, beta, residStd := fitHoltLinear(closes)
	predicted := level + float64(horizonDays)*trend

	// 80% prediction interval: ±1.28 * residStd * sqrt(h)
	band := 1.28 * residStd * math.Sqrt(float64(horizonDays))
	low := predicted - band
	high := predicted + band

	rsi := RSI(closes, 14)
	macd, signal, hist := MACD(closes, 12, 26, 9)
	ma50 := SMA(closes, 50)
	ma200 := SMA(closes, 200)

	trendLabel := "sideways"
	switch {
	case ma50 > 0 && ma200 > 0 && ma50 > ma200*1.005:
		trendLabel = "uptrend"
	case ma50 > 0 && ma200 > 0 && ma50 < ma200*0.995:
		trendLabel = "downtrend"
	}

	direction := classifyDirection(current, predicted, rsi, hist, trendLabel)
	confidence := computeConfidence(current, residStd, alpha, beta, rsi, hist, trendLabel, direction)

	return ForecastResult{
		Current:     current,
		Predicted:   predicted,
		Low:         low,
		High:        high,
		Direction:   direction,
		Confidence:  confidence,
		RSI14:       rsi,
		MACD:        macd,
		MACDSignal:  signal,
		MACDHist:    hist,
		MA50:        ma50,
		MA200:       ma200,
		Trend:       trendLabel,
		HorizonDays: horizonDays,
		Samples:     len(closes),
	}, nil
}

// SMA returns the simple moving average over the last `period` values.
// Returns 0 if there are fewer than `period` values.
func SMA(values []float64, period int) float64 {
	if period <= 0 || len(values) < period {
		return 0
	}
	sum := 0.0
	for _, v := range values[len(values)-period:] {
		sum += v
	}
	return sum / float64(period)
}

// EMA returns the full exponential moving average series.
// EMA[0..period-2] are zero; EMA[period-1] is the SMA seed; subsequent values
// use the recursive EMA formula with alpha = 2 / (period + 1).
func EMA(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 0 || len(values) < period {
		return out
	}
	alpha := 2.0 / float64(period+1)
	seed := 0.0
	for i := 0; i < period; i++ {
		seed += values[i]
	}
	seed /= float64(period)
	out[period-1] = seed
	for i := period; i < len(values); i++ {
		out[i] = alpha*values[i] + (1-alpha)*out[i-1]
	}
	return out
}

// RSI returns the Wilder-smoothed Relative Strength Index for the most recent
// bar. Returns 50 (neutral) when there is insufficient data.
func RSI(closes []float64, period int) float64 {
	if period <= 0 || len(closes) <= period {
		return 50
	}
	avgGain, avgLoss := 0.0, 0.0
	for i := 1; i <= period; i++ {
		ch := closes[i] - closes[i-1]
		if ch > 0 {
			avgGain += ch
		} else {
			avgLoss -= ch
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	for i := period + 1; i < len(closes); i++ {
		ch := closes[i] - closes[i-1]
		gain, loss := 0.0, 0.0
		if ch > 0 {
			gain = ch
		} else {
			loss = -ch
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

// MACD returns the (macd, signal, histogram) values for the most recent bar
// using the classic (fast, slow, signalPeriod) EMAs.
func MACD(closes []float64, fast, slow, signalPeriod int) (float64, float64, float64) {
	if len(closes) < slow+signalPeriod {
		return 0, 0, 0
	}
	emaFast := EMA(closes, fast)
	emaSlow := EMA(closes, slow)
	macdLine := make([]float64, len(closes))
	for i := slow - 1; i < len(closes); i++ {
		macdLine[i] = emaFast[i] - emaSlow[i]
	}
	// Build the signal EMA only over the populated MACD region.
	macdTail := macdLine[slow-1:]
	signalTail := EMA(macdTail, signalPeriod)
	macd := macdLine[len(macdLine)-1]
	signal := signalTail[len(signalTail)-1]
	return macd, signal, macd - signal
}

// fitHoltLinear performs a coarse grid search over (alpha, beta) and returns
// the final level, trend, chosen parameters, and residual standard deviation
// for the best-fit Holt linear smoother.
func fitHoltLinear(values []float64) (level, trend, bestAlpha, bestBeta, residStd float64) {
	bestSSE := math.Inf(1)
	for _, a := range []float64{0.1, 0.2, 0.3, 0.5, 0.7, 0.9} {
		for _, b := range []float64{0.05, 0.1, 0.2, 0.3, 0.5} {
			l, t, sse := holtLinearFit(values, a, b)
			if sse < bestSSE {
				bestSSE = sse
				level, trend = l, t
				bestAlpha, bestBeta = a, b
			}
		}
	}
	// Residual standard deviation (one-step in-sample errors).
	residStd = math.Sqrt(bestSSE / float64(len(values)-2))
	return
}

// holtLinearFit runs a Holt linear smoother over `values` with the given
// (alpha, beta) and returns the final (level, trend, sum of squared one-step
// in-sample residuals).
func holtLinearFit(values []float64, alpha, beta float64) (level, trend, sse float64) {
	if len(values) < 2 {
		return values[0], 0, 0
	}
	level = values[0]
	trend = values[1] - values[0]
	for i := 1; i < len(values); i++ {
		fcast := level + trend
		err := values[i] - fcast
		sse += err * err
		newLevel := alpha*values[i] + (1-alpha)*(level+trend)
		newTrend := beta*(newLevel-level) + (1-beta)*trend
		level = newLevel
		trend = newTrend
	}
	return
}

// classifyDirection combines the forecast delta, RSI level, MACD histogram,
// and trend label into a directional bias.
func classifyDirection(current, predicted, rsi, macdHist float64, trend string) string {
	score := 0
	delta := (predicted - current) / current
	switch {
	case delta > 0.005:
		score++
	case delta < -0.005:
		score--
	}
	switch {
	case rsi > 55:
		score++
	case rsi < 45:
		score--
	}
	switch {
	case macdHist > 0:
		score++
	case macdHist < 0:
		score--
	}
	switch trend {
	case "uptrend":
		score++
	case "downtrend":
		score--
	}
	switch {
	case score >= 2:
		return "bullish"
	case score <= -2:
		return "bearish"
	default:
		return "neutral"
	}
}

// computeConfidence maps model fit quality and indicator agreement to a 0..1
// confidence score. Calibrated to land in roughly 0.45..0.85 for typical data.
func computeConfidence(current, residStd, alpha, beta, rsi, macdHist float64, trend, direction string) float64 {
	if current == 0 {
		return 0.5
	}
	// Lower residual volatility relative to price → tighter fit → higher confidence.
	relErr := residStd / current
	fit := math.Max(0, 1-relErr*40) // ~2.5% daily noise → fit ≈ 0
	fit = math.Min(fit, 1)

	// Indicator agreement with the chosen direction.
	agree := 0
	total := 3
	switch direction {
	case "bullish":
		if rsi > 50 {
			agree++
		}
		if macdHist > 0 {
			agree++
		}
		if trend == "uptrend" {
			agree++
		}
	case "bearish":
		if rsi < 50 {
			agree++
		}
		if macdHist < 0 {
			agree++
		}
		if trend == "downtrend" {
			agree++
		}
	default:
		// Neutral: confidence is whatever the fit suggests, capped lower.
		return clamp(0.4+0.3*fit, 0.3, 0.7)
	}
	agreeFrac := float64(agree) / float64(total)

	// Heavier alpha/beta → more reactive (less smooth) → slightly less confident.
	smoothPenalty := 0.05 * (alpha + beta)

	c := 0.45 + 0.25*fit + 0.2*agreeFrac - smoothPenalty
	return clamp(c, 0.3, 0.9)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
