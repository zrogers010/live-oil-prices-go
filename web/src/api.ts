import type { Price, ChartData, NewsArticle, Prediction, MarketAnalysis } from './types';

const BASE = '';

async function fetchJSON<T>(url: string): Promise<T> {
  try {
    const res = await fetch(`${BASE}${url}`);
    if (!res.ok) {
      const errorText = await res.text();
      console.error(`API error fetching ${url}:`, res.status, errorText);
      // Throw a more informative error with response details for better debugging
      const err = new Error(`API error: ${res.status} - ${errorText}`);
      throw err;
    }
    return res.json();
  } catch (error) {
    console.error(`Network error fetching ${url}:`, error);
    throw error;
  }
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
