import { getPrices, getChartData, getNews } from './api';
import { initChart, updateChartData, subscribeCrosshair } from './charts';
import type { Price, ChartData, NewsArticle, OHLCV } from './types';

let currentSymbol = 'WTI';
let currentDays = 90;
let allNews: NewsArticle[] = [];
let currentCategory = 'all';

// Error and loading state
let errorMessage = '';
let loading = false;

// ─── Bootstrap ──────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
  setupNavigation();
  setupNewsFilters();
  setupClickHandlers();
  loadAllData();
  setInterval(refreshPrices, 15000);
});

async function loadAllData(): Promise<void> {
  setLoading(true);
  clearError();
  try {
    await Promise.all([
      loadPrices(),
      loadChart(currentSymbol, currentDays),
      loadNews(),
    ]);
  } catch (e) {
    setError('An error occurred loading data.');
  } finally {
    setLoading(false);
  }
}

function setLoading(value: boolean) {
  loading = value;
  const loader = document.getElementById('loader');
  if (loader) loader.style.display = loading ? 'block' : 'none';
}

function setError(message: string) {
  errorMessage = message;
  const errorContainer = document.getElementById('errorContainer');
  if (errorContainer) {
    errorContainer.textContent = errorMessage;
    errorContainer.style.display = errorMessage ? 'block' : 'none';
  }
}

function clearError() {
  setError('');
}

async function loadPrices(): Promise<void> {
  try {
    const prices = await getPrices();
    renderTicker(prices);
    renderPriceCards(prices);
    renderMarketTable(prices);
  } catch (err) {
    console.error('Failed to load prices:', err);
    setError('Failed to load prices. Please try again later.');
  }
}

async function refreshPrices(): Promise<void> {
  try {
    const prices = await getPrices();
    renderTicker(prices);
    updatePriceValues(prices);
    renderMarketTable(prices);
  } catch (err) {
    console.error('Failed to refresh prices:', err);
    setError('Failed to refresh prices. Please try again later.');
  }
}

async function loadChart(symbol: string, days: number): Promise<void> {
  try {
    const container = document.getElementById('chartContainer')!;
    if (!window['chart']) {
      window['chart'] = initChart(container);
    }

    subscribeCrosshair((o, h, l, c, v) => {
      document.getElementById('chartOpen')!.textContent = `$${o.toFixed(2)}`;
      document.getElementById('chartHigh')!.textContent = `$${h.toFixed(2)}`;
      document.getElementById('chartLow')!.textContent = `$${l.toFixed(2)}`;
      document.getElementById('chartClose')!.textContent = `$${c.toFixed(2)}`;
      document.getElementById('chartVolume')!.textContent = formatVolume(v);
    });

    const data = await getChartData(symbol, days);
    updateChartData(window['chart'], data.data);
    renderChartStats(data);
  } catch (err) {
    console.error('Failed to load chart:', err);
    setError('Failed to load chart data. Please try again later.');
  }
}

async function loadNews(): Promise<void> {
  try {
    allNews = await getNews();
    renderFilteredNews();
  } catch (err) {
    console.error('Failed to load news:', err);
    setError('Failed to load news. Please try again later.');
  }
}

// Remaining existing helper functions unchanged
