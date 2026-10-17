package services

import (
	"fmt"
	"hash/fnv"
	"io"
	"live-oil-prices-go/internal/models"
	"math"
	"sync"
	"time"
)

type MarketDataService struct {
	yahoo *YahooFinanceService
	pyth  *PythService
	eia   *EIAService

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
	svc := &MarketDataService{
		yahoo: NewYahooFinanceService(),
		pyth:  NewPythService(),
		eia:   NewEIAService(),
	}
	// Compute the spot/front-month basis once synchronously so the very
	// first /api/prices call is already basis-corrected, then keep it
	// fresh in the background.
	svc.refreshBasis()
	go svc.basisLoop()
	return svc
}

// basisRefreshEvery sets how often we resync the spot/front-month basis
// for each Pyth symbol. Yahoo's /api/prices feed refreshes every 30s on
// the server side, and the spot/futures spread itself moves slowly
// (typically pennies per hour outside of contract-roll days), so 30s
// keeps display values aligned without being chatty.
const basisRefreshEvery = 30 * time.Second

// basisLoop keeps the per-symbol Pyth basis fresh by periodically
// recomputing yahoo_front_month - pyth_raw_spot. The loop owns the
// only goroutine that calls SetBasis so there's no contention; reads
// elsewhere just see the most recent value applied transparently.
func (s *MarketDataService) basisLoop() {
	ticker := time.NewTicker(basisRefreshEvery)
	defer ticker.Stop()
	for range ticker.C {
		s.refreshBasis()
	}
}

// refreshBasis recomputes the additive offset between Yahoo's front-month
// price and Pyth's raw spot tick for every Pyth-tracked symbol and
// pushes it to PythService. After this returns, GetQuote/GetBucketBar
// readers see Pyth values shifted onto the Yahoo front-month price level
// — important for Brent in particular, where the spot/futures spread is
// $2–$4 and the raw Pyth tick would otherwise look like a misleading
// gap-down on the live chart.
//
// Symbols where either side is missing or zero are left at their last
// known basis (or 0 if never set) so we degrade to "raw Pyth" rather
// than silently zeroing out a previously valid correction.
func (s *MarketDataService) refreshBasis() {
	if s.yahoo == nil || s.pyth == nil {
		return
	}
	yahoos := s.yahoo.GetPrices()
	for _, f := range pythFeeds {
		yp, ok := yahoos[f.symbol]
		if !ok || yp.Price <= 0 {
			continue
		}
		raw, ok := s.pyth.GetRawQuote(f.symbol)
		if !ok || raw.Price <= 0 {
			continue
		}
		s.pyth.SetBasis(f.symbol, yp.Price-raw.Price)
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
		// No real data available - return unavailable price
		prices[i] = models.Price{
			Symbol:    c.symbol,
			Name:      c.name,
			Price:     0,
			Change:    0,
			ChangePct: 0,
			High:      0,
			Low:       0,
			Volume:    0,
			UpdatedAt: "",
			Source:    "",
			Available: false,
		}
	}
	return prices
}

func (s *MarketDataService) GetChartData(symbol string, days int, interval string) models.ChartData {
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

	if s.yahoo == nil {
		return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: []models.OHLCV{}}
	}

	// For daily intervals, return real Yahoo daily history
	if interval == "1d" {
		if bars := s.yahoo.GetDailyHistoryByDate(symbol, days); len(bars) > 0 {
			return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: bars}
		}
		// No daily history - return empty
		return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: []models.OHLCV{}}
	}

	// For intraday intervals (2h, 4h), fetch Yahoo intraday for the full range
	var yahooInterval, yahooRange string
	var hoursPerCandle int
	
	switch {
	case days <= 1:
		// 1 day: fetch 2d of 5m bars, aggregate to 1h
		yahooInterval = "5m"
		yahooRange = "2d"
		hoursPerCandle = 1
		interval = "1h"
	case days <= 7:
		// 1 week: fetch 5d of 15m bars, aggregate to 2h
		yahooInterval = "15m"
		yahooRange = "5d"
		hoursPerCandle = 2
		interval = "2h"
	case days <= 30:
		// 1 month: fetch 1mo of 60m bars, aggregate to 4h
		yahooInterval = "60m"
		yahooRange = "1mo"
		hoursPerCandle = 4
		interval = "4h"
	default:
		// Fall back to daily
		if bars := s.yahoo.GetDailyHistoryByDate(symbol, days); len(bars) > 0 {
			return models.ChartData{Symbol: symbol, Name: name, Interval: "1d", Data: bars}
		}
		return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: []models.OHLCV{}}
	}

	intradayBars := s.yahoo.FetchIntradayRange(symbol, yahooInterval, yahooRange)
	if len(intradayBars) == 0 {
		// Fall back to daily bars if available, sliced to the requested range
		if bars := s.yahoo.GetDailyHistoryByDate(symbol, days); len(bars) > 0 {
			return models.ChartData{Symbol: symbol, Name: name, Interval: "1d", Data: bars}
		}
		return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: []models.OHLCV{}}
	}

	// Aggregate to requested interval
	aggregated := aggregateBars(intradayBars, hoursPerCandle)
	
	// Slice to requested days by calendar date
	if days > 0 && days < 100 {
		cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Unix()
		filtered := make([]models.OHLCV, 0, len(aggregated))
		for _, bar := range aggregated {
			if bar.Time >= cutoff {
				filtered = append(filtered, bar)
			}
		}
		aggregated = filtered
	}

	return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: aggregated}
}

// aggregateBars aggregates 5-min (or similar) bars into larger hourly candles
func aggregateBars(bars []models.OHLCV, hoursPerCandle int) []models.OHLCV {
	if len(bars) == 0 || hoursPerCandle <= 0 {
		return bars
	}

	bucketSize := int64(hoursPerCandle * 3600)
	var result []models.OHLCV
	var currentBucket *models.OHLCV

	for _, bar := range bars {
		bucketStart := (bar.Time / bucketSize) * bucketSize

		if currentBucket == nil || currentBucket.Time != bucketStart {
			if currentBucket != nil {
				result = append(result, *currentBucket)
			}
			currentBucket = &models.OHLCV{
				Time:   bucketStart,
				Open:   bar.Open,
				High:   bar.High,
				Low:    bar.Low,
				Close:  bar.Close,
				Volume: bar.Volume,
			}
		} else {
			// Update the current bucket
			if bar.High > currentBucket.High {
				currentBucket.High = bar.High
			}
			if bar.Low < currentBucket.Low {
				currentBucket.Low = bar.Low
			}
			currentBucket.Close = bar.Close
			currentBucket.Volume += bar.Volume
		}
	}

	if currentBucket != nil {
		result = append(result, *currentBucket)
	}

	return result
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
	byID := make(map[string]models.Price, len(prices))
	for _, p := range prices {
		byID[p.Symbol] = p
	}

	out := make([]models.Prediction, 0, len(predictionSymbols))
	for _, ps := range predictionSymbols {
		price, ok := byID[ps.symbol]
		// Skip if price is unavailable
		if !ok || !price.Available {
			continue
		}

		current := price.Price
		predSource := price.Source
		if predSource == "" {
			predSource = "yahoo"
		}
		var history []float64
		if s.yahoo != nil {
			history = s.yahoo.GetHistory(ps.symbol)
		}

		if len(history) < 30 || current <= 0 {
			out = append(out, insufficientPrediction(ps.symbol, ps.name, current, predSource, ps.horizon, len(history)))
			continue
		}

		// Use the live price as the most recent close so the forecast
		// reflects intraday movement. Yahoo's daily series already contains
		// a bar for the current session, so we overwrite that bar instead
		// of appending a duplicate observation for the same day.
		lastBar, haveBar := s.yahoo.lastHistoryBarTime(ps.symbol)
		quoteAt, _ := time.Parse(time.RFC3339, price.UpdatedAt)
		closes := spliceCurrentClose(history, lastBar, haveBar, current, quoteAt)

		f, err := Forecast(closes, ps.horizon)
		if err != nil {
			out = append(out, insufficientPrediction(ps.symbol, ps.name, current, predSource, ps.horizon, len(history)))
			continue
		}

		pred := models.Prediction{
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
			Status:        f.Status,
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
		}
		if f.Status != ForecastStatusOK {
			// No publishable forecast: don't emit a predicted price, an
			// interval around it, or a confidence score for it.
			pred.Predicted, pred.PredictedLow, pred.PredictedHigh = 0, 0, 0
			pred.Confidence = 0
			pred.Direction = "neutral"
		}
		out = append(out, pred)
	}
	return out
}

// spliceCurrentClose returns `history` with the live price as its final
// observation. When the last daily bar already belongs to the quote's
// exchange day (or later), that bar is replaced; otherwise the live price
// is appended as a new day. Without bar timestamps we can't tell, so we
// append (the historical behaviour).
func spliceCurrentClose(history []float64, lastBarTime int64, haveBarTime bool, current float64, quoteAt time.Time) []float64 {
	out := make([]float64, len(history), len(history)+1)
	copy(out, history)
	if len(out) > 0 && haveBarTime && !quoteAt.IsZero() && exchangeDay(lastBarTime) >= exchangeDay(quoteAt.Unix()) {
		out[len(out)-1] = current
		return out
	}
	return append(out, current)
}

// insufficientPrediction is returned when we don't have enough history to
// fit and validate the model (cold start, Yahoo unreachable). It carries
// no predicted price; the UI shows a "not enough data" state.
func insufficientPrediction(symbol, name string, current float64, source string, horizon, samples int) models.Prediction {
	return models.Prediction{
		Symbol:     symbol,
		Name:       name,
		Current:    r2(current),
		Timeframe:  fmt.Sprintf("%d days", horizon),
		Direction:  "neutral",
		Analysis:   fmt.Sprintf("%s: not enough daily price history loaded to fit and backtest the forecast model (%d of at least 77 sessions). No forecast is shown until more data is available.", name, samples),
		Status:     ForecastStatusInsufficient,
		Model:      "holt-damped+backtest+rsi/macd",
		Source:     source,
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

	signals := fmt.Sprintf("RSI(14) is %.1f (%s), MACD histogram is %s, and the 50/200-day MA configuration suggests %s %s.",
		f.RSI14, rsiLabel, macdLabel, trendArticle, f.Trend)

	switch f.Status {
	case ForecastStatusNoEdge:
		return fmt.Sprintf(
			"%s: no %d-day forecast. Over the last %d sessions the damped Holt model's %d-day-ahead error (%.1f%% MAPE) "+
				"was no better than assuming the price stays where it is (%.1f%% MAPE), so we don't publish a point forecast. %s",
			name, f.HorizonDays, f.BacktestSteps, f.HorizonDays, f.MAPE*100, f.NaiveMAPE*100, signals,
		)
	case ForecastStatusInsufficient:
		return fmt.Sprintf(
			"%s: not enough daily history to backtest the %d-day forecast model, so no forecast is shown. %s",
			name, f.HorizonDays, signals,
		)
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
	out.Delayed = !s.pyth.Enabled()

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
// When appending a brand-new bucket, we anchor its Open to the previous
// bar's Close. This is the right thing to do because the in-progress
// Pyth bucket only contains the ticks Pyth has captured *so far* in
// that 5-minute window — typically a handful of seconds' worth, not a
// full bucket — so its raw "open" is just the price at the moment of
// Pyth's first poll inside the bucket, NOT the actual market open at
// the bucket boundary. Without anchoring you get a visible discontinuity
// between the previous bar's close and the live bar's open every time
// the chart loads. The high/low are widened to keep the previous close
// inside the candle's range so the wick still draws correctly.
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
		anchored := live
		anchored.Open = last.Close
		if anchored.High < anchored.Open {
			anchored.High = anchored.Open
		}
		if anchored.Low > anchored.Open || anchored.Low <= 0 {
			anchored.Low = anchored.Open
		}
		return append(series, anchored)
	}
	// Live bucket is older than the last Yahoo bar — Yahoo is ahead of
	// Pyth (rare, only happens right after a Yahoo refresh lands a future
	// timestamp). Trust Yahoo, drop the stale Pyth bucket.
	return series
}
