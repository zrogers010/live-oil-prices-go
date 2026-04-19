package services

import (
	"encoding/json"
	"fmt"
	"io"
	"live-oil-prices-go/internal/models"
	"log"
	"math"
	"net/http"
	"sync"
	"time"
)

type yahooSymbol struct {
	internal string
	yahoo    string
	name     string
}

var yahooSymbols = []yahooSymbol{
	{"WTI", "CL=F", "WTI Crude Oil"},
	{"BRENT", "BZ=F", "Brent Crude Oil"},
	{"NATGAS", "NG=F", "Natural Gas"},
	{"HEATING", "HO=F", "Heating Oil"},
	{"RBOB", "RB=F", "RBOB Gasoline"},
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol             string  `json:"symbol"`
				ShortName          string  `json:"shortName"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
				ChartPreviousClose float64 `json:"chartPreviousClose"`
				RegularMarketTime  int64   `json:"regularMarketTime"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []json.Number `json:"open"`
					High   []json.Number `json:"high"`
					Low    []json.Number `json:"low"`
					Close  []json.Number `json:"close"`
					Volume []json.Number `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// intradayBars holds a cached intraday series for one symbol along with the
// exchange-local trading day it represents. We keep the date alongside the
// bars so the API response can label the chart "Last session: YYYY-MM-DD"
// when markets are paused.
type intradayBars struct {
	bars        []models.OHLCV
	sessionDate string // YYYY-MM-DD in NYMEX exchange-local time
	fetchedAt   time.Time
	interval    string // e.g. "5m"
}

type YahooFinanceService struct {
	client     *http.Client
	mu         sync.RWMutex
	prices     map[string]models.Price
	history    map[string][]float64    // 2y of daily closes (legacy, kept for prediction models)
	historyOHLC map[string][]models.OHLCV // 2y of daily OHLCV bars used for the main chart
	intraday   map[string]intradayBars
}

func NewYahooFinanceService() *YahooFinanceService {
	svc := &YahooFinanceService{
		client:      &http.Client{Timeout: 15 * time.Second},
		prices:      make(map[string]models.Price),
		history:     make(map[string][]float64),
		historyOHLC: make(map[string][]models.OHLCV),
		intraday:    make(map[string]intradayBars),
	}
	svc.refresh()
	svc.refreshHistory()
	svc.refreshIntraday()
	go svc.loop()
	go svc.historyLoop()
	go svc.intradayLoop()
	return svc
}

func (s *YahooFinanceService) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.refresh()
	}
}

// historyLoop refreshes the 2-year daily-close history every 6 hours.
// Daily candles only roll over after market close so polling more often is
// wasteful; this is purely to pick up the new daily bar each session.
func (s *YahooFinanceService) historyLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.refreshHistory()
	}
}

// intradayLoop refreshes the cached 5-minute intraday bars every 5 minutes.
// The hero chart falls back to this series whenever Pyth is paused (weekend
// or holiday) so the homepage always has something meaningful to render.
//
// 5 minutes is a deliberate compromise: fast enough that newly-formed bars
// near the session close show up promptly (so when markets reopen Monday
// we don't strand on Friday's snapshot for an hour), but slow enough to be
// trivial load on Yahoo's free endpoint.
func (s *YahooFinanceService) intradayLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.refreshIntraday()
	}
}

func (s *YahooFinanceService) refresh() {
	var wg sync.WaitGroup
	results := make(chan models.Price, len(yahooSymbols))

	for _, sym := range yahooSymbols {
		wg.Add(1)
		go func(ys yahooSymbol) {
			defer wg.Done()
			p, err := s.fetchQuote(ys)
			if err != nil {
				log.Printf("yahoo: failed to fetch %s (%s): %v", ys.internal, ys.yahoo, err)
				return
			}
			results <- p
		}(sym)
	}

	wg.Wait()
	close(results)

	s.mu.Lock()
	for p := range results {
		s.prices[p.Symbol] = p
	}
	s.mu.Unlock()
}

func (s *YahooFinanceService) fetchQuote(sym yahooSymbol) (models.Price, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=5d&interval=1d&includePrePost=false",
		sym.yahoo,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return models.Price{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return models.Price{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return models.Price{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return models.Price{}, fmt.Errorf("read body: %w", err)
	}

	var chart yahooChartResponse
	if err := json.Unmarshal(body, &chart); err != nil {
		return models.Price{}, fmt.Errorf("parse json: %w", err)
	}

	if chart.Chart.Error != nil {
		return models.Price{}, fmt.Errorf("api error: %s - %s", chart.Chart.Error.Code, chart.Chart.Error.Description)
	}
	if len(chart.Chart.Result) == 0 {
		return models.Price{}, fmt.Errorf("no results")
	}

	meta := chart.Chart.Result[0].Meta
	price := meta.RegularMarketPrice

	var dayHigh, dayLow float64
	var volume int64
	var priorDailyClose float64

	quotes := chart.Chart.Result[0].Indicators.Quote
	if len(quotes) > 0 {
		q := quotes[0]
		if n := len(q.High); n > 0 {
			if v, err := q.High[n-1].Float64(); err == nil {
				dayHigh = v
			}
		}
		if n := len(q.Low); n > 0 {
			if v, err := q.Low[n-1].Float64(); err == nil {
				dayLow = v
			}
		}
		if n := len(q.Volume); n > 0 {
			if v, err := q.Volume[n-1].Int64(); err == nil {
				volume = v
			}
		}
		// Resolve the most recent daily close that is STRICTLY BEFORE the
		// regularMarketTime's NYMEX trading day. Two Yahoo behaviours we
		// have to handle:
		//   - Mid-session: the last 1d bar is "yesterday's close"; today
		//     hasn't formed yet → use n-1.
		//   - Post-close: the last 1d bar IS today's close → use n-2.
		// We compare in America/New_York because NYMEX/ICE products are
		// dated by exchange local time; comparing in raw UTC wrongly bins
		// the previous evening's close into the next calendar day.
		timestamps := chart.Chart.Result[0].Timestamp
		if len(q.Close) > 0 && len(timestamps) == len(q.Close) {
			marketDay := exchangeDay(meta.RegularMarketTime)
			for i := len(q.Close) - 1; i >= 0; i-- {
				v, err := q.Close[i].Float64()
				if err != nil || v <= 0 || math.IsNaN(v) {
					continue
				}
				if exchangeDay(timestamps[i]) == marketDay {
					continue
				}
				priorDailyClose = v
				break
			}
		}
	}

	change, changePct := computeChange(price, priorDailyClose, meta.ChartPreviousClose)

	if dayHigh == 0 {
		dayHigh = price
	}
	if dayLow == 0 {
		dayLow = price
	}

	contract := parseContractMonth(meta.ShortName, sym.name)

	return models.Price{
		Symbol:    sym.internal,
		Name:      sym.name,
		Price:     round2(price),
		Change:    round2(change),
		ChangePct: round2(changePct),
		High:      round2(dayHigh),
		Low:       round2(dayLow),
		Volume:    volume,
		UpdatedAt: time.Unix(meta.RegularMarketTime, 0).UTC().Format(time.RFC3339),
		Contract:  contract,
		Source:    "yahoo",
	}, nil
}

func (s *YahooFinanceService) GetPrices() map[string]models.Price {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]models.Price, len(s.prices))
	for k, v := range s.prices {
		out[k] = v
	}
	return out
}

// GetHistory returns a copy of the cached daily close history for the given
// internal symbol (e.g. "WTI"). Returns nil if no history has been loaded yet.
func (s *YahooFinanceService) GetHistory(symbol string) []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src, ok := s.history[symbol]
	if !ok {
		return nil
	}
	out := make([]float64, len(src))
	copy(out, src)
	return out
}

// refreshHistory fetches ~2 years of daily OHLCV bars for every Yahoo-tracked
// symbol in parallel and updates the cache. Both the OHLCV history (used by
// the main chart) and a closes-only projection (used by the prediction
// models) are derived from the same network call.
func (s *YahooFinanceService) refreshHistory() {
	type result struct {
		symbol string
		bars   []models.OHLCV
		closes []float64
	}
	var wg sync.WaitGroup
	results := make(chan result, len(yahooSymbols))

	for _, sym := range yahooSymbols {
		wg.Add(1)
		go func(ys yahooSymbol) {
			defer wg.Done()
			bars, err := s.fetchHistory(ys)
			if err != nil {
				log.Printf("yahoo: failed to fetch history for %s (%s): %v", ys.internal, ys.yahoo, err)
				return
			}
			closes := make([]float64, 0, len(bars))
			for _, b := range bars {
				closes = append(closes, b.Close)
			}
			results <- result{symbol: ys.internal, bars: bars, closes: closes}
		}(sym)
	}

	wg.Wait()
	close(results)

	s.mu.Lock()
	for r := range results {
		s.history[r.symbol] = r.closes
		s.historyOHLC[r.symbol] = r.bars
	}
	s.mu.Unlock()
}

// fetchHistory pulls 2y of daily OHLCV bars for a Yahoo symbol. We keep
// every bar that has a valid (positive, non-NaN) close; bars with null
// individual O/H/L are repaired by falling back to the close so the chart
// renders without gaps.
func (s *YahooFinanceService) fetchHistory(sym yahooSymbol) ([]models.OHLCV, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=2y&interval=1d&includePrePost=false",
		sym.yahoo,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var chart yahooChartResponse
	if err := json.Unmarshal(body, &chart); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	if chart.Chart.Error != nil {
		return nil, fmt.Errorf("api error: %s - %s", chart.Chart.Error.Code, chart.Chart.Error.Description)
	}
	if len(chart.Chart.Result) == 0 {
		return nil, fmt.Errorf("no results")
	}
	timestamps := chart.Chart.Result[0].Timestamp
	quotes := chart.Chart.Result[0].Indicators.Quote
	if len(quotes) == 0 {
		return nil, fmt.Errorf("no quote indicators")
	}
	q := quotes[0]
	bars := make([]models.OHLCV, 0, len(timestamps))
	// Yahoo can return null entries for non-trading days that weren't filtered;
	// we drop bars whose close is missing/invalid. For bars with null O/H/L
	// individually (rare but happens around contract rolls), we repair with
	// the close so the chart still draws a candle instead of leaving a hole.
	for i, ts := range timestamps {
		if i >= len(q.Close) {
			break
		}
		cl, err := q.Close[i].Float64()
		if err != nil || cl <= 0 || math.IsNaN(cl) {
			continue
		}
		open := cl
		if i < len(q.Open) {
			if v, err := q.Open[i].Float64(); err == nil && v > 0 && !math.IsNaN(v) {
				open = v
			}
		}
		high := math.Max(open, cl)
		if i < len(q.High) {
			if v, err := q.High[i].Float64(); err == nil && v > 0 && !math.IsNaN(v) {
				high = v
			}
		}
		low := math.Min(open, cl)
		if i < len(q.Low) {
			if v, err := q.Low[i].Float64(); err == nil && v > 0 && !math.IsNaN(v) {
				low = v
			}
		}
		var vol int64
		if i < len(q.Volume) {
			if v, err := q.Volume[i].Int64(); err == nil && v >= 0 {
				vol = v
			}
		}
		bars = append(bars, models.OHLCV{
			Time: ts, Open: round2(open), High: round2(high), Low: round2(low), Close: round2(cl), Volume: vol,
		})
	}
	if len(bars) < 30 {
		return nil, fmt.Errorf("insufficient daily history: %d bars", len(bars))
	}
	return bars, nil
}

// GetDailyHistory returns up to `days` of the most recent cached daily
// OHLCV bars for `symbol`, oldest-first. Returns nil if nothing is cached
// yet for that symbol (e.g. cold start, or a non-Yahoo symbol). The
// `days` cap is taken in *bar* count, not calendar days, so weekends and
// holidays are naturally excluded from the count.
func (s *YahooFinanceService) GetDailyHistory(symbol string, days int) []models.OHLCV {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src, ok := s.historyOHLC[symbol]
	if !ok || len(src) == 0 {
		return nil
	}
	if days <= 0 || days >= len(src) {
		out := make([]models.OHLCV, len(src))
		copy(out, src)
		return out
	}
	out := make([]models.OHLCV, days)
	copy(out, src[len(src)-days:])
	return out
}

// intradayHotSymbols are the symbols we proactively keep an intraday series
// cached for. WTI is the only one used by the homepage hero, but we cache
// Brent too because the per-commodity detail pages can fall back to the
// same machinery when their own live feed isn't available.
var intradayHotSymbols = map[string]bool{
	"WTI":   true,
	"BRENT": true,
}

// refreshIntraday fans out a fetchIntraday call for each hot symbol in
// parallel and replaces the cache atomically. Errors are logged but the
// previous cached value is kept so a transient Yahoo failure doesn't blank
// the chart.
func (s *YahooFinanceService) refreshIntraday() {
	type result struct {
		symbol string
		bars   intradayBars
	}
	var wg sync.WaitGroup
	results := make(chan result, len(intradayHotSymbols))
	for _, sym := range yahooSymbols {
		if !intradayHotSymbols[sym.internal] {
			continue
		}
		wg.Add(1)
		go func(ys yahooSymbol) {
			defer wg.Done()
			bars, sessionDate, err := s.fetchIntraday(ys, "5m", "5d")
			if err != nil {
				log.Printf("yahoo: intraday fetch failed for %s (%s): %v", ys.internal, ys.yahoo, err)
				return
			}
			results <- result{symbol: ys.internal, bars: intradayBars{
				bars:        bars,
				sessionDate: sessionDate,
				fetchedAt:   time.Now().UTC(),
				interval:    "5m",
			}}
		}(sym)
	}
	wg.Wait()
	close(results)

	s.mu.Lock()
	for r := range results {
		s.intraday[r.symbol] = r.bars
	}
	s.mu.Unlock()
}

// GetPriorSessionIntraday returns the cached intraday bars for the most
// recent COMPLETE exchange-local trading day, along with that day's date
// (YYYY-MM-DD in NYMEX local time) and the bar interval (e.g. "5m"). If
// nothing is cached yet, returns nil bars and an empty date.
//
// "Most recent complete trading day" is defined as the latest exchange-day
// whose bars don't overlap with the current exchange-day. During market
// hours this is yesterday's session; on weekends/holidays it's whichever
// trading day was most recently completed (typically Friday).
func (s *YahooFinanceService) GetPriorSessionIntraday(symbol string) (bars []models.OHLCV, sessionDate, interval string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cached, ok := s.intraday[symbol]
	if !ok {
		return nil, "", ""
	}
	out := make([]models.OHLCV, len(cached.bars))
	copy(out, cached.bars)
	return out, cached.sessionDate, cached.interval
}

// fetchIntraday hits Yahoo's chart endpoint for an intraday series and
// returns ONLY the bars belonging to the most recent complete exchange-day.
// We deliberately filter to a single session because:
//   - It's what the user asked for ("1 day chart of the prior open day").
//   - It keeps the bar count small (~80–280 bars) for snappy chart renders.
//   - It avoids visually splicing two sessions into one continuous line,
//     which can imply a price gap that doesn't exist (the gap *is* the
//     overnight settle pause).
//
// Returns (bars, sessionDateYYYYMMDD, error). bars are oldest-first.
func (s *YahooFinanceService) fetchIntraday(sym yahooSymbol, interval, rangeParam string) ([]models.OHLCV, string, error) {
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=%s&includePrePost=false",
		sym.yahoo, rangeParam, interval,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	var chart yahooChartResponse
	if err := json.Unmarshal(body, &chart); err != nil {
		return nil, "", fmt.Errorf("parse json: %w", err)
	}
	if chart.Chart.Error != nil {
		return nil, "", fmt.Errorf("api error: %s - %s", chart.Chart.Error.Code, chart.Chart.Error.Description)
	}
	if len(chart.Chart.Result) == 0 {
		return nil, "", fmt.Errorf("no results")
	}
	timestamps := chart.Chart.Result[0].Timestamp
	quotes := chart.Chart.Result[0].Indicators.Quote
	if len(quotes) == 0 || len(timestamps) == 0 {
		return nil, "", fmt.Errorf("empty intraday series")
	}
	q := quotes[0]

	// Pick the most recent complete exchange-day. Walk backwards from the
	// last timestamp; the first bar's exchange-day defines our anchor, but
	// if that day matches "today" in NYMEX local time AND the underlying
	// market is closed (weekend/holiday), Yahoo may still serve a couple of
	// stale post-close bars dated today — we still want the *previous*
	// fully-formed session in that case.
	//
	// Strategy: find the most recent NYMEX day that has at least N bars in
	// the series. N=10 weeds out partial post-close residue and weekend
	// straggler ticks. Among remaining days, pick the latest.
	const minBarsPerSession = 10
	dayCounts := make(map[string]int)
	for _, ts := range timestamps {
		dayCounts[exchangeDay(ts)]++
	}
	// Sort days descending, keep the first one with >= minBarsPerSession.
	var candidateDays []string
	for d, count := range dayCounts {
		if count >= minBarsPerSession {
			candidateDays = append(candidateDays, d)
		}
	}
	if len(candidateDays) == 0 {
		return nil, "", fmt.Errorf("no session has enough intraday bars (max=%d)", maxIntInMap(dayCounts))
	}
	// candidateDays are YYYY-MM-DD strings; lex sort is calendar sort.
	sessionDate := candidateDays[0]
	for _, d := range candidateDays[1:] {
		if d > sessionDate {
			sessionDate = d
		}
	}

	bars := make([]models.OHLCV, 0, dayCounts[sessionDate])
	for i, ts := range timestamps {
		if exchangeDay(ts) != sessionDate {
			continue
		}
		open, _ := q.Open[i].Float64()
		high, _ := q.High[i].Float64()
		low, _ := q.Low[i].Float64()
		cl, _ := q.Close[i].Float64()
		vol, _ := q.Volume[i].Int64()
		if cl <= 0 || math.IsNaN(cl) {
			continue
		}
		bars = append(bars, models.OHLCV{
			Time: ts, Open: round2(open), High: round2(high),
			Low: round2(low), Close: round2(cl), Volume: vol,
		})
	}
	if len(bars) == 0 {
		return nil, "", fmt.Errorf("session %s had bars in metadata but all rows were null", sessionDate)
	}
	return bars, sessionDate, nil
}

// maxIntInMap is a tiny helper for diagnostic error messages.
func maxIntInMap(m map[string]int) int {
	max := 0
	for _, v := range m {
		if v > max {
			max = v
		}
	}
	return max
}

var monthNames = []string{
	"Jan", "Feb", "Mar", "Apr", "May", "Jun",
	"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
}

// parseContractMonth extracts a clean contract label like "May 2026" from
// Yahoo Finance's shortName. Falls back to deriving from current date.
func parseContractMonth(shortName, baseName string) string {
	for _, m := range monthNames {
		idx := -1
		for i := 0; i <= len(shortName)-len(m); i++ {
			if shortName[i:i+len(m)] == m {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		rest := shortName[idx+len(m):]
		// Expect " YY" after month name
		if len(rest) >= 3 && rest[0] == ' ' && rest[1] >= '0' && rest[1] <= '9' && rest[2] >= '0' && rest[2] <= '9' {
			yearStr := rest[1:3]
			return fmt.Sprintf("%s 20%s Contract", m, yearStr)
		}
	}
	// shortName doesn't have month info (e.g. Brent), derive from date
	now := time.Now()
	month := now.Month()
	year := now.Year()
	if now.Day() >= 20 {
		month++
		if month > 12 {
			month = 1
			year++
		}
	}
	return fmt.Sprintf("%s %d Contract", month.String()[:3], year)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// nyTZ is the canonical exchange timezone for the NYMEX / ICE / CME
// futures we surface. All daily-bar dating is computed in this zone so a
// 17:00 ET close doesn't get misfiled into the next UTC calendar day.
var nyTZ = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Fallback to a fixed -05:00 offset (EST) if the tzdata isn't
		// available on the host. DST is unlikely to flip the date for
		// our use case so this is a safe approximation.
		return time.FixedZone("EST", -5*60*60)
	}
	return loc
}()

// exchangeDay returns a YYYY-MM-DD date string for a unix timestamp,
// computed in the NYMEX/ICE exchange-local timezone.
func exchangeDay(unixSec int64) string {
	return time.Unix(unixSec, 0).In(nyTZ).Format("2006-01-02")
}

// extremeChangePct is the threshold above which we suppress a computed daily
// change. Real-world energy commodities have moved 7–10% on the very worst
// sessions (2008 crash, 2020 covid-lockdown crash); anything beyond ~10%
// in a single day is almost always a Yahoo data artefact (contract roll,
// missing dividend, bad print). Showing it would mislead users — better to
// surface the live price with no change indicator than a fake -14% crash.
const extremeChangePct = 10.0

// computeChange derives the day's (change, %change) for a Yahoo quote.
//
// Yahoo's meta.chartPreviousClose can be corrupted when a futures contract
// rolls — it returns the previous front-month's close even after the new
// contract has taken over, fabricating a fake double-digit move. Using the
// actual prior daily-bar close from the 5d series is robust because it's
// the same instrument that's quoting today.
//
// As a defence-in-depth, we also suppress any final result whose magnitude
// exceeds extremeChangePct.
func computeChange(price, priorDailyClose, metaPrevClose float64) (float64, float64) {
	prev := priorDailyClose
	if prev <= 0 {
		prev = metaPrevClose
	}
	if prev <= 0 || price <= 0 {
		return 0, 0
	}
	change := price - prev
	pct := (change / prev) * 100
	if math.Abs(pct) > extremeChangePct {
		return 0, 0
	}
	return change, pct
}
