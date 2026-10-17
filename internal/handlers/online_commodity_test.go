package handlers

import (
	"live-oil-prices-go/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOnlineCommodityPageRendersPrice renders a commodity page with a real
// (available) price and asserts the page is complete and shows that price.
// Guards against template execution errors that only trigger when a price
// exists (the r9 `.Available` regression).
func TestOnlineCommodityPageRendersPrice(t *testing.T) {
	api := NewAPI(&fakeMarketDataService{
		getPricesFunc: func() []models.Price {
			return []models.Price{{
				Symbol: "WTI", Name: "WTI Crude Oil", Price: 99.04, Change: 1.07,
				ChangePct: 1.09, High: 99.34, Low: 95.29, Volume: 304000,
				UpdatedAt: "2026-10-17T00:00:00Z", Source: "yahoo", Available: true,
			}}
		},
	}, &fakeNewsFeedService{})
	if err := InitCommodityTemplate("../../web/templates/commodity.html"); err != nil {
		t.Fatalf("failed to init commodity template: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /commodity/{symbol}", api.ServeCommodityPage)

	req := httptest.NewRequest("GET", "/commodity/WTI", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	body := w.Body.String()

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(body, "Internal Server Error") || !strings.Contains(strings.TrimSpace(body), "</html>") {
		t.Fatalf("page did not render through </html>; tail: %q", body[max(0, len(body)-200):])
	}
	for _, want := range []string{`id="detailPrice">$99.04<`, "<title>WTI Crude Oil Price Today $99.04", `"offers"`, `"price": "99.04"`} {
		if !strings.Contains(body, want) {
			t.Errorf("expected page to contain %q", want)
		}
	}
	if strings.Contains(body, "$0.00") {
		t.Errorf("page should not contain $0.00")
	}
}
