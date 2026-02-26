package services

import (
	"fmt"
	"live-oil-prices-go/internal/models"
	"math"
	"math/rand"
	"time"
)

type MarketDataService struct {
	rng        *rand.Rand
	basePrices map[string]float64
}

func NewMarketDataService() *MarketDataService {
	bases := map[string]float64{
		"WTI":    72.45,
		"BRENT":  76.82,
		"NATGAS": 3.24,
		"HEATING": 2.35,
		"RBOB":   2.18,
		"OPEC":   74.50,
		"DUBAI":  75.10,
		"MURBAN": 76.30,
		"WCS":    58.20,
		"GASOIL": 685.50,
	}
	return &MarketDataService{
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		basePrices: bases,
	}
}

var commodityNames = map[string]string{
	"WTI": "WTI Crude Oil", "BRENT": "Brent Crude Oil",
	"NATGAS": "Natural Gas", "HEATING": "Heating Oil",
	"RBOB": "RBOB Gasoline", "OPEC": "OPEC Basket",
	"DUBAI": "Dubai Crude", "MURBAN": "Murban Crude",
	"WCS": "Western Canadian Select", "GASOIL": "ICE Gasoil",
}

var allCommodities = []struct {
	symbol string
	name   string
}{
	{"WTI", "WTI Crude Oil"},
	{"BRENT", "Brent Crude Oil"},
	{"NATGAS", "Natural Gas"},
	{"HEATING", "Heating Oil"},
	{"RBOB", "RBOB Gasoline"},
	{"OPEC", "OPEC Basket"},
	{"DUBAI", "Dubai Crude"},
	{"MURBAN", "Murban Crude"},
	{"WCS", "Western Canadian Select"},
	{"GASOIL", "ICE Gasoil"},
}

func (s *MarketDataService) GetPrices() []models.Price {
	now := time.Now().UTC().Format(time.RFC3339)
	prices := make([]models.Price, len(allCommodities))
	for i, c := range allCommodities {
		base := s.basePrices[c.symbol]
		volatility := base * 0.008
		change := (s.rng.Float64() - 0.45) * volatility
		price := base + change
		changePct := (change / base) * 100
		dayVolatility := base * 0.02
		high := price + s.rng.Float64()*dayVolatility
		low := price - s.rng.Float64()*dayVolatility
		volume := int64(500000 + s.rng.Intn(2000000))
		prices[i] = models.Price{
			Symbol:    c.symbol,
			Name:      c.name,
			Price:     math.Round(price*100) / 100,
			Change:    math.Round(change*100) / 100,
			ChangePct: math.Round(changePct*100) / 100,
			High:      math.Round(high*100) / 100,
			Low:       math.Round(low*100) / 100,
			Volume:    volume,
			UpdatedAt: now,
		}
	}
	return prices
}

// GetChartData generates OHLCV candles. interval: "2h","4h","1d"
func (s *MarketDataService) GetChartData(symbol string, days int, interval string) models.ChartData {
	base, ok := s.basePrices[symbol]
	if !ok {
		base = 72.0
	}
	name := symbol
	if n, ok := commodityNames[symbol]; ok {
		name = n
	}

	if interval == "" {
		switch {
		case days <= 7:
			interval = "2h"
		case days <= 30:
			interval = "4h"
		default:
			interval = "1d"
		}
	}

	var data []models.OHLCV
	switch interval {
	case "2h":
		data = s.generateIntraday(base, days, 2)
	case "4h":
		data = s.generateIntraday(base, days, 4)
	default:
		data = s.generateDaily(base, days)
	}

	return models.ChartData{Symbol: symbol, Name: name, Interval: interval, Data: data}
}

func (s *MarketDataService) generateDaily(base float64, days int) []models.OHLCV {
	allData := make([]models.OHLCV, 0, days+50)
	price := base - (base * 0.05)
	calendarDays := int(float64(days)*1.5) + 20
	startTime := time.Now().AddDate(0, 0, -calendarDays)
	now := time.Now()

	for i := 0; i <= calendarDays; i++ {
		t := startTime.AddDate(0, 0, i)
		if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
			continue
		}
		if t.After(now) {
			break
		}
		vol := price * 0.015
		change := (s.rng.Float64() - 0.47) * vol
		price += change
		open := price
		cl := open + (s.rng.Float64()-0.5)*vol
		high := math.Max(open, cl) + s.rng.Float64()*vol*0.5
		low := math.Min(open, cl) - s.rng.Float64()*vol*0.5
		v := int64(800000 + s.rng.Intn(1500000))
		allData = append(allData, models.OHLCV{
			Time: t.Unix(), Open: r2(open), High: r2(high), Low: r2(low), Close: r2(cl), Volume: v,
		})
		price = cl
	}
	if len(allData) > days {
		return allData[len(allData)-days:]
	}
	return allData
}

func (s *MarketDataService) generateIntraday(base float64, days int, hoursPerCandle int) []models.OHLCV {
	candlesPerDay := 24 / hoursPerCandle
	data := make([]models.OHLCV, 0, days*candlesPerDay)
	price := base - (base * 0.03)
	calendarDays := int(float64(days)*1.5) + 5
	startTime := time.Now().AddDate(0, 0, -calendarDays)
	now := time.Now()

	for d := 0; d <= calendarDays; d++ {
		dayStart := startTime.AddDate(0, 0, d)
		if dayStart.Weekday() == time.Saturday || dayStart.Weekday() == time.Sunday {
			continue
		}
		dayStart = time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, time.UTC)

		for c := 0; c < candlesPerDay; c++ {
			t := dayStart.Add(time.Duration(c*hoursPerCandle) * time.Hour)
			if t.After(now) {
				break
			}
			vol := price * 0.004
			change := (s.rng.Float64() - 0.48) * vol
			price += change
			open := price
			cl := open + (s.rng.Float64()-0.5)*vol
			high := math.Max(open, cl) + s.rng.Float64()*vol*0.3
			low := math.Min(open, cl) - s.rng.Float64()*vol*0.3
			v := int64(50000 + s.rng.Intn(200000))
			data = append(data, models.OHLCV{
				Time: t.Unix(), Open: r2(open), High: r2(high), Low: r2(low), Close: r2(cl), Volume: v,
			})
			price = cl
		}
	}
	targetCount := days * candlesPerDay
	if len(data) > targetCount {
		return data[len(data)-targetCount:]
	}
	return data
}

func r2(v float64) float64 { return math.Round(v*100) / 100 }

func (s *MarketDataService) GetPredictions() []models.Prediction {
	prices := s.GetPrices()
	pm := make(map[string]float64)
	for _, p := range prices {
		pm[p.Symbol] = p.Price
	}
	return []models.Prediction{
		{Symbol: "WTI", Name: "WTI Crude Oil", Current: pm["WTI"], Predicted: r2(pm["WTI"] * 1.028), Timeframe: "7 days", Confidence: 0.78, Direction: "bullish",
			Analysis: "Technical indicators show a bullish divergence on the daily RSI, while MACD has crossed above the signal line. Fundamental support from declining inventories and OPEC+ production discipline reinforces the upward bias. Key resistance at $74.50."},
		{Symbol: "BRENT", Name: "Brent Crude Oil", Current: pm["BRENT"], Predicted: r2(pm["BRENT"] * 1.022), Timeframe: "7 days", Confidence: 0.74, Direction: "bullish",
			Analysis: "Brent is trading above its 50-day moving average with increasing volume. Geopolitical risk premium remains elevated. Support at $75.50, resistance at $79.00. The Brent-WTI spread is widening, suggesting global supply tightness."},
		{Symbol: "NATGAS", Name: "Natural Gas", Current: pm["NATGAS"], Predicted: r2(pm["NATGAS"] * 1.045), Timeframe: "7 days", Confidence: 0.65, Direction: "bullish",
			Analysis: "Cold weather forecasts for the US Northeast are driving short-term bullish sentiment. Storage levels remain below the 5-year average. LNG export demand continues to provide a floor. Volatility expected to remain elevated."},
		{Symbol: "HEATING", Name: "Heating Oil", Current: pm["HEATING"], Predicted: r2(pm["HEATING"] * 1.018), Timeframe: "7 days", Confidence: 0.71, Direction: "bullish",
			Analysis: "Heating oil demand remains seasonally strong. Distillate inventories are below the 5-year range. Refining margins support continued production, but European demand competition keeps prices firm."},
	}
}

func (s *MarketDataService) GetAnalysis() models.MarketAnalysis {
	now := time.Now().UTC().Format(time.RFC3339)
	wtiPrice := s.basePrices["WTI"]
	return models.MarketAnalysis{
		Sentiment: "bullish", Score: 72,
		Summary: fmt.Sprintf("The crude oil market is displaying bullish momentum with WTI trading near $%.2f. Technical indicators are aligned with an upward bias as the 50-day moving average has crossed above the 200-day MA, forming a golden cross pattern. Fundamental drivers including OPEC+ supply discipline, declining US inventories, and resilient global demand support the constructive outlook. Key risk factors include potential demand slowdown from economic headwinds and the possibility of OPEC+ policy changes.", wtiPrice),
		KeyPoints: []string{
			"OPEC+ production cuts extended through Q3 2026, removing ~2.2 million bpd from market",
			"US crude inventories fell 4.2 million barrels, 3rd consecutive weekly draw",
			"China crude imports at record 12.4 million bpd supporting global demand",
			"Technical golden cross pattern on WTI daily chart signals bullish trend",
			"Geopolitical risk premium elevated due to Middle East tensions",
			"IEA raised 2026 demand growth forecast to 1.4 million bpd",
		},
		Technical: models.TechnicalSignals{
			RSI: 58.4, MACD: "bullish crossover", Signal: "buy",
			MovingAvg50: r2(wtiPrice - 1.20), MovingAvg200: r2(wtiPrice - 3.50), Trend: "uptrend",
		},
		UpdatedAt: now,
	}
}
