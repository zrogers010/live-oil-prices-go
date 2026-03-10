import type { Price, ChartData, NewsArticle, Prediction, MarketAnalysis } from './types';

const BASE = '';

async function fetchJSON<T>(url: string): Promise<T> {
  try {
    const res = await fetch(`${BASE}${url}`);
    if (!res.ok) {
      const errorText = await res.text();
      // Redact potential sensitive data
      const redactedErrorText = errorText.replace(/\b(\d{12,16}|\d{3}-\d{2}-\d{4})\b/g, '[REDACTED]');
      console.error(`API error fetching ${url}:`, res.status, redactedErrorText);
      // For 404 errors, return null as T to allow optional resource handling
      if (res.status === 404) {
        return null as unknown as T;
      }
      
      // Add retry-after header logging for rate limiting transparency
      // const retryAfter = res.headers.get('retry-after');
      // if (retryAfter) {
      //   console.warn(`API rate limited, retry after: ${retryAfter} seconds`);
      // }

      const err = new Error(`API error: ${res.status} - ${redactedErrorText}`);
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
