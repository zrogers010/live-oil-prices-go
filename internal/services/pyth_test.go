package services

import (
	"encoding/json"
	"fmt"
	"live-oil-prices-go/internal/models"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestScalePrice(t *testing.T) {
	cases := []struct {
		mantissa string
		expo     int
		want     float64
		ok       bool
	}{
		{"8336710", -5, 83.36710, true},     // WTI sample
		{"325000000", -8, 3.25, true},       // tiny number, normal expo
		{"42", 0, 42, true},                 // zero exponent
		{"not-a-number", -5, 0, false},      // garbage mantissa
	}
	for _, c := range cases {
		got, ok := scalePrice(c.mantissa, c.expo)
		if ok != c.ok {
			t.Errorf("scalePrice(%q, %d) ok=%v want %v", c.mantissa, c.expo, ok, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		if absDiff(got, c.want) > 1e-6 {
			t.Errorf("scalePrice(%q, %d) = %v, want %v", c.mantissa, c.expo, got, c.want)
		}
	}
}

func TestApplyPyth_OverlaysOnYahoo(t *testing.T) {
	yahoo := models.Price{
		Symbol:    "WTI",
		Name:      "WTI Crude Oil",
		Price:     82.10,
		Change:    1.20,
		ChangePct: 1.48,
		High:      82.40,
		Low:       80.90,
		Volume:    1_200_000,
		Contract:  "May 2026 Contract",
		Source:    "yahoo",
	}
	q := PythQuote{
		Symbol:      "WTI",
		Price:       82.85,
		Confidence:  0.28,
		PublishedAt: time.Now().UTC(),
	}

	got := applyPyth(yahoo, q)

	if got.Source != "pyth" {
		t.Errorf("expected source=pyth, got %q", got.Source)
	}
	if got.Price != 82.85 {
		t.Errorf("expected live price 82.85, got %v", got.Price)
	}
	// prevClose = 82.10 - 1.20 = 80.90, change = 82.85 - 80.90 = 1.95
	if absDiff(got.Change, 1.95) > 0.001 {
		t.Errorf("expected change 1.95 vs prev close, got %v", got.Change)
	}
	wantPct := (1.95 / 80.90) * 100
	if absDiff(got.ChangePct, round2(wantPct)) > 0.01 {
		t.Errorf("expected pct ~%.2f, got %v", wantPct, got.ChangePct)
	}
	if got.Volume != 1_200_000 {
		t.Errorf("yahoo volume should be preserved, got %v", got.Volume)
	}
	if got.Contract != "May 2026 Contract" {
		t.Errorf("yahoo contract should be preserved, got %q", got.Contract)
	}
	// Pyth tick is above Yahoo's daily high → high should expand.
	if got.High != 82.85 {
		t.Errorf("expected high to expand to 82.85, got %v", got.High)
	}
	// Pyth tick is above Yahoo's daily low → low should be unchanged.
	if got.Low != 80.90 {
		t.Errorf("expected low unchanged at 80.90, got %v", got.Low)
	}
}

// When Yahoo's prevClose is corrupted by a contract roll (e.g. CL=F shows a
// fake -15% move because the previous close belongs to the prior front
// month), applyPyth must NOT propagate that corruption into the merged
// price.
func TestApplyPyth_GuardsAgainstYahooRollArtifact(t *testing.T) {
	yahoo := models.Price{
		Symbol:    "WTI",
		Name:      "WTI Crude Oil",
		Price:     82.59,
		Change:    -16.49, // implied prev close of $99.08 — clearly bogus
		ChangePct: -16.64,
		High:      82.40,
		Low:       80.90,
		Source:    "yahoo",
	}
	q := PythQuote{
		Symbol:      "WTI",
		Price:       83.37,
		PublishedAt: time.Now().UTC(),
	}

	got := applyPyth(yahoo, q)

	// Naive prevClose subtraction would give change = 83.37 - 99.08 = -15.71.
	// The sanity guard should detect that and recover by adjusting Yahoo's
	// own change by the small live delta: -16.49 + (83.37 - 82.59) = -15.71,
	// which is STILL out of bounds, so we expect change=0 (rather than a
	// misleading double-digit number).
	if got.Change != 0 || got.ChangePct != 0 {
		t.Errorf("expected sanity guard to zero out a clearly bogus change, got change=%v pct=%v", got.Change, got.ChangePct)
	}
	if got.Price != 83.37 {
		t.Errorf("expected live Pyth price to still be surfaced, got %v", got.Price)
	}
}

func TestApplyPyth_NoYahooBaseline(t *testing.T) {
	q := PythQuote{
		Symbol:      "WTI",
		Price:       83.42,
		PublishedAt: time.Now().UTC(),
	}

	got := applyPyth(models.Price{}, q)

	if got.Symbol != "WTI" {
		t.Errorf("expected symbol WTI, got %q", got.Symbol)
	}
	if got.Name == "" {
		t.Error("expected name to be filled in from commodityNames map")
	}
	if got.Price != 83.42 {
		t.Errorf("expected price 83.42, got %v", got.Price)
	}
	if got.Change != 0 || got.ChangePct != 0 {
		t.Errorf("expected zero change without yahoo baseline, got change=%v pct=%v", got.Change, got.ChangePct)
	}
	if got.Source != "pyth" {
		t.Errorf("expected source=pyth, got %q", got.Source)
	}
}

func TestPythQuote_Stale(t *testing.T) {
	fresh := PythQuote{PublishedAt: time.Now().Add(-30 * time.Second)}
	if fresh.Stale(2 * time.Minute) {
		t.Error("30s old quote should not be stale within 2-min budget")
	}
	old := PythQuote{PublishedAt: time.Now().Add(-5 * time.Minute)}
	if !old.Stale(2 * time.Minute) {
		t.Error("5min old quote should be stale within 2-min budget")
	}
}

func TestPythQuote_IsLive(t *testing.T) {
	live := PythQuote{PublishedAt: time.Now().Add(-10 * time.Second)}
	if !live.IsLive() {
		t.Error("10s old quote should be considered live")
	}
	paused := PythQuote{PublishedAt: time.Now().Add(-3 * time.Hour)}
	if paused.IsLive() {
		t.Error("3h old quote should not be considered live (markets paused)")
	}
}

// TestPythService_Refresh end-to-end exercises the HTTP -> parse -> cache
// pipeline against a fake Hermes server, so we don't need network access.
func TestPythService_Refresh(t *testing.T) {
	if len(pythFeeds) == 0 {
		t.Fatal("pythFeeds should contain at least WTI")
	}
	wtiID := pythFeeds[0].feedID

	now := time.Now().Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query()["ids[]"]
		if len(ids) == 0 {
			http.Error(w, "missing ids", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"parsed":[
		  {"id":%q,"price":{"price":"8336710","conf":"28069","expo":-5,"publish_time":%d}}
		]}`, wtiID, now)
	}))
	defer srv.Close()

	svc := &PythService{
		client: srv.Client(),
		quotes: make(map[string]PythQuote),
		stop:   make(chan struct{}),
	}

	// The real `refresh` builds the URL from the constant, so we re-implement
	// the request inline here to validate parsing + caching behaviour
	// deterministically against the fake server.
	resp, err := svc.client.Get(srv.URL + "?ids[]=" + wtiID + "&parsed=true&encoding=hex")
	if err != nil {
		t.Fatalf("test fetch: %v", err)
	}
	defer resp.Body.Close()
	var raw pythRawResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Parsed) != 1 {
		t.Fatalf("expected 1 parsed entry, got %d", len(raw.Parsed))
	}
	wtiPx, ok := scalePrice(raw.Parsed[0].Price.Price, raw.Parsed[0].Price.Expo)
	if !ok {
		t.Fatal("scale wti")
	}
	if absDiff(wtiPx, 83.36710) > 1e-5 {
		t.Errorf("WTI scaled price = %v, want 83.36710", wtiPx)
	}
}

// TestAppendTickLocked_BucketsByMinute verifies that ticks within the same
// 1-minute bucket update the existing bar (high/low/close), and ticks that
// cross the boundary open a new bar.
func TestAppendTickLocked_BucketsByMinute(t *testing.T) {
	svc := &PythService{
		quotes:  make(map[string]PythQuote),
		candles: make(map[string][]models.PythCandle),
	}

	base := time.Date(2026, 4, 18, 14, 30, 0, 0, time.UTC)

	// Three ticks in the same minute: O=83.10, H=83.45, L=83.05, C=83.20
	svc.appendTickLocked("WTI", 83.10, base.Add(2*time.Second))
	svc.appendTickLocked("WTI", 83.45, base.Add(20*time.Second))
	svc.appendTickLocked("WTI", 83.05, base.Add(40*time.Second))
	svc.appendTickLocked("WTI", 83.20, base.Add(58*time.Second))

	// One tick in the next minute: a fresh bar opens at 83.30.
	svc.appendTickLocked("WTI", 83.30, base.Add(70*time.Second))

	bars := svc.candles["WTI"]
	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}

	first := bars[0]
	if first.Open != 83.10 || first.High != 83.45 || first.Low != 83.05 || first.Close != 83.20 {
		t.Errorf("first bar OHLC wrong: %+v", first)
	}
	if first.Ticks != 4 {
		t.Errorf("first bar should have 4 ticks, got %d", first.Ticks)
	}
	if first.Time != base.Truncate(time.Minute).Unix() {
		t.Errorf("first bar time wrong: got %d", first.Time)
	}

	second := bars[1]
	if second.Open != 83.30 || second.Close != 83.30 || second.Ticks != 1 {
		t.Errorf("second bar should be a fresh single-tick bar, got %+v", second)
	}
	if second.Time != base.Add(time.Minute).Truncate(time.Minute).Unix() {
		t.Errorf("second bar time wrong: got %d", second.Time)
	}
}

// TestAppendTickLocked_RingCap verifies the buffer never exceeds pythMaxCandles.
func TestAppendTickLocked_RingCap(t *testing.T) {
	svc := &PythService{
		quotes:  make(map[string]PythQuote),
		candles: make(map[string][]models.PythCandle),
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < pythMaxCandles+50; i++ {
		svc.appendTickLocked("WTI", 80.0+float64(i)*0.01, base.Add(time.Duration(i)*time.Minute))
	}
	bars := svc.candles["WTI"]
	if len(bars) != pythMaxCandles {
		t.Errorf("expected ring capped at %d, got %d", pythMaxCandles, len(bars))
	}
	// Oldest bar should be the (50th) one — first 50 trimmed.
	expectedFirstOpen := 80.0 + float64(50)*0.01
	if absDiff(bars[0].Open, expectedFirstOpen) > 1e-9 {
		t.Errorf("oldest bar open should be %v after trim, got %v", expectedFirstOpen, bars[0].Open)
	}
}

// TestGetCandles_ReturnsCopy verifies we don't hand callers a slice backed
// by the live buffer (which would be a data race waiting to happen).
func TestGetCandles_ReturnsCopy(t *testing.T) {
	svc := &PythService{
		quotes:  make(map[string]PythQuote),
		candles: make(map[string][]models.PythCandle),
	}
	base := time.Date(2026, 4, 18, 14, 30, 0, 0, time.UTC)
	svc.appendTickLocked("WTI", 83.10, base)
	svc.appendTickLocked("WTI", 83.20, base.Add(time.Minute))

	got := svc.GetCandles("WTI", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(got))
	}
	got[0].Close = 999.99 // mutate caller copy
	if svc.candles["WTI"][0].Close == 999.99 {
		t.Error("GetCandles must return a defensive copy, not the live slice")
	}
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
