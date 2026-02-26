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
}

type OHLCV struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
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
	Symbol     string  `json:"symbol"`
	Name       string  `json:"name"`
	Current    float64 `json:"current"`
	Predicted  float64 `json:"predicted"`
	Timeframe  string  `json:"timeframe"`
	Confidence float64 `json:"confidence"`
	Direction  string  `json:"direction"`
	Analysis   string  `json:"analysis"`
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
