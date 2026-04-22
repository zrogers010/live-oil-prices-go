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
 *  always returns today's NY trading session at 5-minute resolution and
 *  picks `mode` based on data freshness:
 *   - "live"          : today's bars + Pyth feeding the rightmost bar in real time.
 *   - "today-paused"  : today's bars but Pyth is quiet (CME daily 5–6 PM ET
 *                       break, brief publisher hiccup, etc.) — chart still
 *                       spans today, just no live pulse.
 *   - "prior-session" : today has no bars yet (pre-Sunday-reopen, full
 *                       weekend day) so we serve the most recent prior
 *                       trading day. `sessionDate` labels which day.
 *   - "warming-up"    : cold start, no data anywhere — render placeholder. */
export interface HeroChart {
  symbol: string;
  mode: "live" | "today-paused" | "prior-session" | "warming-up";
  interval: string; // typically "5m"
  sessionDate?: string; // YYYY-MM-DD in NY-local time
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

  // Signal-stack fields backing the Outlook & Signals card.
  trendLabel?: string;
  rsi14?: number;
  rsiLabel?: string;
  macdHist?: number;
  macdLabel?: string;
  maConfig?: string;

  // Backtest credibility numbers.
  mape?: number;
  naiveMape?: number;
  skill?: number;
  backtestSteps?: number;
}

export interface ConsensusMonthly {
  period: string;
  value: number;
}

export interface ConsensusForecast {
  symbol: string;
  source: string;
  sourceUrl: string;
  releaseDate: string;
  unit: string;
  months: ConsensusMonthly[];
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
