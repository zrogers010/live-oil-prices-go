package services

import (
	"encoding/json"
	"fmt"
	"io"
	"live-oil-prices-go/internal/models"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Pyth Network (Hermes) integration — WTI ONLY.
//
// We use Pyth as a real-time tick source for a single symbol: WTI Crude Oil
// (USOILSPOT/USD CFD, published by first-party publishers including CME and
// major market makers). For every other commodity on the site we rely on
// Yahoo Finance, which is the more battle-tested feed for daily metadata,
// historical bars, and the long tail of contracts.
//
// Why WTI only? Pyth's coverage of the rest of the energy complex is weak
// in practice: dated futures (Henry Hub natgas, ICE Gasoil) are listed in
// the registry but lack live publishers, and Pyth has no spot/CFD feed for
// products like RBOB, heating oil, OPEC basket, or the Asian/Canadian
// crudes. The continuous WTI spot CFD is the one place where Pyth provides
// a clear win over Yahoo's 15-minute-delayed tick.
//
// Hermes is the off-chain price API operated by the Pyth Data Association.
// It exposes parsed prices over a free, unauthenticated REST endpoint:
//
//   https://hermes.pyth.network/v2/updates/price/latest?ids[]=<feed_id>
//
// Pyth price data is licensed under CC-BY-4.0 / CC0 for off-chain reads,
// which means we can legally surface the live values on a public website
// as long as we attribute the source ("Powered by Pyth Network").

const (
	hermesEndpoint = "https://hermes.pyth.network/v2/updates/price/latest"

	// pythPollEvery is how often we hit Hermes for the latest tick. Hermes
	// publishes new aggregates ~every 400ms during market hours and the
	// public endpoint comfortably handles >1 req/s; we run at 2s as a
	// compromise between candle-update responsiveness on the homepage chart
	// and being a polite citizen on a free, unauthenticated API.
	pythPollEvery = 2 * time.Second

	// pythCacheRetention is how long we keep a Pyth quote in the cache after
	// the last publish. The WTI CFD pauses for the weekend (~63h) and can
	// pause longer over US/UK holidays, so 5 days lets us survive Easter
	// and Christmas and still surface the most recent print before falling
	// back to Yahoo.
	pythCacheRetention = 5 * 24 * time.Hour

	// pythCandleInterval is the bucket size for the live candle aggregator.
	// 1 minute is the conventional minimum bar size for streaming charts and
	// matches what the homepage hero is configured to render.
	pythCandleInterval = time.Minute

	// pythMaxCandles caps the in-memory candle ring. 720 one-minute bars =
	// 12 hours, which covers a full NYMEX session plus the after-hours move
	// and is plenty for the homepage's 30m–6h windows.
	pythMaxCandles = 720
)

// pythFeed maps an internal symbol to a Pyth Hermes feed id. We keep this
// as a slice rather than a single constant so future symbols (if Pyth's
// coverage improves) can be added without restructuring the polling loop.
type pythFeed struct {
	symbol string // internal symbol, e.g. "WTI"
	feedID string // Pyth Hermes price feed id (hex, no 0x prefix)
}

// pythFeeds is the static list of feeds the poller subscribes to. WTI's
// USOILSPOT/USD CFD is a continuous spot product (no expiry), so the feed
// id is safe to hard-code.
var pythFeeds = []pythFeed{
	{symbol: "WTI", feedID: "925ca92ff005ae943c158e3563f59698ce7e75c5a8c8dd43303a0a154887b3e6"},
}

// pythRawResponse mirrors the parsed fields we care about from Hermes.
type pythRawResponse struct {
	Parsed []struct {
		ID    string `json:"id"`
		Price struct {
			Price       string `json:"price"`        // scaled integer as string
			Conf        string `json:"conf"`         // 1-sigma confidence interval
			Expo        int    `json:"expo"`         // base-10 exponent (usually negative)
			PublishTime int64  `json:"publish_time"` // unix seconds
		} `json:"price"`
	} `json:"parsed"`
}

// PythQuote is a parsed, denormalised price for one feed.
type PythQuote struct {
	Symbol      string
	Price       float64
	Confidence  float64 // ±$ at 1-sigma
	PublishedAt time.Time
}

// Stale returns true if the publish time is older than maxAge. Hermes
// publishes ~every 400ms during market hours; if we see >maxAge of staleness
// we treat the feed as offline (e.g. exchange closed, rolled contract).
func (q PythQuote) Stale(maxAge time.Duration) bool {
	return time.Since(q.PublishedAt) > maxAge
}

// IsLive returns true if the most recent publish is within ~60 seconds,
// which is the threshold the UI uses to render the green "Real-Time"
// indicator. During weekends and holidays the Pyth oil CFD feed pauses,
// so the cached quote is still useful as a "last close" but isn't live.
func (q PythQuote) IsLive() bool {
	return !q.Stale(60 * time.Second)
}

// PythService polls Hermes for the configured feeds, caches the latest
// quotes by symbol, and aggregates each tick into a rolling 1-minute candle
// buffer per symbol so the frontend can render a true streaming chart.
// Safe for concurrent use.
type PythService struct {
	client  *http.Client
	mu      sync.RWMutex
	quotes  map[string]PythQuote
	candles map[string][]models.PythCandle // keyed by internal symbol
	stop    chan struct{}
}

func NewPythService() *PythService {
	svc := &PythService{
		client:  &http.Client{Timeout: 8 * time.Second},
		quotes:  make(map[string]PythQuote),
		candles: make(map[string][]models.PythCandle),
		stop:    make(chan struct{}),
	}
	// Prime once synchronously so the very first /api/prices call after
	// startup already has Pyth data when the server is healthy.
	if err := svc.refresh(); err != nil {
		log.Printf("pyth: initial refresh failed: %v", err)
	}
	go svc.loop()
	return svc
}

// Stop terminates the background poller. Safe to call multiple times.
func (s *PythService) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

func (s *PythService) loop() {
	ticker := time.NewTicker(pythPollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if err := s.refresh(); err != nil {
				log.Printf("pyth: refresh failed: %v", err)
			}
		}
	}
}

// refresh issues a single batched call to Hermes for all configured feeds
// and updates the cache atomically.
func (s *PythService) refresh() error {
	if len(pythFeeds) == 0 {
		return nil
	}

	q := url.Values{}
	for _, f := range pythFeeds {
		q.Add("ids[]", f.feedID)
	}
	q.Set("parsed", "true")
	q.Set("encoding", "hex")

	req, err := http.NewRequest("GET", hermesEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "live-oil-prices-go/1.0 (+https://liveoilprices.com)")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("hermes request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("hermes status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var parsed pythRawResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	bySymbol := make(map[string]pythFeed, len(pythFeeds))
	for _, f := range pythFeeds {
		bySymbol[f.feedID] = f
	}

	updates := make(map[string]PythQuote, len(parsed.Parsed))
	for _, p := range parsed.Parsed {
		feed, ok := bySymbol[p.ID]
		if !ok {
			continue
		}
		price, ok := scalePrice(p.Price.Price, p.Price.Expo)
		if !ok || price <= 0 {
			continue
		}
		// Skip never-published feeds (publish_time = 0). Defence-in-depth;
		// the WTI feed has been live for years, but if we ever expand the
		// list this guard prevents a brand-new symbol from clobbering the
		// cache with a 1970-era epoch.
		if p.Price.PublishTime <= 0 {
			continue
		}
		conf, _ := scalePrice(p.Price.Conf, p.Price.Expo)

		updates[feed.symbol] = PythQuote{
			Symbol:      feed.symbol,
			Price:       price,
			Confidence:  conf,
			PublishedAt: time.Unix(p.Price.PublishTime, 0).UTC(),
		}
	}

	s.mu.Lock()
	for sym, q := range updates {
		s.quotes[sym] = q
		s.appendTickLocked(sym, q.Price, q.PublishedAt)
	}
	s.mu.Unlock()
	return nil
}

// appendTickLocked folds a single price tick into the per-symbol 1-minute
// candle buffer. Caller must hold s.mu (write lock).
//
// Bucketing: each tick is assigned to the floor(ts / 1min) bucket. If the
// last bar in the buffer is the same bucket, we update its high/low/close;
// otherwise we open a new bar with O=H=L=C=price. The buffer is truncated
// to pythMaxCandles to keep memory bounded.
//
// We deliberately do NOT back-fill empty buckets (e.g. the minute between
// the last weekend tick on Friday and Monday morning's open). Streaming
// chart libraries handle gaps gracefully by drawing a discontinuity, which
// is the honest representation of "no data published".
func (s *PythService) appendTickLocked(symbol string, price float64, ts time.Time) {
	if price <= 0 {
		return
	}
	bucket := ts.Truncate(pythCandleInterval).Unix()
	bars := s.candles[symbol]

	if n := len(bars); n > 0 && bars[n-1].Time == bucket {
		bar := bars[n-1]
		if price > bar.High {
			bar.High = price
		}
		if price < bar.Low {
			bar.Low = price
		}
		bar.Close = price
		bar.Ticks++
		bars[n-1] = bar
		s.candles[symbol] = bars
		return
	}

	bars = append(bars, models.PythCandle{
		Time: bucket, Open: price, High: price, Low: price, Close: price, Ticks: 1,
	})
	if len(bars) > pythMaxCandles {
		bars = bars[len(bars)-pythMaxCandles:]
	}
	s.candles[symbol] = bars
}

// GetBucketBar aggregates every Pyth 1-minute candle whose start falls in
// [bucketStart, bucketStart+bucketSec) into a single OHLC bar. Used to
// build the "live in-progress" bar that the homepage hero overlays on top
// of Yahoo's 5-minute intraday series — Yahoo refreshes every ~5 min, so
// without this overlay the rightmost bar would visibly lag the spot price
// by up to 5 minutes during fast-moving markets.
//
// Returns (bar, true) only if the bucket has at least one Pyth tick;
// otherwise (zero, false) so the caller knows there's nothing to show
// for that bucket yet.
func (s *PythService) GetBucketBar(symbol string, bucketStart, bucketSec int64) (models.PythCandle, bool) {
	if bucketSec <= 0 {
		return models.PythCandle{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	candles := s.candles[symbol]
	if len(candles) == 0 {
		return models.PythCandle{}, false
	}
	bucketEnd := bucketStart + bucketSec
	out := models.PythCandle{Time: bucketStart}
	initialized := false
	for _, c := range candles {
		if c.Time < bucketStart || c.Time >= bucketEnd {
			continue
		}
		if !initialized {
			out.Open = c.Open
			out.High = c.High
			out.Low = c.Low
			initialized = true
		}
		if c.High > out.High {
			out.High = c.High
		}
		if c.Low < out.Low {
			out.Low = c.Low
		}
		out.Close = c.Close
		out.Ticks += c.Ticks
	}
	if !initialized {
		return models.PythCandle{}, false
	}
	return out, true
}

// GetCandles returns up to `max` most recent 1-minute candles for a symbol,
// oldest-first (which is the order TradingView's lightweight-charts expects
// for setData). Pass max <= 0 to get the full retention window.
func (s *PythService) GetCandles(symbol string, max int) []models.PythCandle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bars := s.candles[symbol]
	if len(bars) == 0 {
		return nil
	}
	if max > 0 && len(bars) > max {
		bars = bars[len(bars)-max:]
	}
	out := make([]models.PythCandle, len(bars))
	copy(out, bars)
	return out
}

// GetQuotes returns a copy of the latest Pyth quotes keyed by internal
// symbol. Quotes older than `pythCacheRetention` are dropped so we don't
// surface a price that's days out of date even if the upstream goes silent.
func (s *PythService) GetQuotes() map[string]PythQuote {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]PythQuote, len(s.quotes))
	for k, v := range s.quotes {
		if v.Stale(pythCacheRetention) {
			continue
		}
		out[k] = v
	}
	return out
}

// GetQuote returns a single Pyth quote, or (zero, false) if missing or
// older than the cache retention window.
func (s *PythService) GetQuote(symbol string) (PythQuote, bool) {
	s.mu.RLock()
	q, ok := s.quotes[symbol]
	s.mu.RUnlock()
	if !ok || q.Stale(pythCacheRetention) {
		return PythQuote{}, false
	}
	return q, true
}

// scalePrice converts Hermes' (mantissa string, exponent) tuple into a
// human-scale float. Pyth scales prices to integers to avoid floating-point
// rounding on-chain, e.g. ("8336710", -5) → 83.36710.
func scalePrice(mantissa string, expo int) (float64, bool) {
	n, err := strconv.ParseInt(mantissa, 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(n) * math.Pow10(expo), true
}

// applyPyth merges a Pyth quote onto a Yahoo-derived Price record. We keep
// Yahoo's daily OHLC/volume/contract metadata (Pyth doesn't expose those),
// but use Pyth's tick price as the canonical "live" value and recompute the
// day's change against the previous session close.
//
// If yahoo is the zero value the function returns a Price built solely from
// the Pyth quote and a synthetic name lookup, so symbols that aren't on
// Yahoo at all still surface a real-time number.
func applyPyth(yahoo models.Price, q PythQuote) models.Price {
	merged := yahoo
	if merged.Symbol == "" {
		merged.Symbol = q.Symbol
		if name, ok := commodityNames[q.Symbol]; ok {
			merged.Name = name
		} else {
			merged.Name = q.Symbol
		}
	}

	merged.Price = round2(q.Price)

	prevClose := yahoo.Price - yahoo.Change
	change := 0.0
	changePct := 0.0
	if prevClose > 0 {
		change = q.Price - prevClose
		changePct = (change / prevClose) * 100
	}

	// Yahoo's chartPreviousClose can be wildly stale when a futures contract
	// rolls — a "+1%" day suddenly looks like "-15%" because Yahoo is
	// comparing the new front month against the old expiring one. If the
	// implied move is extreme, fall back to Yahoo's already-computed change
	// and adjust it by the small delta between Yahoo's last refresh and
	// Pyth's live tick.
	const sanityPctThreshold = 10.0
	if math.Abs(changePct) > sanityPctThreshold && yahoo.Price > 0 {
		priceDelta := q.Price - yahoo.Price
		change = yahoo.Change + priceDelta
		changePct = (change / yahoo.Price) * 100
		if math.Abs(changePct) > sanityPctThreshold {
			// Both baselines are unreliable — show the live price but no
			// directional indicator rather than misleading the user.
			change = 0
			changePct = 0
		}
	}

	merged.Change = round2(change)
	merged.ChangePct = round2(changePct)

	// Extend the intraday high/low band if the live tick has moved beyond
	// the values Yahoo reported on its last daily refresh.
	if yahoo.High == 0 || q.Price > yahoo.High {
		merged.High = round2(q.Price)
	}
	if yahoo.Low == 0 || (q.Price < yahoo.Low && q.Price > 0) {
		merged.Low = round2(q.Price)
	}

	merged.UpdatedAt = q.PublishedAt.Format(time.RFC3339)
	merged.Source = "pyth"
	return merged
}
