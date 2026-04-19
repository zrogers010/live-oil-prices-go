package services

import (
	"live-oil-prices-go/internal/models"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestEIANoKeyDegradesGracefully ensures the service is safe to construct
// and call when EIA_API_KEY is unset. This is the production default for
// most deployments, so it must never panic and must always return empty.
func TestEIANoKeyDegradesGracefully(t *testing.T) {
	// Force unset for this test even if the developer has it locally.
	prev := os.Getenv("EIA_API_KEY")
	os.Unsetenv("EIA_API_KEY")
	defer os.Setenv("EIA_API_KEY", prev)

	svc := NewEIAService()
	if svc == nil {
		t.Fatal("expected non-nil service even without API key")
	}
	if got := svc.GetAll(); len(got) != 0 {
		t.Fatalf("expected empty result without API key, got %d", len(got))
	}
	if _, ok := svc.Get("WTI"); ok {
		t.Fatal("expected Get(WTI) to be false without API key")
	}
}

// TestEIAFetchSeriesParsesResponse validates the EIA STEO JSON envelope
// parsing against a representative payload. Uses an httptest server so
// we don't hit the real EIA API in tests.
func TestEIAFetchSeriesParsesResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// EIA returns a top-level {"response": {"data": [...]}} envelope.
		// We include a historical month before the current month to verify
		// it gets filtered out of the forward-month output.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "response": {
		    "data": [
		      {"period": "2020-01", "value": "61.5"},
		      {"period": "9999-01", "value": "92.10"},
		      {"period": "9999-02", "value": "93.20"},
		      {"period": "9999-03", "value": "94.40"}
		    ]
		  }
		}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := &EIAService{
		client: ts.Client(),
		apiKey: "test-key",
		cache:  make(map[string]models.ConsensusForecast),
	}
	// Override the URL by hand: fetchSeries hardcodes eiaSTEOURL, so we
	// can't easily inject a custom host. Instead exercise the parse by
	// pointing the client's transport at the test server.
	// The client.Do will still hit eiaSTEOURL, so use a roundtripper
	// rewrite below.
	svc.client = &http.Client{Transport: rewriteTransport{target: ts.URL}}

	got, err := svc.fetchSeries("WTI", "WTIPUUS", "USD/barrel")
	if err != nil {
		t.Fatalf("fetchSeries: %v", err)
	}
	if got.Symbol != "WTI" {
		t.Fatalf("symbol: want WTI, got %s", got.Symbol)
	}
	if got.Source != "EIA STEO" {
		t.Fatalf("source: want EIA STEO, got %s", got.Source)
	}
	if got.Unit != "USD/barrel" {
		t.Fatalf("unit: want USD/barrel, got %s", got.Unit)
	}
	if len(got.Months) == 0 {
		t.Fatal("expected at least one forward month")
	}
	// The 2020-01 historical entry must have been filtered.
	for _, m := range got.Months {
		if m.Period < "2020-02" {
			t.Errorf("historical period %s leaked into forward months", m.Period)
		}
	}
}

// rewriteTransport rewrites every request URL to point at `target`,
// preserving the path and query. Lets us point a hardcoded HTTPS endpoint
// at an httptest server without changing production code.
type rewriteTransport struct{ target string }

func (rt rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.URL, _ = clone.URL.Parse(rt.target + r.URL.Path + "?" + r.URL.RawQuery)
	clone.Host = clone.URL.Host
	clone.RequestURI = ""
	return http.DefaultTransport.RoundTrip(clone)
}
