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
  timeframe: string;
  confidence: number;
  direction: string;
  analysis: string;
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
