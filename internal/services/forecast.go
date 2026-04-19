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
	// MAPE is the rolling-origin out-of-sample mean-absolute-percentage error
	// observed when we replay the same model over the most recent history.
	// Drives the confidence score directly so confidence is empirical rather
	// than heuristic. Reported as a fraction (0.04 = 4%).
	MAPE float64
	// NaiveMAPE is the same metric for the naive "no-change" forecast
	// (yhat = current price). Used to compute the model's skill score.
	NaiveMAPE float64
	// Skill is the relative improvement of the damped-Holt model over naive,
	// expressed as 1 - MAPE/NaiveMAPE. Positive = model beats naive,
	// negative = model is worse than naive. Bounded loosely in [-1, 1].
	Skill         float64
	BacktestSteps int
	// Phi is the trend damping factor selected by the grid search. Values
	// below 1.0 mean the trend dampens out over the horizon — a classic
	// tweak to Holt that sharply improves accuracy for h > 1.
	Phi float64
}

// Forecast produces a point forecast `horizonDays` steps ahead from `closes`,
// along with technical indicator readings and an empirically-derived
// confidence score.
//
// Model: damped Holt smoothing (Gardner & McKenzie 1985) chosen via grid
// search over (alpha, beta, phi) minimising in-sample SSE. The damped trend
// is well-known to outperform linear Holt on multi-step horizons because it
// prevents the trend from extrapolating indefinitely.
//
// Confidence: derived from a rolling-origin out-of-sample backtest. We replay
// the same damped-Holt fit across the most recent ~30 trading days and measure
// the actual h-step-ahead MAPE; that empirical error — combined with how well
// the trend agrees with the technical indicators — produces the score.
func Forecast(closes []float64, horizonDays int) (ForecastResult, error) {
	if horizonDays <= 0 {
		return ForecastResult{}, fmt.Errorf("horizonDays must be > 0")
	}
	if len(closes) < 30 {
		return ForecastResult{}, fmt.Errorf("need at least 30 closes, got %d", len(closes))
	}

	current := closes[len(closes)-1]

	// Front-month futures series have periodic contract-roll discontinuities
	// (a 5-15% gap on a single day when the active contract switches). Those
	// are data artifacts, not real moves a statistical model could ever
	// predict, so we back-adjust the history before fitting. The most recent
	// value is preserved — only earlier bars shift to make the series
	// continuous through every detected roll.
	closes = backAdjustRolls(closes, 0.07)

	level, trend, _, _, phi, residStd := fitHoltDamped(closes)
	predicted := dampedHoltForecast(level, trend, phi, horizonDays)

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

	// Out-of-sample backtest. We use up to 30 rolling-origin steps; on shorter
	// histories we use whatever we can get without dipping below the model's
	// minimum training size.
	backtestSteps := 30
	if budget := len(closes) - 60 - horizonDays; budget < backtestSteps {
		backtestSteps = budget
	}
	if backtestSteps < 5 {
		backtestSteps = 5
	}
	mape, naiveMape, n := backtestMAPEWithBaseline(closes, horizonDays, backtestSteps)

	skill := 0.0
	if naiveMape > 0 {
		skill = 1.0 - mape/naiveMape
	}

	// If the model is materially worse than naive, fall back to the naive
	// forecast (predict no change). It would be dishonest to publish a
	// confident-looking arrow when the model can't beat "tomorrow looks
	// like today" on its own backtest.
	if n >= 10 && mape > 0 && naiveMape > 0 && mape > naiveMape*1.05 {
		predicted = current
		direction = "neutral"
	}

	// Prediction interval: prefer the empirical out-of-sample error scaled
	// to the current price; fall back to in-sample residual std if the
	// backtest didn't run for some reason. 1.28 ≈ 80% one-sided z.
	band := 1.28 * residStd * math.Sqrt(float64(horizonDays))
	if mape > 0 && n >= 5 {
		band = 1.28 * mape * current
	}
	low := predicted - band
	high := predicted + band

	confidence := computeConfidence(mape, naiveMape, n, rsi, hist, trendLabel, direction)

	return ForecastResult{
		Current:       current,
		Predicted:     predicted,
		Low:           low,
		High:          high,
		Direction:     direction,
		Confidence:    confidence,
		RSI14:         rsi,
		MACD:          macd,
		MACDSignal:    signal,
		MACDHist:      hist,
		MA50:          ma50,
		MA200:         ma200,
		Trend:         trendLabel,
		HorizonDays:   horizonDays,
		Samples:       len(closes),
		MAPE:          mape,
		NaiveMAPE:     naiveMape,
		Skill:         skill,
		BacktestSteps: n,
		Phi:           phi,
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
	macdTail := macdLine[slow-1:]
	signalTail := EMA(macdTail, signalPeriod)
	macd := macdLine[len(macdLine)-1]
	signal := signalTail[len(signalTail)-1]
	return macd, signal, macd - signal
}

// fitHoltLinear is retained for tests and as a documented baseline. New code
// should use fitHoltDamped, which subsumes it (damped reduces to linear when
// phi == 1.0) and consistently outperforms it for multi-step horizons.
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
	residStd = math.Sqrt(bestSSE / float64(len(values)-2))
	return
}

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

// fitHoltDamped runs a coarse grid search over (alpha, beta, phi) and returns
// the smoother's final state plus the in-sample residual standard deviation.
//
// The damped form (Gardner & McKenzie 1985) replaces the linear trend with a
// trend that decays geometrically by phi each step, so multi-step forecasts
// asymptote rather than runaway. Including phi=1.0 in the grid keeps the
// linear case available when it actually fits best.
func fitHoltDamped(values []float64) (level, trend, bestAlpha, bestBeta, bestPhi, residStd float64) {
	bestSSE := math.Inf(1)
	for _, a := range []float64{0.1, 0.2, 0.3, 0.5, 0.7, 0.9} {
		for _, b := range []float64{0.05, 0.1, 0.2, 0.3} {
			for _, phi := range []float64{0.85, 0.9, 0.95, 0.98, 1.0} {
				l, t, sse := holtDampedFit(values, a, b, phi)
				if sse < bestSSE {
					bestSSE = sse
					level, trend = l, t
					bestAlpha, bestBeta, bestPhi = a, b, phi
				}
			}
		}
	}
	residStd = math.Sqrt(bestSSE / float64(len(values)-2))
	return
}

func holtDampedFit(values []float64, alpha, beta, phi float64) (level, trend, sse float64) {
	if len(values) < 2 {
		return values[0], 0, 0
	}
	level = values[0]
	trend = values[1] - values[0]
	for i := 1; i < len(values); i++ {
		fcast := level + phi*trend
		err := values[i] - fcast
		sse += err * err
		newLevel := alpha*values[i] + (1-alpha)*(level+phi*trend)
		newTrend := beta*(newLevel-level) + (1-beta)*phi*trend
		level = newLevel
		trend = newTrend
	}
	return
}

// dampedHoltForecast extrapolates h steps ahead using the damped trend
// closed form: y_{t+h} = level + (sum_{i=1..h} phi^i) * trend.
func dampedHoltForecast(level, trend, phi float64, h int) float64 {
	if h <= 0 {
		return level
	}
	var dampSum float64
	if math.Abs(phi-1) < 1e-9 {
		dampSum = float64(h)
	} else {
		// Geometric series: phi + phi^2 + ... + phi^h = phi*(1-phi^h)/(1-phi).
		dampSum = phi * (1 - math.Pow(phi, float64(h))) / (1 - phi)
	}
	return level + dampSum*trend
}

// backtestMAPEWithBaseline walks a rolling origin across the tail of the
// series and returns (modelMAPE, naiveMAPE, steps). At each step we make a
// h-step-ahead forecast with damped Holt and a parallel h-step "no change"
// forecast; both are scored against the same actual value.
//
// The naive MAPE is the bar a useful model has to clear — if our model
// can't beat "tomorrow looks like today" out of sample, we shouldn't be
// claiming any confidence in its directional pick.
//
// Returns (modelMAPE, naiveMAPE, steps). MAPEs are fractions (0.04 = 4%).
func backtestMAPEWithBaseline(closes []float64, horizon, steps int) (float64, float64, int) {
	if steps < 1 || horizon < 1 {
		return 0, 0, 0
	}
	minTrain := 60
	if len(closes) < minTrain+horizon+steps {
		steps = len(closes) - minTrain - horizon
		if steps < 1 {
			return 0, 0, 0
		}
	}

	totalModel := 0.0
	totalNaive := 0.0
	n := 0
	startOrigin := len(closes) - steps - horizon
	for i := 0; i < steps; i++ {
		origin := startOrigin + i
		train := closes[:origin+1]
		level, trend, _, _, phi, _ := fitHoltDamped(train)
		yhat := dampedHoltForecast(level, trend, phi, horizon)
		actual := closes[origin+horizon]
		if actual == 0 {
			continue
		}
		totalModel += math.Abs(yhat-actual) / math.Abs(actual)
		totalNaive += math.Abs(closes[origin]-actual) / math.Abs(actual)
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	return totalModel / float64(n), totalNaive / float64(n), n
}

// backAdjustRolls returns a copy of `values` with futures contract-roll
// discontinuities removed by back-adjusting earlier bars. A "roll" is any
// single-bar percentage move whose magnitude exceeds `threshold` (e.g. 0.07
// = 7%). When a roll is detected at index i, every value at indices < i is
// multiplied by closes[i] / closes[i-1] so the gap closes seamlessly. The
// most recent value is preserved verbatim.
//
// This produces a back-adjusted continuous series of the kind futures data
// providers like CSI/Quandl publish, which is what statistical time-series
// models actually want to consume. Without this, fitting and backtesting
// against the raw front-month series wildly overstates forecast error
// because the model is "wrong" on every roll day.
func backAdjustRolls(values []float64, threshold float64) []float64 {
	if len(values) < 2 {
		out := make([]float64, len(values))
		copy(out, values)
		return out
	}
	out := make([]float64, len(values))
	copy(out, values)
	// Walk forward; whenever we detect a roll between i-1 and i, multiply
	// every prior bar by the gap ratio so the series becomes continuous
	// through that point.
	for i := 1; i < len(out); i++ {
		if out[i-1] == 0 {
			continue
		}
		ratio := out[i] / out[i-1]
		// |ratio - 1| > threshold marks a roll-sized gap.
		if math.Abs(ratio-1) > threshold {
			for j := 0; j < i; j++ {
				out[j] *= ratio
			}
		}
	}
	return out
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

// computeConfidence maps the empirical backtest performance and the
// agreement between the trend and the technical indicators into a 0..1
// score.
//
// Confidence is dominated by two empirical inputs:
//
//  1. Absolute fit (MAPE) — how wrong was the model in absolute terms?
//     For 7-day oil forecasts, ~3% MAPE is excellent and ~10% is poor.
//
//  2. Skill vs naive (Skill = 1 - MAPE/NaiveMAPE) — does the model add any
//     information over a "no change" baseline? Negative skill means the
//     model is actively hurting; we floor it at zero in the score so a
//     bad model can't be rescued by tight noise.
//
// Indicator agreement adds a small bonus on top, capped at +0.15. Neutral
// directions are capped harder since the model is implicitly saying "I
// don't know".
func computeConfidence(mape, naiveMape float64, samples int, rsi, macdHist float64, trend, direction string) float64 {
	if samples < 5 || mape <= 0 {
		return 0.40
	}

	// Absolute fit term, calibrated to realistic 7-day oil-forecast MAPE.
	// 2.5% → 1.0 (excellent); 10% → 0.0 (poor).
	const goodMAPE = 0.025
	const badMAPE = 0.10
	fit := 1.0 - (mape-goodMAPE)/(badMAPE-goodMAPE)
	fit = clamp(fit, 0, 1)

	// Skill term: 1 - mape/naiveMape, floored at 0. A model that beats naive
	// by 30% gets a big bump; a model worse than naive contributes nothing.
	skill := 0.0
	if naiveMape > 0 {
		skill = 1.0 - mape/naiveMape
	}
	skill = clamp(skill, 0, 1)

	// Indicator agreement, normalised to [0, 1].
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
		// Neutral direction: confidence reflects fit only, capped at 0.65.
		return clamp(0.30+0.30*fit, 0.25, 0.65)
	}
	agreeFrac := float64(agree) / float64(total)

	// 50% absolute fit + 25% skill-vs-naive + 15% indicator agreement
	// + 10% floor so a confident-but-disagreeing model still reads as
	// moderate. Final clamp keeps anything from claiming absolute certainty.
	c := 0.10 + 0.50*fit + 0.25*skill + 0.15*agreeFrac
	return clamp(c, 0.25, 0.95)
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

// LabelRSI maps an RSI(14) value into the classic short, plain-English buckets
// the UI uses for its signal-stack chips. Boundaries mirror the standard
// 30/45/55/70 momentum thresholds.
func LabelRSI(rsi float64) string {
	switch {
	case rsi >= 70:
		return "overbought"
	case rsi >= 55:
		return "bullish"
	case rsi <= 30:
		return "oversold"
	case rsi <= 45:
		return "bearish"
	default:
		return "neutral"
	}
}

// LabelMACD turns the MACD histogram into a directional bias chip. We use the
// histogram (macd - signal) rather than the raw MACD line because it's the
// usual cross-confirmation signal traders look at first.
func LabelMACD(hist float64) string {
	switch {
	case hist > 0.05:
		return "above signal (bullish)"
	case hist < -0.05:
		return "below signal (bearish)"
	default:
		return "flat"
	}
}

// LabelMAConfig describes the 50DMA vs 200DMA configuration in plain words.
// "Golden cross" / "death cross" language is intentional — these are the
// terms traders actually use, and they're great for SEO.
func LabelMAConfig(ma50, ma200 float64) string {
	if ma50 <= 0 || ma200 <= 0 {
		return ""
	}
	gap := (ma50 - ma200) / ma200
	switch {
	case gap > 0.02:
		return "50DMA well above 200DMA (golden-cross regime)"
	case gap > 0.005:
		return "50DMA above 200DMA (bullish bias)"
	case gap < -0.02:
		return "50DMA well below 200DMA (death-cross regime)"
	case gap < -0.005:
		return "50DMA below 200DMA (bearish bias)"
	default:
		return "50DMA and 200DMA flat (no trend regime)"
	}
}
