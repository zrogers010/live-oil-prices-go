package services

import (
	"fmt"
	"hash/fnv"
	"io"
	"live-oil-prices-go/internal/models"
	"math"
	"math/rand"
	"sync"
	"time"
)

type MarketDataService struct {
	rng        *rand.Rand
	basePrices map[string]float64
	yahoo      *YahooFinanceService
	pyth       *PythService
	eia        *EIAService

	// Predictions are computed with a damped-Holt fit + 30-step rolling-origin
	// backtest per symbol, which is heavy enough that we don't want to do it
	// on every /api/predictions hit or every page render. Cached for predictionTTL.
	predictionsMu     sync.RWMutex
	cachedPredictions []models.Prediction
	cachedPredAt      time.Time
}

// predictionTTL bounds how stale GetPredictions can be. The underlying
// daily-history input only refreshes hourly, so 60s is plenty fresh while
// still keeping the model off the critical request path.
const predictionTTL = 60 * time.Second

func NewMarketDataService() *MarketDataService {
	bases := map[string]float64{
		"WTI":     72.45,
		"BRENT":   76.82,
		"NATGAS":  3.24,
		"HEATING": 2.35,
		"RBOB":    2.18,
		"OPEC":    74.50,
		"DUBAI":   75.10,
		"MURBAN":  76.30,
		"WCS":     58.20,
		"GASOIL":  685.50,
	}
	return &MarketDataService{
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		basePrices: bases,
		yahoo:      NewYahooFinanceService(),
		pyth:       NewPythService(),
		eia:        NewEIAService(),
	}
}

// GetConsensusForecasts returns the institutional outlook (EIA STEO) for
// every benchmark we publish. Returns an empty slice when EIA_API_KEY isn't
// configured — the UI hides the section gracefully in that case.
func (s *MarketDataService) GetConsensusForecasts() []models.ConsensusForecast {
	if s.eia == nil {
		return nil
	}
	return s.eia.GetAll()
}

// GetConsensusForecast returns a single institutional outlook by symbol.
func (s *MarketDataService) GetConsensusForecast(symbol string) (models.ConsensusForecast, bool) {
	if s.eia == nil {
		return models.ConsensusForecast{}, false
	}
	return s.eia.Get(symbol)
}

var commodityNames = map[string]string{
	"WTI": "WTI Crude Oil", "BRENT": "Brent Crude Oil",
	"NATGAS": "Natural Gas", "HEATING": "Heating Oil",
	"RBOB": "RBOB Gasoline", "OPEC": "OPEC Basket",
	"DUBAI": "Dubai Crude", "MURBAN": "Murban Crude",
	"WCS": "Western Canadian Select", "GASOIL": "ICE Gasoil",
}

var allCommodities = []struct {
	symbol string
	name   string
}{
	{"WTI", "WTI Crude Oil"},
	{"BRENT", "Brent Crude Oil"},
	{"NATGAS", "Natural Gas"},
	{"HEATING", "Heating Oil"},
	{"RBOB", "RBOB Gasoline"},
	{"OPEC", "OPEC Basket"},
	{"DUBAI", "Dubai Crude"},
	{"MURBAN", "Murban Crude"},
	{"WCS", "Western Canadian Select"},
	{"GASOIL", "ICE Gasoil"},
}

func (s *MarketDataService) GetPrices() []models.Price {
	var yahooData map[string]models.Price
	if s.yahoo != nil {
		yahooData = s.yahoo.GetPrices()
	}
	var pythData map[string]PythQuote
	if s.pyth != nil {
		pythData = s.pyth.GetQuotes()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	prices := make([]models.Price, len(allCommodities))

	for i, c := range allCommodities {
		yp, hasYahoo := yahooData[c.symbol]
		pq, hasPyth := pythData[c.symbol]

		switch {
		case hasPyth && hasYahoo:
			// Real-time Pyth tick over Yahoo daily metadata.
			prices[i] = applyPyth(yp, pq)
			continue
		case hasPyth:
			// Pyth-only: surface the live tick, no daily baseline.
			prices[i] = applyPyth(models.Price{Symbol: c.symbol, Name: c.name}, pq)
			continue
		case hasYahoo:
			prices[i] = yp
			continue
		}
		// Synthetic fallback for commodities without a Yahoo Finance ticker
		base := s.basePrices[c.symbol]
		volatility := base * 0.008
		change := (s.rng.Float64() - 0.45) * volatility
		price := base + change
		changePct := (change / base) * 100
		dayVolatility := base * 0.02
		high := price + s.rng.Float64()*dayVolatility
		low := price - s.rng.Float64()*dayVolatility
		volume := int64(500000 + s.rng.Intn(2000000))
		prices[i] = models.Price{
			Symbol:    c.symbol,
			Name:      c.name,
			Price:     math.Round(price*100) / 100,
			Change:    math.Round(change*100) / 100,
			ChangePct: math.Round(changePct*100) / 100,
			High:      math.Round(high*100) / 100,
			Low:       math.Round(low*100) / 100,
			Volume:    volume,
			UpdatedAt: now,
			Source:    "estimate",
		}
	}
	return prices
}

func (s *MarketDataService) GetChartData(symbol string, days int, interval string) models.ChartData {
	base, ok := s.basePrices[symbol]
	if !ok {
		base = 72.0
	}

	if s.yahoo != nil {
		if yp, ok := s.yahoo.GetPrices()[symbol]; ok {
			base = yp.Price
		}
	}

	name := symbol
	if n, ok := commodityNames[symbol]; ok {
		name = n
	}

	if interval == "" {
		switch {
		case days <= 7:
			interval = "2h"
		case days <= 30:
			interval = "4h"
		default:
			interval = "1d"
		}
	}

	// Prefer REAL cached Yahoo OHLCV daily history when available. This
	// is what the user sees on /charts and we want it to be actual market
	// history, not a randomly regenerated series. If the cache hasn't
	// loaded yet (cold start) or this symbol isn't tracked by Yahoo
	// (estimates: OPEC, DUBAI, etc.), we fall through to the synthetic
	// generator below.
	if interval == "1d" && s.yahoo != nil {
		if bars := s.yahoo.GetDailyHistory(symbol, days); len(bars) > 0 {
			return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: bars}
		}
	}

	// Synthetic fallback. We seed a per-call RNG with a hash of
	// (symbol, days, today's UTC date) so flipping back and forth between
	// tabs returns the SAME chart instead of a freshly randomised series.
	// The series naturally rolls over once a day when the seed changes.
	rng := rand.New(rand.NewSource(syntheticChartSeed(symbol, days, interval)))

	var data []models.OHLCV
	switch interval {
	case "2h":
		data = s.generateIntraday(rng, base, days, 2)
	case "4h":
		data = s.generateIntraday(rng, base, days, 4)
	default:
		data = s.generateDaily(rng, base, days)
	}

	return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: data}
}

// syntheticChartSeed produces a stable per-day seed for the synthetic
// chart generator. Two requests for the same (symbol, days, interval) on
// the same UTC calendar day return the same RNG sequence and therefore
// the same chart. Across days the seed shifts so the chart "ages
// forward" naturally.
func syntheticChartSeed(symbol string, days int, interval string) int64 {
	h := fnv.New64a()
	io.WriteString(h, symbol)
	io.WriteString(h, "|")
	io.WriteString(h, interval)
	io.WriteString(h, "|")
	fmt.Fprintf(h, "%d|", days)
	io.WriteString(h, time.Now().UTC().Format("2006-01-02"))
	// fnv64 is unsigned; cast preserves all bits for use as an int64 seed.
	return int64(h.Sum64())
}

func (s *MarketDataService) generateDaily(rng *rand.Rand, base float64, days int) []models.OHLCV {
	allData := make([]models.OHLCV, 0, days+50)
	price := base - (base * 0.05)
	calendarDays := int(float64(days)*1.5) + 20
	startTime := time.Now().AddDate(0, 0, -calendarDays)
	now := time.Now()

	for i := 0; i <= calendarDays; i++ {
		t := startTime.AddDate(0, 0, i)
		if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			continue
		}
		if t.After(now) {
			break
		}
		vol := price * 0.015
		change := (rng.Float64() - 0.47) * vol
		price += change
		open := price
		cl := open + (rng.Float64()-0.5)*vol
		high := math.Max(open, cl) + rng.Float64()*vol*0.5
		low := math.Min(open, cl) - rng.Float64()*vol*0.5
		v := int64(800000 + rng.Intn(1500000))
		allData = append(allData, models.OHLCV{
			Time: t.Unix(), Open: r2(open), High: r2(high), Low: r2(low), Close: r2(cl), Volume: v,
		})
		price = cl
	}
	if len(allData) > days {
		return allData[len(allData)-days:]
	}
	return allData
}

func (s *MarketDataService) generateIntraday(rng *rand.Rand, base float64, days int, hoursPerCandle int) []models.OHLCV {
	candlesPerDay := 24 / hoursPerCandle
	data := make([]models.OHLCV, 0, days*candlesPerDay)
	price := base - (base * 0.03)
	calendarDays := int(float64(days)*1.5) + 5
	startTime := time.Now().AddDate(0, 0, -calendarDays)
	now := time.Now()

	for d := 0; d <= calendarDays; d++ {
		dayStart := startTime.AddDate(0, 0, d)
		if dayStart.Weekday() == time.Saturday || dayStart.Weekday() == time.Sunday {
			continue
		}
		dayStart = time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, time.UTC)

		for c := 0; c < candlesPerDay; c++ {
			t := dayStart.Add(time.Duration(c*hoursPerCandle) * time.Hour)
			if t.After(now) {
				break
			}
			vol := price * 0.004
			change := (rng.Float64() - 0.48) * vol
			price += change
			open := price
			cl := open + (rng.Float64()-0.5)*vol
			high := math.Max(open, cl) + rng.Float64()*vol*0.3
			low := math.Min(open, cl) - rng.Float64()*vol*0.3
			v := int64(50000 + rng.Intn(200000))
			data = append(data, models.OHLCV{
				Time: t.Unix(), Open: r2(open), High: r2(high), Low: r2(low), Close: r2(cl), Volume: v,
			})
			price = cl
		}
	}
	targetCount := days * candlesPerDay
	if len(data) > targetCount {
		return data[len(data)-targetCount:]
	}
	return data
}

func r2(v float64) float64 { return math.Round(v*100) / 100 }

// predictionSymbols defines which symbols we publish a forecast for, and the
// horizon used. We only forecast symbols backed by real Yahoo history; falling
// back to a flat "no signal" prediction if history is not yet loaded.
var predictionSymbols = []struct {
	symbol  string
	name    string
	horizon int
}{
	{"WTI", "WTI Crude Oil", 7},
	{"BRENT", "Brent Crude Oil", 7},
	{"NATGAS", "Natural Gas", 7},
	{"HEATING", "Heating Oil", 7},
}

const predictionDisclaimer = "Statistical forecast for informational purposes only. Not investment advice."

func (s *MarketDataService) GetPredictions() []models.Prediction {
	// Fast path: serve the cached slice if it's fresh.
	s.predictionsMu.RLock()
	if time.Since(s.cachedPredAt) < predictionTTL && len(s.cachedPredictions) > 0 {
		out := make([]models.Prediction, len(s.cachedPredictions))
		copy(out, s.cachedPredictions)
		s.predictionsMu.RUnlock()
		return out
	}
	s.predictionsMu.RUnlock()

	out := s.computePredictions()

	s.predictionsMu.Lock()
	s.cachedPredictions = out
	s.cachedPredAt = time.Now()
	s.predictionsMu.Unlock()

	return out
}

// computePredictions runs the damped-Holt + backtest pipeline for each
// configured symbol. Called by GetPredictions when the cache is stale.
func (s *MarketDataService) computePredictions() []models.Prediction {
	prices := s.GetPrices()
	pm := make(map[string]float64)
	sources := make(map[string]string)
	for _, p := range prices {
		pm[p.Symbol] = p.Price
		sources[p.Symbol] = p.Source
	}

	out := make([]models.Prediction, 0, len(predictionSymbols))
	for _, ps := range predictionSymbols {
		current := pm[ps.symbol]
		var history []float64
		if s.yahoo != nil {
			history = s.yahoo.GetHistory(ps.symbol)
		}

		if len(history) < 30 {
			out = append(out, fallbackPrediction(ps.symbol, ps.name, current, ps.horizon))
			continue
		}

		// Splice the live current price as the most recent close so the forecast
		// reflects intraday movement, not just the last completed daily bar.
		closes := history
		if current > 0 {
			closes = append(append([]float64{}, history...), current)
		}

		f, err := Forecast(closes, ps.horizon)
		if err != nil {
			out = append(out, fallbackPrediction(ps.symbol, ps.name, current, ps.horizon))
			continue
		}

		predSource := sources[ps.symbol]
		if predSource == "" {
			predSource = "yahoo"
		}
		out = append(out, models.Prediction{
			Symbol:        ps.symbol,
			Name:          ps.name,
			Current:       r2(f.Current),
			Predicted:     r2(f.Predicted),
			PredictedLow:  r2(f.Low),
			PredictedHigh: r2(f.High),
			Timeframe:     fmt.Sprintf("%d days", ps.horizon),
			Confidence:    math.Round(f.Confidence*100) / 100,
			Direction:     f.Direction,
			Analysis:      buildAnalysis(ps.name, f),
			Model:         "holt-damped+backtest+rsi/macd",
			Source:        predSource,
			Disclaimer:    predictionDisclaimer,

			TrendLabel: f.Trend,
			RSI14:      math.Round(f.RSI14*10) / 10,
			RSILabel:   LabelRSI(f.RSI14),
			MACDHist:   math.Round(f.MACDHist*1000) / 1000,
			MACDLabel:  LabelMACD(f.MACDHist),
			MAConfig:   LabelMAConfig(f.MA50, f.MA200),

			MAPE:          math.Round(f.MAPE*10000) / 10000,
			NaiveMAPE:     math.Round(f.NaiveMAPE*10000) / 10000,
			Skill:         math.Round(f.Skill*1000) / 1000,
			BacktestSteps: f.BacktestSteps,
		})
	}
	return out
}

// fallbackPrediction is returned when we don't yet have enough history loaded
// (e.g. cold start, or Yahoo unreachable). The "predicted" value mirrors the
// current price so the UI doesn't show a misleading move.
func fallbackPrediction(symbol, name string, current float64, horizon int) models.Prediction {
	return models.Prediction{
		Symbol:     symbol,
		Name:       name,
		Current:    r2(current),
		Predicted:  r2(current),
		Timeframe:  fmt.Sprintf("%d days", horizon),
		Confidence: 0.0,
		Direction:  "neutral",
		Analysis:   "Forecast unavailable — insufficient historical data loaded yet. Please retry shortly.",
		Model:      "fallback",
		Source:     "estimate",
		Disclaimer: predictionDisclaimer,
	}
}

// buildAnalysis composes a short, fact-based commentary from the indicator
// readings. Kept deliberately simple and quantitative; no fabricated narrative.
func buildAnalysis(name string, f ForecastResult) string {
	delta := f.Predicted - f.Current
	pct := 0.0
	if f.Current != 0 {
		pct = (delta / f.Current) * 100
	}

	rsiLabel := "neutral"
	switch {
	case f.RSI14 >= 70:
		rsiLabel = "overbought"
	case f.RSI14 >= 55:
		rsiLabel = "bullish"
	case f.RSI14 <= 30:
		rsiLabel = "oversold"
	case f.RSI14 <= 45:
		rsiLabel = "bearish"
	}

	macdLabel := "flat"
	switch {
	case f.MACDHist > 0:
		macdLabel = "above signal (bullish)"
	case f.MACDHist < 0:
		macdLabel = "below signal (bearish)"
	}

	bandPct := 0.0
	if f.Current != 0 {
		bandPct = ((f.High - f.Low) / 2 / f.Current) * 100
	}

	trendArticle := "a"
	if f.Trend == "uptrend" {
		trendArticle = "an"
	}

	mapeStr := ""
	if f.MAPE > 0 && f.BacktestSteps > 0 {
		skillStr := ""
		if f.NaiveMAPE > 0 {
			switch {
			case f.Skill > 0.10:
				skillStr = fmt.Sprintf(" — model beats naive baseline by %.0f%%", f.Skill*100)
			case f.Skill < -0.05:
				skillStr = fmt.Sprintf(" — model underperforms naive baseline by %.0f%%", -f.Skill*100)
			default:
				skillStr = " — model essentially matches naive baseline"
			}
		}
		mapeStr = fmt.Sprintf(" Recent %d-step backtest: %.1f%% MAPE on %d-day-ahead forecasts%s.",
			f.BacktestSteps, f.MAPE*100, f.HorizonDays, skillStr)
	}
	dampStr := ""
	if f.Phi > 0 && f.Phi < 1 {
		dampStr = fmt.Sprintf(" (damping φ=%.2f)", f.Phi)
	}

	return fmt.Sprintf(
		"%s: %d-day damped Holt forecast%s points to $%.2f (%+.2f%% from $%.2f), with an 80%% interval of $%.2f–$%.2f (±%.1f%%). "+
			"RSI(14) is %.1f (%s), MACD histogram is %s, and the 50/200-day MA configuration suggests %s %s.%s "+
			"Confidence: %.0f%%.",
		name,
		f.HorizonDays,
		dampStr,
		f.Predicted, pct, f.Current,
		f.Low, f.High, bandPct,
		f.RSI14, rsiLabel,
		macdLabel,
		trendArticle, f.Trend,
		mapeStr,
		f.Confidence*100,
	)
}

// GetPythCandles surfaces the streaming 1-minute candle buffer for a single
// symbol so the homepage hero chart can render true real-time bars. Returns
// nil if Pyth is unavailable or hasn't accumulated any ticks yet.
func (s *MarketDataService) GetPythCandles(symbol string, max int) []models.PythCandle {
	if s.pyth == nil {
		return nil
	}
	return s.pyth.GetCandles(symbol, max)
}

// pythLiveWindow defines how recent the latest Pyth tick must be for us to
// treat the feed as actively streaming. Outside this window the underlying
// market is paused (weekend/holiday/maintenance/CME daily break) and we
// should fall back to Yahoo's intraday series so the user doesn't see a
// stale "LIVE" pill on a chart that hasn't moved.
//
// 90 seconds is the sweet spot: Pyth's WTI publishers normally print every
// ~400ms during market hours, so even a several-cycle hiccup stays inside
// the window, but the daily CME maintenance break (5–6 PM ET) and the
// weekend pause flip us to Yahoo within ~1.5 minutes instead of dragging
// a fake "LIVE" indicator along for 5 minutes.
const pythLiveWindow = 90 * time.Second

// heroBucketSec is the resolution of the hero chart's bars. We render the
// homepage hero at 5-minute granularity because (a) it matches Yahoo's
// intraday endpoint that backfills the rest of the trading day, and
// (b) at 5m a full ~23-hour NY session fits in ~280 bars — readable
// without scrolling at typical screen widths.
const heroBucketSec int64 = 300

// GetHeroChart returns the homepage hero chart payload covering the
// rolling last 24 hours. The shape of the response is one of:
//
//   - mode="live": Yahoo's 5-min intraday bars from 24h ago through the
//     most recent completed bucket, with an additional in-progress bar
//     appended (or the latest bucket overwritten) using Pyth's streaming
//     ticks. Steady state while the futures market is open and Pyth is
//     publishing.
//
//   - mode="today-paused": Same rolling 24h Yahoo bars as live mode, but
//     Pyth has gone quiet (typically the daily 5–6 PM ET CME maintenance
//     break, or a brief publisher hiccup). The chart still shows the
//     same data window; we just don't pulse the LIVE indicator. The
//     CME break shows up naturally as a 1-hour gap in the bars.
//
//   - mode="prior-session": Yahoo has no recent bars (full weekend, cold
//     start before first refresh) so we serve the most recent complete
//     prior session as a stand-in. SessionDate labels which day.
//
//   - mode="warming-up": Cold start — neither Yahoo nor Pyth have any
//     data yet. Frontend renders a placeholder.
//
// We use rolling-24h rather than "today's calendar day" so the chart is
// always populated regardless of viewing time — at 00:13 ET there'd be
// only 3 bars under a strict-today rule, which is a poor UX. The
// vertical session-boundary markers the frontend overlays make the
// trading-hours structure obvious anyway.
//
// `maxLiveBars` is accepted for backwards compatibility with the API
// signature and ignored — the hero always returns the full 24h window.
func (s *MarketDataService) GetHeroChart(symbol string, _ int) models.HeroChart {
	out := models.HeroChart{Symbol: symbol, Bars: []models.PythCandle{}}

	pythLive := false
	var pythPublishedAt time.Time
	if s.pyth != nil {
		if q, ok := s.pyth.GetQuote(symbol); ok && time.Since(q.PublishedAt) <= pythLiveWindow {
			pythLive = true
			pythPublishedAt = q.PublishedAt
		}
	}

	// 1) Rolling 24h of Yahoo intraday — the primary hero data source.
	if s.yahoo != nil {
		bars, interval := s.yahoo.GetRolling24hIntraday(symbol)
		if len(bars) > 0 {
			out.Source = "yahoo"
			out.Interval = interval
			// SessionDate carries today's NY-local date so the frontend
			// can compute the session-boundary markers (17:00 / 18:00 ET
			// transitions) without having to repeat the timezone math.
			out.SessionDate = nyTodayDate()
			out.Bars = ohlcvToCandles(bars)

			// Splice in a live in-progress 5-minute bar from Pyth ticks
			// when the feed is fresh — Yahoo only refreshes every ~5 min
			// server-side, so without this overlay the rightmost bar
			// would visibly lag the spot price by up to 5 minutes.
			if pythLive && s.pyth != nil {
				bucketStart := pythPublishedAt.Truncate(time.Duration(heroBucketSec) * time.Second).Unix()
				if bar, ok := s.pyth.GetBucketBar(symbol, bucketStart, heroBucketSec); ok {
					out.Bars = mergeLiveBucket(out.Bars, bar)
				}
			}

			if pythLive {
				out.Mode = "live"
				out.UpdatedAt = pythPublishedAt.Format(time.RFC3339)
			} else {
				out.Mode = "today-paused"
				last := out.Bars[len(out.Bars)-1]
				out.UpdatedAt = time.Unix(last.Time, 0).UTC().Format(time.RFC3339)
			}
			return out
		}
	}

	// 2) No recent Yahoo bars (weekend, cold-start before first refresh).
	// Fall back to the most recent complete prior session.
	if s.yahoo != nil {
		bars, sessionDate, interval := s.yahoo.GetPriorSessionIntraday(symbol)
		if len(bars) > 0 {
			out.Mode = "prior-session"
			out.Interval = interval
			out.Source = "yahoo"
			out.SessionDate = sessionDate
			out.Bars = ohlcvToCandles(bars)
			last := bars[len(bars)-1]
			out.UpdatedAt = time.Unix(last.Time, 0).UTC().Format(time.RFC3339)
			return out
		}
	}

	// 3) Last resort — show whatever Pyth has accumulated since boot,
	// even if Yahoo is completely cold. Common right after server
	// startup before the first Yahoo intraday refresh has landed.
	if s.pyth != nil {
		bars := s.pyth.GetCandles(symbol, 0)
		if len(bars) > 0 {
			out.Source = "pyth"
			out.Interval = "1m"
			out.Bars = bars
			out.SessionDate = nyTodayDate()
			if pythLive {
				out.Mode = "live"
				out.UpdatedAt = pythPublishedAt.Format(time.RFC3339)
			} else {
				out.Mode = "today-paused"
				out.UpdatedAt = time.Unix(bars[len(bars)-1].Time, 0).UTC().Format(time.RFC3339)
			}
			return out
		}
	}

	// 4) Nothing — frontend renders the cold-start placeholder.
	out.Mode = "warming-up"
	return out
}

// nyTodayDate returns today's NY-local date as YYYY-MM-DD. Mirrors the
// helper in yahoo.go but lives here too so market_data.go has no cross-
// service-internal dependency.
func nyTodayDate() string {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.Now().UTC().Format("2006-01-02")
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// ohlcvToCandles reshapes Yahoo's OHLCV bars (which carry a volume field
// the hero chart doesn't need) into the lighter PythCandle shape so the
// frontend has a single bar type to render across all hero modes.
func ohlcvToCandles(in []models.OHLCV) []models.PythCandle {
	out := make([]models.PythCandle, len(in))
	for i, b := range in {
		out[i] = models.PythCandle{
			Time:  b.Time,
			Open:  b.Open,
			High:  b.High,
			Low:   b.Low,
			Close: b.Close,
		}
	}
	return out
}

// mergeLiveBucket folds a freshly-built Pyth in-progress bar into the
// session series. If the latest series bar already covers the same bucket
// (Yahoo refreshed midway through a 5-min window), we overwrite it so the
// chart shows the more recent Pyth-derived close. Otherwise we append the
// new bucket to the right of the series.
//
// We deliberately don't try to "merge" overlapping ranges from both
// sources — Pyth's tick range is the more current truth inside the live
// bucket, and Yahoo will replace it on its next intraday refresh anyway.
func mergeLiveBucket(series []models.PythCandle, live models.PythCandle) []models.PythCandle {
	if len(series) == 0 {
		return []models.PythCandle{live}
	}
	last := series[len(series)-1]
	if last.Time == live.Time {
		series[len(series)-1] = live
		return series
	}
	if live.Time > last.Time {
		return append(series, live)
	}
	// Live bucket is older than the last Yahoo bar — Yahoo is ahead of
	// Pyth (rare, only happens right after a Yahoo refresh lands a future
	// timestamp). Trust Yahoo, drop the stale Pyth bucket.
	return series
}

func (s *MarketDataService) GetAnalysis() models.MarketAnalysis {
	now := time.Now().UTC().Format(time.RFC3339)
	wtiPrice := s.basePrices["WTI"]
	if s.yahoo != nil {
		if yp, ok := s.yahoo.GetPrices()["WTI"]; ok {
			wtiPrice = yp.Price
		}
	}
	return models.MarketAnalysis{
		Sentiment: "bullish", Score: 72,
		Summary: fmt.Sprintf("The crude oil market is displaying bullish momentum with WTI trading near $%.2f. Technical indicators are aligned with an upward bias as the 50-day moving average has crossed above the 200-day MA, forming a golden cross pattern. Fundamental drivers including OPEC+ supply discipline, declining US inventories, and resilient global demand support the constructive outlook. Key risk factors include potential demand slowdown from economic headwinds and the possibility of OPEC+ policy changes.", wtiPrice),
		KeyPoints: []string{
			"OPEC+ production cuts extended through Q3 2026, removing ~2.2 million bpd from market",
			"US crude inventories fell 4.2 million barrels, 3rd consecutive weekly draw",
			"China crude imports at record 12.4 million bpd supporting global demand",
			"Technical golden cross pattern on WTI daily chart signals bullish trend",
			"Geopolitical risk premium elevated due to Middle East tensions",
			"IEA raised 2026 demand growth forecast to 1.4 million bpd",
		},
		Technical: models.TechnicalSignals{
			RSI: 58.4, MACD: "bullish crossover", Signal: "buy",
			MovingAvg50: r2(wtiPrice - 1.20), MovingAvg200: r2(wtiPrice - 3.50), Trend: "uptrend",
		},
		UpdatedAt: now,
	}
}
