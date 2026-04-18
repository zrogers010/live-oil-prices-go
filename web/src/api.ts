import type { Price, ChartData, NewsArticle, Prediction, MarketAnalysis, HeroChart } from './types';

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

/** getHeroChart fetches the homepage hero chart payload. The server picks
 *  the right mode automatically: streaming 1-minute Pyth candles when the
 *  market is live, or a 1-day intraday Yahoo series for the prior session
 *  when the feed is paused (weekends, holidays). `max` only affects the
 *  live mode; prior-session mode always returns the full session. */
export function getHeroChart(symbol: string, max: number = 360): Promise<HeroChart> {
  return fetchJSON<HeroChart>(`/api/hero/${symbol}?max=${max}`);
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
