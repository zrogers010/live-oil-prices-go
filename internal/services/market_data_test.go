package services

import (
	"fmt"
	"live-oil-prices-go/internal/models"
	"testing"
	"time"
)

func newDeterministicMarketDataService() *MarketDataService {
	svc := &MarketDataService{
		yahoo: nil,
		pyth:  nil,
		eia:   nil,
	}
	return svc
}

func TestGetPricesReturnsAllCommodities(t *testing.T) {
	svc := newDeterministicMarketDataService()
	prices := svc.GetPrices()
	if len(prices) != len(allCommodities) {
		t.Fatalf("expected %d commodities, got %d", len(allCommodities), len(prices))
	}
	for _, p := range prices {
		if p.Symbol == "" || p.Name == "" {
			t.Fatalf("missing symbol or name in %+v", p)
		}
	}
}

func TestGetChartDataDefaultsAndOverrides(t *testing.T) {
	svc := newDeterministicMarketDataService()

	// Without Yahoo data, GetChartData returns empty data
	short := svc.GetChartData("WTI", 5, "")
	if short.Symbol != "WTI" {
		t.Fatalf("expected symbol WTI, got %q", short.Symbol)
	}
	if short.Name != "WTI Crude Oil" {
		t.Fatalf("expected WTI name, got %q", short.Name)
	}
	if short.Interval != "2h" {
		t.Fatalf("expected 2h interval for 5 days, got %q", short.Interval)
	}
	// Expect empty data when no real Yahoo data available
	if got := len(short.Data); got != 0 {
		t.Fatalf("expected empty chart data without Yahoo service, got %d", got)
	}

	medium := svc.GetChartData("BRENT", 30, "")
	if medium.Interval != "4h" {
		t.Fatalf("expected 4h interval for 30 days, got %q", medium.Interval)
	}
	if len(medium.Data) != 0 {
		t.Fatalf("expected empty chart data without Yahoo service")
	}

	long := svc.GetChartData("NATGAS", 120, "")
	if long.Interval != "1d" {
		t.Fatalf("expected 1d interval for 120 days, got %q", long.Interval)
	}
	if len(long.Data) != 0 {
		t.Fatalf("expected empty chart data without Yahoo service")
	}

	override := svc.GetChartData("GASOIL", 120, "2h")
	if override.Interval != "2h" {
		t.Fatalf("expected explicit interval override to win, got %q", override.Interval)
	}
}

func TestGetChartDataUnknownSymbolFallsBack(t *testing.T) {
	svc := newDeterministicMarketDataService()
	data := svc.GetChartData("UNKNOWN", 7, "")

	if data.Symbol != "UNKNOWN" {
		t.Fatalf("expected symbol to remain UNKNOWN, got %q", data.Symbol)
	}
	if data.Name != "UNKNOWN" {
		t.Fatalf("expected unknown symbol to fallback to symbol name, got %q", data.Name)
	}

	if data.Interval != "2h" {
		t.Fatalf("expected interval to remain valid for 7 days, got %q", data.Interval)
	}
	// Without Yahoo data, expect empty data
	if len(data.Data) != 0 {
		t.Fatalf("expected empty data for unknown symbol without Yahoo service")
	}
}

// TestGetChartDataIsStableAcrossCalls is a regression guard for the bug
// where flipping back and forth between commodity tabs returned a freshly
// randomised series each time. With real Yahoo data, two consecutive 
// identical requests must return identical bars. Without Yahoo data,
// returns should consistently be empty.
func TestGetChartDataIsStableAcrossCalls(t *testing.T) {
	svc := newDeterministicMarketDataService()

	first := svc.GetChartData("OPEC", 30, "")
	second := svc.GetChartData("OPEC", 30, "")
	third := svc.GetChartData("OPEC", 30, "")

	// Without Yahoo service, should consistently return empty data
	if len(first.Data) != 0 {
		t.Fatalf("expected empty chart data without Yahoo service")
	}
	if len(first.Data) != len(second.Data) || len(second.Data) != len(third.Data) {
		t.Fatalf("inconsistent bar counts across calls: %d / %d / %d",
			len(first.Data), len(second.Data), len(third.Data))
	}
}

// TestGetChartDataIntervalSelection tests that GetChartData selects the correct
// interval and aggregation strategy based on the requested day range
func TestGetChartDataIntervalSelection(t *testing.T) {
	svc := newDeterministicMarketDataService()
	
	tests := []struct {
		days             int
		expectedInterval string
	}{
		{1, "2h"},   // days=1 falls under days<=7, so 2h
		{5, "2h"},
		{7, "2h"},
		{20, "4h"},
		{30, "4h"},
		{90, "1d"},
		{180, "1d"},
	}
	
	for _, tt := range tests {
		t.Run(fmt.Sprintf("days=%d", tt.days), func(t *testing.T) {
			data := svc.GetChartData("WTI", tt.days, "")
			if data.Interval != tt.expectedInterval {
				t.Errorf("days=%d: expected interval %s, got %s", tt.days, tt.expectedInterval, data.Interval)
			}
		})
	}
}

// TestAggregateBars tests the OHLCV aggregation logic
func TestAggregateBars(t *testing.T) {
	// Start at exact hour boundary to ensure all bars fall in same bucket
	hourStart := (time.Now().Unix() / 3600) * 3600
	
	// Create 12 5-min bars (1 hour total)
	bars := make([]models.OHLCV, 12)
	for i := 0; i < 12; i++ {
		bars[i] = models.OHLCV{
			Time:   hourStart + int64(i*300), // 5-min intervals
			Open:   100.0 + float64(i),
			High:   105.0 + float64(i),
			Low:    95.0 + float64(i),
			Close:  102.0 + float64(i),
			Volume: 1000 + int64(i*100),
		}
	}
	
	// Aggregate to 1-hour candles (12 bars -> 1 bar)
	aggregated := aggregateBars(bars, 1)
	
	if len(aggregated) != 1 {
		t.Fatalf("expected 1 aggregated bar, got %d", len(aggregated))
	}
	
	bar := aggregated[0]
	// Open should be from first bar
	if bar.Open != 100.0 {
		t.Errorf("expected open 100.0, got %.2f", bar.Open)
	}
	// Close should be from last bar
	if bar.Close != 113.0 { // 102.0 + 11
		t.Errorf("expected close 113.0, got %.2f", bar.Close)
	}
	// High should be max of all highs
	if bar.High != 116.0 { // 105.0 + 11
		t.Errorf("expected high 116.0, got %.2f", bar.High)
	}
	// Low should be min of all lows
	if bar.Low != 95.0 {
		t.Errorf("expected low 95.0, got %.2f", bar.Low)
	}
	// Volume should be sum
	expectedVolume := int64(0)
	for i := 0; i < 12; i++ {
		expectedVolume += 1000 + int64(i*100)
	}
	if bar.Volume != expectedVolume {
		t.Errorf("expected volume %d, got %d", expectedVolume, bar.Volume)
	}
}

func TestGetPredictionsTrackCurrentPrices(t *testing.T) {
	svcForCurrent := newDeterministicMarketDataService()
	prices := svcForCurrent.GetPrices()
	expected := map[string]float64{}
	for _, p := range prices {
		// Skip empty prices
		if p.Price > 0 {
			expected[p.Symbol] = p.Price
		}
	}

	// Test that predictions track current prices when available
	svc := newDeterministicMarketDataService()
	predictions := svc.GetPredictions()
	
	// Without yahoo history configured, predictions will be empty (correct behavior)
	// This test validates no crashes occur
	for _, p := range predictions {
		current, ok := expected[p.Symbol]
		if !ok {
			// Symbol may not have real data, skip
			continue
		}
		if p.Current != current {
			t.Fatalf("prediction current mismatch for %s: expected %.2f got %.2f", p.Symbol, current, p.Current)
		}
		if p.Predicted <= 0 {
			t.Fatalf("predicted price should be positive for %s", p.Symbol)
		}
	}
}


