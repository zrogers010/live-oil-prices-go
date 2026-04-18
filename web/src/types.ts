export interface Price {
  symbol: string;
  name: string;
  price: number;
  change: number;
  changePct: number;
  high: number;
  low: number;
  volume: number;
  updatedAt: string;
  contract?: string;
  source?: string;
}

export interface OHLCV {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface ChartData {
  symbol: string;
  name: string;
  interval: string;
  data: OHLCV[];
}

/** PythCandle is a streaming 1-minute OHLC bar built from Pyth Network ticks.
 *  Volume is omitted by design — Pyth aggregates publishers, not trades. */
export interface PythCandle {
  time: number; // unix seconds at the start of the 1-min bucket
  open: number;
  high: number;
  low: number;
  close: number;
  ticks?: number;
}

/** HeroChart is the unified payload for the homepage hero chart. The server
 *  picks `mode` based on data freshness:
 *   - "live": streaming 1-minute Pyth candles (markets open).
 *   - "prior-session": intraday Yahoo bars from the most recent complete
 *     trading day (weekend / holiday / feed paused).
 *   - "warming-up": no data yet — render the cold-start placeholder. */
export interface HeroChart {
  symbol: string;
  mode: "live" | "prior-session" | "warming-up";
  interval: string; // e.g. "1m" or "5m"
  sessionDate?: string; // YYYY-MM-DD in NYMEX local time, set when mode === "prior-session"
  updatedAt?: string; // RFC3339 of latest bar
  source: "pyth" | "yahoo" | "";
  bars: PythCandle[];
}

export interface NewsArticle {
  id: string;
  slug: string;
  title: string;
  summary: string;
  content: string;
  source: string;
  sourceUrl: string;
  category: string;
  publishedAt: string;
  imageUrl: string;
  readTime: string;
}

export interface Prediction {
  symbol: string;
  name: string;
  current: number;
  predicted: number;
  predictedLow?: number;
  predictedHigh?: number;
  timeframe: string;
  confidence: number;
  direction: string;
  analysis: string;
  model?: string;
  source?: string;
  disclaimer?: string;
}

export interface TechnicalSignals {
  rsi: number;
  macd: string;
  signal: string;
  movingAvg50: number;
  movingAvg200: number;
  trend: string;
}

export interface MarketAnalysis {
  sentiment: string;
  score: number;
  summary: string;
  keyPoints: string[];
  technical: TechnicalSignals;
  updatedAt: string;
}
