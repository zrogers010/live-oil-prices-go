package models

import (
	"encoding/json"
	"testing"
)

func TestPriceJSONRoundTrip(t *testing.T) {
	original := Price{
		Symbol:    "WTI",
		Name:      "WTI Crude Oil",
		Price:     72.45,
		Change:    1.23,
		ChangePct: 1.71,
		High:      73.22,
		Low:       71.89,
		Volume:    1234567,
		UpdatedAt: "2026-03-09T00:00:00Z",
	}

	blob, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	var decoded Price
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if decoded != original {
		t.Fatalf("price round trip mismatch:\norig=%+v\ndecoded=%+v", original, decoded)
	}
}

func TestMarketAnalysisJSONShape(t *testing.T) {
	original := MarketAnalysis{
		Sentiment: "bullish",
		Score:     68,
		Summary:   "Markets are volatile",
		KeyPoints: []string{"OPEC", "Demand"},
		Technical: TechnicalSignals{
			RSI:          58.4,
			MACD:         "bullish",
			Signal:       "buy",
			MovingAvg50:  71.2,
			MovingAvg200: 69.4,
			Trend:        "uptrend",
		},
		UpdatedAt: "2026-03-09T00:00:00Z",
	}

	blob, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	var decoded MarketAnalysis
	if err := json.Unmarshal(blob, &decoded); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if decoded.Sentiment != original.Sentiment ||
		decoded.Score != original.Score ||
		decoded.UpdatedAt != original.UpdatedAt ||
		len(decoded.KeyPoints) != len(original.KeyPoints) ||
		decoded.Technical.Trend != original.Technical.Trend {
		t.Fatalf("analysis JSON mismatch:\norig=%+v\ndecoded=%+v", original, decoded)
	}
}
