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
