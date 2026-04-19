package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"live-oil-prices-go/internal/models"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PageData is the unified template payload shared by every server-rendered
// page (home, charts, forecast, news). It carries SEO metadata, JSON-LD
// structured data, the active page id (for nav highlighting), and any
// server-rendered market data the page wants to embed in the initial HTML.
type PageData struct {
	// Identity / navigation
	ActivePage string // "home" | "charts" | "forecast" | "news"
	HideTicker bool

	// SEO
	Title         string
	Description   string
	Keywords      string
	Canonical     string
	OGTitle       string
	OGDescription string
	OGType        string
	SchemaType    string

	// Each entry is a JSON-encodable value; the layout marshals them
	// inside <script type="application/ld+json"> tags.
	StructuredData []any

	// Server-rendered market data embedded into initial HTML.
	// All of these are derived from MarketDataClient at request time,
	// so crawlers and slow-network users see real numbers immediately.
	Prices     []priceView
	CardPrices []priceView // subset rendered as the headline price cards
	HeroWTI    *priceView
	Forecasts  []forecastView
	Consensus  []consensusView
}

type priceView struct {
	Symbol          string
	SymbolPrefix    string
	Name            string
	Contract        string
	Source          string
	Price           float64
	Change          float64
	ChangePct       float64
	High            float64
	Low             float64
	Volume          int64
	VolumeFormatted string
	UpdatedAt       string
	IsPositive      bool
	Sign            string
}

type forecastView struct {
	Symbol          string
	Name            string
	Current         float64
	Predicted       float64
	Delta           float64
	DeltaPct        float64
	DeltaSign       string
	Direction       string
	DirClass        string
	DirectionLabel  string // human-friendly: "Bullish bias", "Bearish bias", "Mixed signals"
	Arrow           string
	Timeframe       string
	ConfidencePct   int
	ConfidenceLabel string
	Analysis        string
	Model           string
	SourceLabel     string

	// Signal-stack chips. Each chip has a label and a tone class
	// ("bullish" | "bearish" | "neutral") so the template can colour them.
	Signals []signalChip

	// Backtest credibility block.
	HasBacktest      bool
	BacktestSteps    int
	MAPEPct          float64 // 4.2 not 0.042
	NaiveMAPEPct     float64
	SkillPct         float64 // signed; positive = beats naive
	BacktestVerdict  string  // "Beats the naive baseline by 18%"
	BacktestSentence string  // longer sentence for screen readers / SEO
}

type signalChip struct {
	Label string // "Trend"
	Value string // "Uptrend"
	Tone  string // "bullish" | "bearish" | "neutral"
}

type consensusView struct {
	Symbol      string
	Name        string
	Source      string
	SourceURL   string
	Unit        string
	ReleasedAt  string // human-friendly: "Released Apr 9, 2026"
	Months      []consensusMonthView
}

type consensusMonthView struct {
	Period   string  // "May 2026"
	RawValue float64
	Value    string  // "$72.45"
}

// Symbols rendered as the headline "price cards" in the hero/markets section.
// Mirrors the CARD_SYMBOLS list in web/src/app.ts so SSR markup matches CSR.
var cardSymbols = []string{"WTI", "BRENT", "NATGAS", "HEATING", "RBOB", "OPEC"}

// Page templates, parsed once at startup. Each entry is a fully composed
// (layout + partials + page) template, ready to be executed against PageData.
var pageTemplates = map[string]*template.Template{}

// InitPageTemplates parses the layout, shared partials, and per-page
// content templates from disk. Must be called once at startup.
func InitPageTemplates(dir string) error {
	layout := filepath.Join(dir, "layout.html")
	partials := []string{
		filepath.Join(dir, "partials", "nav.html"),
		filepath.Join(dir, "partials", "footer.html"),
		filepath.Join(dir, "partials", "price_grid.html"),
		filepath.Join(dir, "partials", "forecast_grid.html"),
	}

	funcs := template.FuncMap{
		"safeJSON": func(v any) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("{}")
			}
			return template.JS(b)
		},
	}

	pages := []string{"home", "charts", "forecast", "news"}
	for _, name := range pages {
		files := append([]string{layout, filepath.Join(dir, "pages", name+".html")}, partials...)
		t, err := template.New("layout").Funcs(funcs).ParseFiles(files...)
		if err != nil {
			return fmt.Errorf("parse page %s: %w", name, err)
		}
		pageTemplates[name] = t
	}
	return nil
}

func (a *API) renderPage(w http.ResponseWriter, r *http.Request, name string, data *PageData) {
	tmpl, ok := pageTemplates[name]
	if !ok {
		http.Error(w, "page template not initialized", http.StatusInternalServerError)
		return
	}

	a.populateMarketData(data)

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=15")
	_, _ = buf.WriteTo(w)
}

// populateMarketData fills the live data fields (prices, hero, forecasts)
// from the market service so the initial HTML is fully rendered for
// crawlers and first-paint. Safe to call even if some sub-services error
// out — empty lists just degrade gracefully in the templates.
func (a *API) populateMarketData(data *PageData) {
	if a.market == nil {
		return
	}

	prices := a.market.GetPrices()
	views := make([]priceView, 0, len(prices))
	for _, p := range prices {
		views = append(views, toPriceView(p))
	}
	data.Prices = views

	// Card subset, ordered the same way the JS does it.
	cards := make([]priceView, 0, len(cardSymbols))
	byID := map[string]priceView{}
	for _, v := range views {
		byID[v.Symbol] = v
	}
	for _, sym := range cardSymbols {
		if v, ok := byID[sym]; ok {
			cards = append(cards, v)
		}
	}
	data.CardPrices = cards

	if v, ok := byID["WTI"]; ok {
		data.HeroWTI = &v
	}

	preds := a.market.GetPredictions()
	fviews := make([]forecastView, 0, len(preds))
	for _, p := range preds {
		fviews = append(fviews, toForecastView(p))
	}
	data.Forecasts = fviews

	consensus := a.market.GetConsensusForecasts()
	cviews := make([]consensusView, 0, len(consensus))
	for _, c := range consensus {
		cviews = append(cviews, toConsensusView(c))
	}
	data.Consensus = cviews
}

var consensusNames = map[string]string{
	"WTI":    "WTI Crude Oil",
	"BRENT":  "Brent Crude Oil",
	"NATGAS": "Henry Hub Natural Gas",
}

func toConsensusView(c models.ConsensusForecast) consensusView {
	name := consensusNames[c.Symbol]
	if name == "" {
		name = c.Symbol
	}
	released := ""
	if t, err := time.Parse(time.RFC3339, c.ReleaseDate); err == nil {
		released = "Released " + t.Format("Jan 2, 2006")
	}
	months := make([]consensusMonthView, 0, len(c.Months))
	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	for _, m := range c.Months {
		// "2026-05" → "May 2026"
		label := m.Period
		if len(m.Period) >= 7 {
			y := m.Period[:4]
			mm := m.Period[5:7]
			if idx, err := strconv.Atoi(mm); err == nil && idx >= 1 && idx <= 12 {
				label = monthNames[idx-1] + " " + y
			}
		}
		months = append(months, consensusMonthView{
			Period:   label,
			RawValue: m.Value,
			Value:    fmt.Sprintf("$%.2f", m.Value),
		})
	}
	return consensusView{
		Symbol:     c.Symbol,
		Name:       name,
		Source:     c.Source,
		SourceURL:  c.SourceURL,
		Unit:       c.Unit,
		ReleasedAt: released,
		Months:     months,
	}
}

func toPriceView(p models.Price) priceView {
	v := priceView{
		Symbol:          p.Symbol,
		SymbolPrefix:    safePrefix(p.Symbol),
		Name:            p.Name,
		Contract:        p.Contract,
		Source:          p.Source,
		Price:           p.Price,
		Change:          p.Change,
		ChangePct:       p.ChangePct,
		High:            p.High,
		Low:             p.Low,
		Volume:          p.Volume,
		VolumeFormatted: formatVolume(p.Volume),
		UpdatedAt:       p.UpdatedAt,
		IsPositive:      p.Change >= 0,
	}
	if v.IsPositive {
		v.Sign = "+"
	}
	return v
}

func toForecastView(p models.Prediction) forecastView {
	delta := p.Predicted - p.Current
	pct := 0.0
	if p.Current != 0 {
		pct = (delta / p.Current) * 100
	}
	dirClass := "neutral"
	arrow := "→"
	dirLabel := "Mixed signals"
	switch p.Direction {
	case "bullish":
		dirClass, arrow, dirLabel = "bullish", "↑", "Bullish bias"
	case "bearish":
		dirClass, arrow, dirLabel = "bearish", "↓", "Bearish bias"
	}
	confPct := int(p.Confidence*100 + 0.5)
	confLabel := "Unavailable"
	switch {
	case confPct >= 70:
		confLabel = "High"
	case confPct >= 50:
		confLabel = "Moderate"
	case confPct > 0:
		confLabel = "Low"
	}
	model := p.Model
	if model == "" {
		model = "statistical"
	}
	deltaSign := ""
	if delta >= 0 {
		deltaSign = "+"
	}
	srcLabel := "Statistical model"
	if p.Source == "yahoo" {
		srcLabel = "Yahoo Finance history"
	} else if p.Source == "estimate" {
		srcLabel = "Estimated baseline"
	}

	signals := buildSignalChips(p)

	// Backtest credibility block. We surface the model's actual recent
	// accuracy and skill-vs-naive so the user has an evidence-based way
	// to interpret each card, instead of having to trust an opaque
	// "confidence" number.
	hasBT := p.BacktestSteps > 0 && p.MAPE > 0
	mapePct := p.MAPE * 100
	naivePct := p.NaiveMAPE * 100
	skillPct := p.Skill * 100
	verdict := ""
	sentence := ""
	if hasBT {
		switch {
		case skillPct >= 10:
			verdict = fmt.Sprintf("Beats naive baseline by %.0f%%", skillPct)
		case skillPct <= -5:
			verdict = fmt.Sprintf("Underperforms naive baseline by %.0f%%", -skillPct)
		default:
			verdict = "Roughly matches naive baseline"
		}
		sentence = fmt.Sprintf(
			"Out-of-sample backtest over the last %d trading sessions: %.1f%% MAPE on %s forecasts vs %.1f%% MAPE for a naive 'no change' baseline.",
			p.BacktestSteps, mapePct, p.Timeframe, naivePct,
		)
	}

	return forecastView{
		Symbol:           p.Symbol,
		Name:             p.Name,
		Current:          p.Current,
		Predicted:        p.Predicted,
		Delta:            delta,
		DeltaPct:         pct,
		DeltaSign:        deltaSign,
		Direction:        p.Direction,
		DirClass:         dirClass,
		DirectionLabel:   dirLabel,
		Arrow:            arrow,
		Timeframe:        p.Timeframe,
		ConfidencePct:    confPct,
		ConfidenceLabel:  confLabel,
		Analysis:         p.Analysis,
		Model:            model,
		SourceLabel:      srcLabel,
		Signals:          signals,
		HasBacktest:      hasBT,
		BacktestSteps:    p.BacktestSteps,
		MAPEPct:          mapePct,
		NaiveMAPEPct:     naivePct,
		SkillPct:         skillPct,
		BacktestVerdict:  verdict,
		BacktestSentence: sentence,
	}
}

// buildSignalChips assembles the four-chip "signal stack" — Trend, RSI, MACD
// and the 50/200 DMA configuration — used by the Outlook & Signals card.
//
// Each chip carries a tone (bullish/bearish/neutral) so the template can
// colour them consistently with the direction badge. We deliberately keep
// the chip values short (1-2 words) so the row reads as a glance-able
// dashboard rather than another paragraph of analysis.
func buildSignalChips(p models.Prediction) []signalChip {
	chips := make([]signalChip, 0, 4)

	// Trend chip — driven by the 50/200 DMA configuration label.
	trendTone := "neutral"
	trendValue := "Sideways"
	switch p.TrendLabel {
	case "uptrend":
		trendTone, trendValue = "bullish", "Uptrend"
	case "downtrend":
		trendTone, trendValue = "bearish", "Downtrend"
	}
	chips = append(chips, signalChip{Label: "Trend", Value: trendValue, Tone: trendTone})

	// RSI / momentum chip — keep value short (one word) since the label
	// already says "Momentum"; the numeric reading goes in the title attr
	// for power users on hover (handled in the template by aria-label).
	rsiTone := "neutral"
	rsiValue := fmt.Sprintf("RSI %.0f", p.RSI14)
	switch p.RSILabel {
	case "bullish":
		rsiTone, rsiValue = "bullish", "Bullish"
	case "overbought":
		rsiTone, rsiValue = "bearish", "Overbought"
	case "bearish":
		rsiTone, rsiValue = "bearish", "Bearish"
	case "oversold":
		rsiTone, rsiValue = "bullish", "Oversold"
	}
	chips = append(chips, signalChip{Label: "Momentum", Value: rsiValue, Tone: rsiTone})

	// MACD chip.
	macdTone := "neutral"
	macdValue := "Flat"
	switch p.MACDLabel {
	case "above signal (bullish)":
		macdTone, macdValue = "bullish", "Above signal"
	case "below signal (bearish)":
		macdTone, macdValue = "bearish", "Below signal"
	}
	chips = append(chips, signalChip{Label: "MACD", Value: macdValue, Tone: macdTone})

	// MA configuration chip — the long-term regime.
	maTone := "neutral"
	maValue := "Range-bound"
	switch {
	case strings.Contains(p.MAConfig, "golden-cross"):
		maTone, maValue = "bullish", "Golden cross"
	case strings.Contains(p.MAConfig, "above 200DMA"):
		maTone, maValue = "bullish", "50DMA > 200DMA"
	case strings.Contains(p.MAConfig, "death-cross"):
		maTone, maValue = "bearish", "Death cross"
	case strings.Contains(p.MAConfig, "below 200DMA"):
		maTone, maValue = "bearish", "50DMA < 200DMA"
	}
	chips = append(chips, signalChip{Label: "Regime", Value: maValue, Tone: maTone})

	return chips
}

func formatVolume(v int64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(v)/1_000_000)
	}
	if v >= 1_000 {
		return fmt.Sprintf("%dK", v/1_000)
	}
	return fmt.Sprintf("%d", v)
}

func safePrefix(s string) string {
	if len(s) >= 2 {
		return strings.ToUpper(s[:2])
	}
	return strings.ToUpper(s)
}

// ─── Page handlers ──────────────────────────────────────────────────────

func (a *API) ServeHome(w http.ResponseWriter, r *http.Request) {
	data := &PageData{
		ActivePage:    "home",
		Title:         "Live Oil Prices — Real-Time Crude Oil, WTI, Brent & Energy Market Data",
		Description:   "Live oil prices updated every 15 seconds. Track WTI crude, Brent crude, natural gas, heating oil, RBOB gasoline, and OPEC basket prices with interactive charts and breaking energy market news.",
		Keywords:      "oil prices, crude oil price, WTI price, Brent crude price, live oil prices, oil price today, natural gas price, heating oil, RBOB gasoline, OPEC, energy market, oil chart, oil news",
		Canonical:     "https://liveoilprices.com/",
		OGTitle:       "Live Oil Prices — Real-Time Crude Oil & Energy Market Data",
		OGDescription: "Track WTI, Brent, natural gas, and 10+ energy commodities with live prices, interactive charts, and breaking market news.",
		SchemaType:    "WebSite",
		StructuredData: []any{
			map[string]any{
				"@context":    "https://schema.org",
				"@type":       "Organization",
				"name":        "Live Oil Prices",
				"url":         "https://liveoilprices.com",
				"description": "Real-time energy market data, interactive oil price charts, statistical price forecasts, and breaking energy news.",
			},
			map[string]any{
				"@context":    "https://schema.org",
				"@type":       "WebSite",
				"name":        "Live Oil Prices",
				"url":         "https://liveoilprices.com",
				"description": "Live crude oil prices, energy market charts, statistical price forecasts and breaking oil and gas news.",
				"potentialAction": map[string]any{
					"@type":       "SearchAction",
					"target":      "https://liveoilprices.com/commodity/{search_term_string}",
					"query-input": "required name=search_term_string",
				},
			},
		},
	}
	a.renderPage(w, r, "home", data)
}

func (a *API) ServeCharts(w http.ResponseWriter, r *http.Request) {
	data := &PageData{
		ActivePage:    "charts",
		Title:         "Live Oil Price Charts — WTI, Brent, Natural Gas & RBOB Candlestick Charts",
		Description:   "Interactive candlestick oil price charts for WTI crude, Brent crude, natural gas, heating oil, RBOB gasoline and OPEC basket. Switch between 1-week, 1-month, 3-month, 6-month and 1-year timeframes with volume analysis.",
		Keywords:      "oil price chart, crude oil chart, WTI chart, Brent crude chart, natural gas chart, heating oil chart, RBOB chart, candlestick chart, oil price history",
		Canonical:     "https://liveoilprices.com/charts",
		OGTitle:       "Live Oil Price Charts — Interactive WTI, Brent & Energy Charts",
		OGDescription: "Interactive candlestick charts for WTI, Brent, natural gas and more, with multi-timeframe lookbacks and volume analysis.",
		StructuredData: []any{
			breadcrumbJSONLD([][2]string{{"Home", "https://liveoilprices.com/"}, {"Oil Charts", "https://liveoilprices.com/charts"}}),
			faqJSONLD([][2]string{
				{"How often are these oil price charts updated?",
					"Spot prices refresh every 15 seconds. Daily candlestick charts use Yahoo Finance end-of-day data and refresh hourly. The streaming WTI hero chart on the homepage uses real-time Pyth Network ticks aggregated into 1-minute candles."},
				{"What's the difference between WTI and Brent crude?",
					"WTI (West Texas Intermediate) is lighter and sweeter than Brent and is delivered at Cushing, Oklahoma. Brent is sourced from the North Sea and is the global benchmark used to price about two-thirds of the world's crude. The Brent–WTI spread reflects relative supply and demand between U.S. and international markets."},
				{"Why does the chart sometimes show no data on weekends?",
					"Energy futures trade Sunday evening through Friday afternoon (U.S. Eastern Time). When markets are closed, charts display the last completed session and the homepage hero chart automatically falls back to the most recent intraday session."},
				{"Can I get a chart for a specific contract month?",
					"The charts on this page show the front-month contract for each commodity, which is the most actively traded. For deeper details on each benchmark, visit the dedicated commodity pages such as /commodity/WTI or /commodity/BRENT."},
			}),
		},
	}
	a.renderPage(w, r, "charts", data)
}

func (a *API) ServeForecast(w http.ResponseWriter, r *http.Request) {
	data := &PageData{
		ActivePage:    "forecast",
		Title:         "Oil Price Outlook & Technical Signals — WTI, Brent, Natural Gas",
		Description:   "Multi-signal oil price outlook for WTI, Brent, natural gas and heating oil. Trend, RSI, MACD and 50/200-day moving-average regime stacked alongside a damped-Holt 7-day model forecast and an institutional EIA Short-Term Energy Outlook reference.",
		Keywords:      "oil price outlook, WTI outlook, Brent outlook, oil technical analysis, RSI MACD oil, 50 day moving average oil, oil price signals, EIA STEO forecast, natural gas outlook",
		Canonical:     "https://liveoilprices.com/forecast",
		OGTitle:       "Oil Price Outlook & Technical Signals — WTI, Brent & Energy",
		OGDescription: "Stacked technical signals (Trend, RSI, MACD, 50/200 DMA) plus a damped-Holt forecast and EIA STEO reference for every major oil benchmark.",
		StructuredData: []any{
			breadcrumbJSONLD([][2]string{{"Home", "https://liveoilprices.com/"}, {"Outlook", "https://liveoilprices.com/forecast"}}),
			faqJSONLD([][2]string{
				{"How is this different from a traditional price forecast?",
					"Instead of leading with a single point prediction, each card stacks four independent technical signals — long-term trend, RSI momentum, MACD cross, and the 50/200-day moving-average regime — alongside a damped-Holt statistical forecast. You get to see whether the signals agree before you read the dollar number."},
				{"How accurate are the model forecasts?",
					"Each card shows the model's actual recent out-of-sample accuracy — the rolling 30-step backtest MAPE — and compares it to a naive 'no change' baseline. A 4% MAPE on a 7-day oil forecast is a strong result; a model that can't beat the naive baseline is honestly flagged as such."},
				{"Where does the institutional outlook come from?",
					"The institutional outlook is sourced directly from the U.S. Energy Information Administration's monthly Short-Term Energy Outlook (STEO), which publishes 12+ months of forward price expectations for WTI, Brent and Henry Hub natural gas. Every figure links to the original EIA release."},
				{"Why do you publish low-confidence outlooks at all?",
					"Because honest signal is more useful than a fabricated headline number. When trend, momentum and the regime disagree, that disagreement is itself the signal — it tells you the market hasn't picked a side, and we'd rather show you that than paper over it with a confident-looking arrow."},
				{"Can I use this to trade?",
					"No. The signals and forecasts on this page are for informational purposes only. They are not personalised financial advice and should not be the basis for any trading decision."},
			}),
		},
	}
	a.renderPage(w, r, "forecast", data)
}

func (a *API) ServeNews(w http.ResponseWriter, r *http.Request) {
	data := &PageData{
		ActivePage:    "news",
		Title:         "Energy Market News — Live Oil, Gas, OPEC & Refining Headlines",
		Description:   "Breaking energy market news covering crude oil, natural gas, OPEC+ decisions, refining and global energy markets. Aggregated from Reuters, Bloomberg, the EIA and 50+ sources, updated continuously.",
		Keywords:      "oil news, energy news, crude oil news, OPEC news, natural gas news, oil market news, energy market news today",
		Canonical:     "https://liveoilprices.com/news",
		OGTitle:       "Energy Market News — Oil, Gas & OPEC Headlines",
		OGDescription: "Breaking oil, gas and energy news from Reuters, Bloomberg, the EIA and 50+ sources, updated continuously.",
		StructuredData: []any{
			breadcrumbJSONLD([][2]string{{"Home", "https://liveoilprices.com/"}, {"News", "https://liveoilprices.com/news"}}),
			faqJSONLD([][2]string{
				{"Where do these articles come from?",
					"Articles are aggregated from major financial wire services (Reuters, Bloomberg, AP), specialist energy publications, the U.S. Energy Information Administration (EIA), OPEC press releases and a curated list of global energy outlets."},
				{"How often is the news feed refreshed?",
					"The feed is refreshed continuously throughout the trading day. New stories typically appear within a few minutes of publication."},
				{"Can I get news for a specific commodity?",
					"For commodity-specific news, visit each benchmark's dedicated page — for example, /commodity/WTI, /commodity/BRENT or /commodity/NATGAS."},
			}),
		},
	}
	a.renderPage(w, r, "news", data)
}

// ─── JSON-LD helpers ────────────────────────────────────────────────────

func breadcrumbJSONLD(items [][2]string) map[string]any {
	list := make([]map[string]any, 0, len(items))
	for i, it := range items {
		list = append(list, map[string]any{
			"@type":    "ListItem",
			"position": i + 1,
			"name":     it[0],
			"item":     it[1],
		})
	}
	return map[string]any{
		"@context":        "https://schema.org",
		"@type":           "BreadcrumbList",
		"itemListElement": list,
	}
}

func faqJSONLD(qa [][2]string) map[string]any {
	entities := make([]map[string]any, 0, len(qa))
	for _, q := range qa {
		entities = append(entities, map[string]any{
			"@type": "Question",
			"name":  q[0],
			"acceptedAnswer": map[string]any{
				"@type": "Answer",
				"text":  q[1],
			},
		})
	}
	return map[string]any{
		"@context":   "https://schema.org",
		"@type":      "FAQPage",
		"mainEntity": entities,
	}
}
