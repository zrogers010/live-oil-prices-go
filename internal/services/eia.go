package services

import (
	"encoding/json"
	"fmt"
	"io"
	"live-oil-prices-go/internal/models"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

// EIAService fetches the U.S. Energy Information Administration's monthly
// Short-Term Energy Outlook (STEO) and exposes the next ~6 forward months of
// price forecasts per benchmark.
//
// The STEO is the most-cited free institutional outlook for U.S. energy
// markets. Surfacing it next to our on-site model gives users a third-party
// reference point so the page reads as a balanced "what does the model say
// vs. what does the EIA say" rather than a single black-box prediction.
//
// This service requires a free EIA API key (https://www.eia.gov/opendata/).
// Set EIA_API_KEY in the environment to enable. Without a key, all methods
// return empty data and the UI section degrades gracefully (hidden / stub
// message). This keeps deployments without the key unbroken.
type EIAService struct {
	client *http.Client
	apiKey string

	mu        sync.RWMutex
	cache     map[string]models.ConsensusForecast
	updatedAt time.Time
}

// eiaSymbol maps our internal symbol id to the EIA STEO series id and the
// human-readable unit string we surface in the UI.
//
// Series IDs verified against the EIA STEO browser
// (https://www.eia.gov/opendata/browser/steo). The "PUUS" / "EUUS" suffix
// distinguishes spot prices from futures expectations; we use spot for the
// crude benchmarks (matches the live spot prices on this site) and Henry
// Hub spot for natural gas.
var eiaSeries = []struct {
	internal string
	series   string
	unit     string
}{
	{"WTI", "WTIPUUS", "USD/barrel"},
	{"BRENT", "BREPUUS", "USD/barrel"},
	{"NATGAS", "NGHHMCF", "USD/MMBtu"},
}

const (
	eiaSTEOURL  = "https://api.eia.gov/v2/steo/data/"
	eiaSourceID = "EIA STEO"
	// We surface 6 forward months — the STEO publishes ~24 months ahead but
	// the further-out values are increasingly speculative; 6 months is the
	// industry-standard "actionable" window.
	eiaForwardMonths = 6
	// Refresh once a day. The STEO is published monthly, so daily polling
	// is plenty fresh and very low cost.
	eiaRefreshInterval = 24 * time.Hour
	// First refresh delay on cold start so we don't block server startup
	// on a network call that may need to time out.
	eiaInitialDelay = 30 * time.Second
)

// NewEIAService reads EIA_API_KEY from the environment and starts a daily
// refresh goroutine if a key is configured. Returns a non-nil service
// either way; methods on a key-less service simply return empty data.
func NewEIAService() *EIAService {
	svc := &EIAService{
		client: &http.Client{Timeout: 20 * time.Second},
		apiKey: os.Getenv("EIA_API_KEY"),
		cache:  make(map[string]models.ConsensusForecast),
	}

	if svc.apiKey == "" {
		log.Println("[eia] EIA_API_KEY not set — institutional outlook section will be hidden. Set EIA_API_KEY to enable.")
		return svc
	}

	go svc.refreshLoop()
	return svc
}

func (s *EIAService) refreshLoop() {
	time.Sleep(eiaInitialDelay)
	s.refresh()
	t := time.NewTicker(eiaRefreshInterval)
	defer t.Stop()
	for range t.C {
		s.refresh()
	}
}

func (s *EIAService) refresh() {
	out := make(map[string]models.ConsensusForecast)
	for _, sym := range eiaSeries {
		f, err := s.fetchSeries(sym.internal, sym.series, sym.unit)
		if err != nil {
			log.Printf("[eia] %s (%s) refresh failed: %v", sym.internal, sym.series, err)
			continue
		}
		out[sym.internal] = f
	}
	if len(out) == 0 {
		// Don't blow away an existing cache on a transient failure.
		log.Println("[eia] refresh produced 0 series; keeping previous cache")
		return
	}
	s.mu.Lock()
	s.cache = out
	s.updatedAt = time.Now()
	s.mu.Unlock()
	log.Printf("[eia] refreshed %d series", len(out))
}

// fetchSeries pulls the next eiaForwardMonths months of forecast values for
// a single STEO series. The API returns historical and forecast points in
// the same response; we filter to dates >= today so we only surface forward
// expectations.
func (s *EIAService) fetchSeries(internal, series, unit string) (models.ConsensusForecast, error) {
	now := time.Now().UTC()
	start := now.Format("2006-01")
	end := now.AddDate(0, eiaForwardMonths+1, 0).Format("2006-01")

	q := url.Values{}
	q.Set("api_key", s.apiKey)
	q.Set("frequency", "monthly")
	q.Set("data[0]", "value")
	q.Set("facets[seriesId][]", series)
	q.Set("start", start)
	q.Set("end", end)
	q.Set("sort[0][column]", "period")
	q.Set("sort[0][direction]", "asc")
	q.Set("offset", "0")
	q.Set("length", strconv.Itoa(eiaForwardMonths+2))

	reqURL := eiaSTEOURL + "?" + q.Encode()
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return models.ConsensusForecast{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "liveoilprices.com/1.0 (+https://liveoilprices.com)")

	resp, err := s.client.Do(req)
	if err != nil {
		return models.ConsensusForecast{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return models.ConsensusForecast{}, fmt.Errorf("eia api status %d: %s", resp.StatusCode, string(body))
	}

	var raw eiaSTEOResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return models.ConsensusForecast{}, fmt.Errorf("decode: %w", err)
	}

	months := make([]models.ConsensusMonthly, 0, len(raw.Response.Data))
	currentPeriod := now.Format("2006-01")
	for _, d := range raw.Response.Data {
		// Only surface forward months — the STEO endpoint returns historical
		// values too and we don't want them muddying the "outlook" frame.
		if d.Period < currentPeriod {
			continue
		}
		v, err := d.Value.Float64()
		if err != nil {
			continue
		}
		months = append(months, models.ConsensusMonthly{Period: d.Period, Value: v})
		if len(months) >= eiaForwardMonths {
			break
		}
	}

	if len(months) == 0 {
		return models.ConsensusForecast{}, fmt.Errorf("no forward months in response")
	}

	return models.ConsensusForecast{
		Symbol:      internal,
		Source:      eiaSourceID,
		SourceURL:   "https://www.eia.gov/outlooks/steo/",
		ReleaseDate: time.Now().UTC().Format(time.RFC3339),
		Unit:        unit,
		Months:      months,
	}, nil
}

// GetAll returns a copy of every cached consensus forecast in a stable
// (WTI, BRENT, NATGAS, ...) order. Returns an empty slice when no API key
// is configured or the cache hasn't loaded yet.
func (s *EIAService) GetAll() []models.ConsensusForecast {
	if s == nil || s.apiKey == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.ConsensusForecast, 0, len(s.cache))
	for _, sym := range eiaSeries {
		if v, ok := s.cache[sym.internal]; ok {
			out = append(out, v)
		}
	}
	return out
}

// Get returns a single consensus forecast by symbol, or false when not
// cached / API key not configured.
func (s *EIAService) Get(symbol string) (models.ConsensusForecast, bool) {
	if s == nil || s.apiKey == "" {
		return models.ConsensusForecast{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.cache[symbol]
	return v, ok
}

// eiaSTEOResponse mirrors the EIA v2 API JSON envelope. We only model the
// fields we actually use — the full response carries facet metadata,
// pagination, etc. that we don't need.
type eiaSTEOResponse struct {
	Response struct {
		Data []struct {
			Period string      `json:"period"` // "2026-05"
			Value  json.Number `json:"value"`
		} `json:"data"`
	} `json:"response"`
}
