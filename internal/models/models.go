package models

type Price struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"changePct"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Volume    int64   `json:"volume"`
	UpdatedAt string  `json:"updatedAt"`
	Contract  string  `json:"contract,omitempty"`
	Source    string  `json:"source,omitempty"`
}

type OHLCV struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

// PythCandle is a 1-minute OHLC bar derived from streaming Pyth Network
// ticks. It deliberately omits volume — Pyth aggregates publishers, not
// trades, so a "volume" number wouldn't be meaningful.
type PythCandle struct {
	Time  int64   `json:"time"`  // unix seconds at the start of the bucket
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
	Ticks int     `json:"ticks"` // number of Pyth publishes folded into this bar
}

// HeroChart is the response payload for the homepage hero chart endpoint.
// It abstracts away the live-vs-historical decision: during market hours
// the server returns a streaming 1-minute Pyth series; off-hours it falls
// back to the prior session's intraday Yahoo bars. The frontend just
// renders whatever .bars contains and shows the right header text based
// on .mode.
type HeroChart struct {
	Symbol      string       `json:"symbol"`
	Mode        string       `json:"mode"`                  // "live" | "prior-session"
	Interval    string       `json:"interval"`              // "1m" | "5m" | ...
	SessionDate string       `json:"sessionDate,omitempty"` // YYYY-MM-DD ET, set when mode="prior-session"
	UpdatedAt   string       `json:"updatedAt,omitempty"`   // RFC3339, last bar's wall-clock time
	Source      string       `json:"source"`                // "pyth" | "yahoo"
	Bars        []PythCandle `json:"bars"`
}

type ChartData struct {
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name"`
	Interval string  `json:"interval"`
	Data     []OHLCV `json:"data"`
}

type NewsArticle struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Content     string `json:"content"`
	Source      string `json:"source"`
	SourceURL   string `json:"sourceUrl"`
	Category    string `json:"category"`
	PublishedAt string `json:"publishedAt"`
	ImageURL    string `json:"imageUrl"`
	ReadTime    string `json:"readTime"`
}

type Prediction struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	Current       float64 `json:"current"`
	Predicted     float64 `json:"predicted"`
	PredictedLow  float64 `json:"predictedLow,omitempty"`
	PredictedHigh float64 `json:"predictedHigh,omitempty"`
	Timeframe     string  `json:"timeframe"`
	Confidence    float64 `json:"confidence"`
	Direction     string  `json:"direction"`
	Analysis      string  `json:"analysis"`
	Model         string  `json:"model,omitempty"`  // e.g. "holt-damped+rsi/macd" or "fallback"
	Source        string  `json:"source,omitempty"` // data source: "yahoo" or "estimate"
	Disclaimer    string  `json:"disclaimer,omitempty"`

	// Signal-stack fields powering the "Outlook & Technical Signals" view.
	// These let the UI lead with multi-indicator evidence rather than a
	// single point forecast that's inherently noisy on multi-day horizons.
	TrendLabel string  `json:"trendLabel,omitempty"` // "uptrend"|"downtrend"|"sideways"
	RSI14      float64 `json:"rsi14,omitempty"`
	RSILabel   string  `json:"rsiLabel,omitempty"`  // "overbought"|"bullish"|"neutral"|"bearish"|"oversold"
	MACDHist   float64 `json:"macdHist,omitempty"`
	MACDLabel  string  `json:"macdLabel,omitempty"` // "above signal (bullish)"|"below signal (bearish)"|"flat"
	MAConfig   string  `json:"maConfig,omitempty"`  // "50DMA above 200DMA (bullish cross)" etc.

	// Backtest credibility — surfaced so users can see the model's actual
	// out-of-sample track record instead of a single confidence number.
	MAPE          float64 `json:"mape,omitempty"`          // 0.04 = 4%
	NaiveMAPE     float64 `json:"naiveMape,omitempty"`     // baseline "no change" MAPE
	Skill         float64 `json:"skill,omitempty"`         // 1 - mape/naiveMape
	BacktestSteps int     `json:"backtestSteps,omitempty"` // # of held-out forecasts averaged
}

// ConsensusForecast holds an institutional outlook (e.g. EIA Short-Term
// Energy Outlook) for a single benchmark. Used to give users a third-party
// reference against the on-site statistical model.
type ConsensusForecast struct {
	Symbol      string             `json:"symbol"`
	Source      string             `json:"source"`      // "EIA STEO"
	SourceURL   string             `json:"sourceUrl"`   // link to the STEO release
	ReleaseDate string             `json:"releaseDate"` // RFC3339 date the forecast was published
	Unit        string             `json:"unit"`        // "USD/barrel" etc.
	Months      []ConsensusMonthly `json:"months"`      // forward 6-12 months
}

type ConsensusMonthly struct {
	Period string  `json:"period"` // "2026-05"
	Value  float64 `json:"value"`
}

type TechnicalSignals struct {
	RSI          float64 `json:"rsi"`
	MACD         string  `json:"macd"`
	Signal       string  `json:"signal"`
	MovingAvg50  float64 `json:"movingAvg50"`
	MovingAvg200 float64 `json:"movingAvg200"`
	Trend        string  `json:"trend"`
}

type MarketAnalysis struct {
	Sentiment  string           `json:"sentiment"`
	Score      float64          `json:"score"`
	Summary    string           `json:"summary"`
	KeyPoints  []string         `json:"keyPoints"`
	Technical  TechnicalSignals `json:"technical"`
	UpdatedAt  string           `json:"updatedAt"`
}

type MarketOverview struct {
	Prices      []Price        `json:"prices"`
	Analysis    MarketAnalysis `json:"analysis"`
	Predictions []Prediction   `json:"predictions"`
}
