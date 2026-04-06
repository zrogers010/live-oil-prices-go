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

type YahooFinanceService struct {
	client *http.Client
	mu     sync.RWMutex
	prices map[string]models.Price
}

func NewYahooFinanceService() *YahooFinanceService {
	svc := &YahooFinanceService{
		client: &http.Client{Timeout: 10 * time.Second},
		prices: make(map[string]models.Price),
	}
	svc.refresh()
	go svc.loop()
	return svc
}

func (s *YahooFinanceService) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.refresh()
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
	prevClose := meta.ChartPreviousClose
	change := price - prevClose
	changePct := 0.0
	if prevClose != 0 {
		changePct = (change / prevClose) * 100
	}

	var dayHigh, dayLow float64
	var volume int64

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
	}

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
