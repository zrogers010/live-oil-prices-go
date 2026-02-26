import type { Price, ChartData, NewsArticle, Prediction, MarketAnalysis } from './types';

const BASE = '';

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(`${BASE}${url}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export function getPrices(): Promise<Price[]> {
  return fetchJSON<Price[]>('/api/prices');
}

export function getChartData(symbol: string, days: number = 90): Promise<ChartData> {
  return fetchJSON<ChartData>(`/api/charts/${symbol}?days=${days}`);
}

export function getNews(): Promise<NewsArticle[]> {
  return fetchJSON<NewsArticle[]>('/api/news');
}

export function getNewsArticle(id: string): Promise<NewsArticle> {
  return fetchJSON<NewsArticle>(`/api/news/${id}`);
}

export function getPredictions(): Promise<Prediction[]> {
  return fetchJSON<Prediction[]>('/api/predictions');
}

export function getAnalysis(): Promise<MarketAnalysis> {
  return fetchJSON<MarketAnalysis>('/api/analysis');
}
