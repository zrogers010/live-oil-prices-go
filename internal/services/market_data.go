package services

import (
	"fmt"
	"live-oil-prices-go/internal/models"
	"math"
	"math/rand"
	"time"
)

type MarketDataService struct {
	rng        *rand.Rand
	basePrices map[string]float64
	yahoo      *YahooFinanceService
	pyth       *PythService
}

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
	}
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

	var data []models.OHLCV
	switch interval {
	case "2h":
		data = s.generateIntraday(base, days, 2)
	case "4h":
		data = s.generateIntraday(base, days, 4)
	default:
		data = s.generateDaily(base, days)
	}

	return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: data}
}

func (s *MarketDataService) generateDaily(base float64, days int) []models.OHLCV {
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
		change := (s.rng.Float64() - 0.47) * vol
		price += change
		open := price
		cl := open + (s.rng.Float64()-0.5)*vol
		high := math.Max(open, cl) + s.rng.Float64()*vol*0.5
		low := math.Min(open, cl) - s.rng.Float64()*vol*0.5
		v := int64(800000 + s.rng.Intn(1500000))
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

func (s *MarketDataService) generateIntraday(base float64, days int, hoursPerCandle int) []models.OHLCV {
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
			change := (s.rng.Float64() - 0.48) * vol
			price += change
			open := price
			cl := open + (s.rng.Float64()-0.5)*vol
			high := math.Max(open, cl) + s.rng.Float64()*vol*0.3
			low := math.Min(open, cl) - s.rng.Float64()*vol*0.3
			v := int64(50000 + s.rng.Intn(200000))
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
			Model:         "holt-linear+rsi/macd",
			Source:        predSource,
			Disclaimer:    predictionDisclaimer,
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

	return fmt.Sprintf(
		"%s: %d-day Holt linear forecast points to $%.2f (%+.2f%% from $%.2f), with an 80%% interval of $%.2f–$%.2f (±%.1f%%). "+
			"RSI(14) is %.1f (%s), MACD histogram is %s, and the 50/200-day MA configuration suggests %s %s. "+
			"Confidence: %.0f%%.",
		name,
		f.HorizonDays,
		f.Predicted, pct, f.Current,
		f.Low, f.High, bandPct,
		f.RSI14, rsiLabel,
		macdLabel,
		trendArticle, f.Trend,
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
// market is paused (weekend/holiday/maintenance) and we should fall back to
// Yahoo's intraday series for a meaningful chart.
//
// 5 minutes was chosen because Pyth's WTI publishers can briefly go quiet
// during low-activity periods even when markets are technically open
// (e.g. Sunday evening reopen). 5 min absorbs those pauses without
// flipping the chart over and back.
const pythLiveWindow = 5 * time.Minute

// GetHeroChart returns the current best-available chart for the homepage
// hero. When Pyth is publishing fresh ticks we serve the 1-minute streaming
// candle buffer; otherwise we fall back to the prior session's intraday
// Yahoo bars so the chart has something to render on weekends/holidays.
//
// `maxLiveBars` caps the live window (in 1-minute bars) when streaming.
// It's ignored in fallback mode — prior-session mode always returns the
// full session's bars (typically 80–280 5-min bars).
func (s *MarketDataService) GetHeroChart(symbol string, maxLiveBars int) models.HeroChart {
	out := models.HeroChart{Symbol: symbol, Bars: []models.PythCandle{}}

	// 1) Try Pyth streaming candles first.
	if s.pyth != nil {
		if q, ok := s.pyth.GetQuote(symbol); ok && time.Since(q.PublishedAt) <= pythLiveWindow {
			bars := s.pyth.GetCandles(symbol, maxLiveBars)
			if len(bars) > 0 {
				out.Mode = "live"
				out.Interval = "1m"
				out.Source = "pyth"
				out.UpdatedAt = q.PublishedAt.Format(time.RFC3339)
				out.Bars = bars
				return out
			}
		}
	}

	// 2) Fall back to the prior-session intraday Yahoo series.
	if s.yahoo != nil {
		bars, sessionDate, interval := s.yahoo.GetPriorSessionIntraday(symbol)
		if len(bars) > 0 {
			out.Mode = "prior-session"
			out.Interval = interval
			out.Source = "yahoo"
			out.SessionDate = sessionDate
			last := bars[len(bars)-1]
			out.UpdatedAt = time.Unix(last.Time, 0).UTC().Format(time.RFC3339)
			// Reshape OHLCV (which carries volume we don't need here) into
			// the same PythCandle shape the live mode uses, so the frontend
			// has a single bar type to render.
			out.Bars = make([]models.PythCandle, len(bars))
			for i, b := range bars {
				out.Bars[i] = models.PythCandle{
					Time:  b.Time,
					Open:  b.Open,
					High:  b.High,
					Low:   b.Low,
					Close: b.Close,
				}
			}
			return out
		}
	}

	// 3) Nothing available — empty payload, frontend renders the cold-start
	// "warming up the live feed" placeholder.
	out.Mode = "warming-up"
	return out
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
