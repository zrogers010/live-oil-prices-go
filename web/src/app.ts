import { getPrices, getChartData, getNews, getPredictions } from './api';
import { initChart, updateChartData, subscribeCrosshair } from './charts';
import type { Price, ChartData, NewsArticle, OHLCV, Prediction } from './types';

let currentSymbol = 'WTI';
let currentDays = 90;
let allNews: NewsArticle[] = [];
let currentCategory = 'all';

// ─── Bootstrap ──────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
  setupNavigation();
  setupNewsFilters();
  setupClickHandlers();
  loadAllData();
  setInterval(refreshPrices, 15000);
});

async function loadAllData(): Promise<void> {
  try {
    await Promise.all([
      loadPrices(),
      loadChart(currentSymbol, currentDays),
      loadForecasts(),
      loadNews(),
    ]);
  } catch (e) {
    setError('An error occurred loading data.');
  }
}

function setError(message: string) {
  const errorContainer = document.getElementById('errorContainer');
  if (errorContainer) {
    errorContainer.textContent = message;
    errorContainer.style.display = message ? 'block' : 'none';
  }
}

// ─── Prices ─────────────────────────────────────────────

async function loadPrices(): Promise<void> {
  try {
    const prices = await getPrices();
    renderHeroPrices(prices);
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
    renderHeroPrices(prices);
    renderTicker(prices);
    updatePriceValues(prices);
    renderMarketTable(prices);
  } catch (err) {
    console.error('Failed to refresh prices:', err);
  }
}

// ─── Hero Prices ────────────────────────────────────────

function renderHeroPrices(prices: Price[]): void {
  const wti = prices.find(p => p.symbol === 'WTI');
  const brent = prices.find(p => p.symbol === 'BRENT');

  if (wti) renderHeroPriceCard(wti, 'WTI');
  if (brent) renderHeroPriceCard(brent, 'BRENT');

  const secondary = prices.filter(p =>
    ['NATGAS', 'HEATING', 'RBOB'].includes(p.symbol)
  );
  renderHeroSecondary(secondary);
}

function renderHeroPriceCard(p: Price, id: string): void {
  const priceEl = document.getElementById(`hero${id}Price`);
  const changeEl = document.getElementById(`hero${id}Change`);
  const contractEl = document.getElementById(`hero${id}Contract`);
  const sourceEl = document.getElementById(`hero${id}Source`);
  const updatedEl = document.getElementById(`hero${id}Updated`);

  if (!priceEl) return;

  const positive = p.change >= 0;
  const sign = positive ? '+' : '';
  const arrow = positive ? '▲' : '▼';

  priceEl.textContent = `$${p.price.toFixed(2)}`;

  if (changeEl) {
    changeEl.textContent = `${arrow} ${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)`;
    changeEl.className = `hero-price-change ${positive ? 'positive' : 'negative'}`;
  }

  if (contractEl) {
    contractEl.textContent = p.contract
      ? `${p.contract} — Front Month`
      : '';
  }

  if (sourceEl) {
    sourceEl.textContent = p.source === 'yahoo' ? 'NYMEX / ICE' : 'Estimate';
  }

  if (updatedEl) {
    updatedEl.textContent = p.updatedAt ? timeAgo(p.updatedAt) : '';
  }
}

function renderHeroSecondary(prices: Price[]): void {
  const container = document.getElementById('heroSecondaryPrices');
  if (!container) return;

  container.innerHTML = prices.map(p => {
    const positive = p.change >= 0;
    const sign = positive ? '+' : '';
    const contractHtml = p.contract
      ? `<span class="hero-secondary-contract">${p.contract}</span>`
      : '';

    return `
      <div class="hero-secondary-item" data-symbol="${p.symbol}">
        <span class="hero-secondary-name">${p.name}</span>
        <span class="hero-secondary-price">$${p.price.toFixed(2)}</span>
        <span class="hero-secondary-change ${positive ? 'positive' : 'negative'}">${sign}${p.changePct.toFixed(2)}%</span>
        ${contractHtml}
      </div>
    `;
  }).join('');
}

// ─── Ticker ─────────────────────────────────────────────

function renderTicker(prices: Price[]): void {
  const track = document.getElementById('tickerTrack');
  if (!track) return;

  const html = prices.map(p => {
    const positive = p.change >= 0;
    const sign = positive ? '+' : '';
    return `
      <a href="/commodity/${p.symbol}" class="ticker-item">
        <span class="ticker-symbol">${p.symbol}</span>
        <span class="ticker-price">$${p.price.toFixed(2)}</span>
        <span class="ticker-change ${positive ? 'positive' : 'negative'}">${sign}${p.changePct.toFixed(2)}%</span>
      </a>
      <div class="ticker-divider"></div>
    `;
  }).join('');

  track.innerHTML = html + html;
}

// ─── Price Cards ────────────────────────────────────────

const CARD_SYMBOLS = ['WTI', 'BRENT', 'NATGAS', 'HEATING', 'RBOB', 'OPEC'];

function renderPriceCards(prices: Price[]): void {
  const grid = document.getElementById('priceGrid');
  if (!grid) return;

  const filtered = prices.filter(p => CARD_SYMBOLS.includes(p.symbol));
  grid.innerHTML = filtered.map(p => {
    const positive = p.change >= 0;
    const sign = positive ? '+' : '';
    const arrow = positive ? '↑' : '↓';
    const contractHtml = p.contract
      ? `<div class="price-card-contract">${p.contract}</div>`
      : '';

    return `
      <div class="price-card" data-symbol="${p.symbol}">
        <div class="price-card-header">
          <div>
            <div class="price-card-symbol">${p.symbol}</div>
            <div class="price-card-name">${p.name}</div>
            ${contractHtml}
          </div>
          <span class="price-card-badge ${positive ? 'positive' : 'negative'}">${arrow} ${sign}${p.changePct.toFixed(2)}%</span>
        </div>
        <div class="price-card-price" data-field="price">$${p.price.toFixed(2)}</div>
        <div class="price-card-change ${positive ? 'positive' : 'negative'}" data-field="change">${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)</div>
        <div class="price-card-meta">
          <div class="price-meta-item">
            <span class="price-meta-label">High</span>
            <span class="price-meta-value" data-field="high">$${p.high.toFixed(2)}</span>
          </div>
          <div class="price-meta-item">
            <span class="price-meta-label">Low</span>
            <span class="price-meta-value" data-field="low">$${p.low.toFixed(2)}</span>
          </div>
          <div class="price-meta-item">
            <span class="price-meta-label">Volume</span>
            <span class="price-meta-value" data-field="volume">${formatVolume(p.volume)}</span>
          </div>
        </div>
      </div>
    `;
  }).join('');
}

function updatePriceValues(prices: Price[]): void {
  prices.forEach(p => {
    const card = document.querySelector(`.price-card[data-symbol="${p.symbol}"]`) as HTMLElement;
    if (!card) return;

    const positive = p.change >= 0;
    const sign = positive ? '+' : '';

    const priceEl = card.querySelector('[data-field="price"]');
    if (priceEl) priceEl.textContent = `$${p.price.toFixed(2)}`;

    const changeEl = card.querySelector('[data-field="change"]');
    if (changeEl) {
      changeEl.textContent = `${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)`;
      changeEl.className = `price-card-change ${positive ? 'positive' : 'negative'}`;
    }

    const badge = card.querySelector('.price-card-badge');
    if (badge) {
      const arrow = positive ? '↑' : '↓';
      badge.textContent = `${arrow} ${sign}${p.changePct.toFixed(2)}%`;
      badge.className = `price-card-badge ${positive ? 'positive' : 'negative'}`;
    }

    const highEl = card.querySelector('[data-field="high"]');
    if (highEl) highEl.textContent = `$${p.high.toFixed(2)}`;

    const lowEl = card.querySelector('[data-field="low"]');
    if (lowEl) lowEl.textContent = `$${p.low.toFixed(2)}`;

    const volEl = card.querySelector('[data-field="volume"]');
    if (volEl) volEl.textContent = formatVolume(p.volume);
  });
}

// ─── Market Table ───────────────────────────────────────

function renderMarketTable(prices: Price[]): void {
  const tbody = document.getElementById('marketTableBody');
  if (!tbody) return;

  tbody.innerHTML = prices.map(p => {
    const positive = p.change >= 0;
    const sign = positive ? '+' : '';
    const cls = positive ? 'positive' : 'negative';
    const contractText = p.contract || '—';
    const contractCls = p.source === 'yahoo' ? 'table-contract' : 'table-contract estimate';

    return `
      <tr data-symbol="${p.symbol}">
        <td>
          <div class="table-commodity">
            <div class="table-commodity-icon">${p.symbol.substring(0, 2)}</div>
            <div>
              <div class="table-commodity-name">${p.name}</div>
              <div class="table-commodity-symbol">${p.symbol}</div>
            </div>
          </div>
        </td>
        <td><span class="${contractCls}">${contractText}</span></td>
        <td><span class="table-price">$${p.price.toFixed(2)}</span></td>
        <td><span class="table-change ${cls}">${sign}${p.change.toFixed(2)}</span></td>
        <td><span class="table-pct ${cls}">${sign}${p.changePct.toFixed(2)}%</span></td>
        <td class="hide-mobile"><span class="table-secondary">$${p.high.toFixed(2)}</span></td>
        <td class="hide-mobile"><span class="table-secondary">$${p.low.toFixed(2)}</span></td>
        <td class="hide-mobile"><span class="table-secondary">${formatVolume(p.volume)}</span></td>
      </tr>
    `;
  }).join('');
}

// ─── Chart ──────────────────────────────────────────────

async function loadChart(symbol: string, days: number): Promise<void> {
  try {
    const container = document.getElementById('chartContainer')!;
    if (!(window as any)['chartInit']) {
      initChart(container);
      (window as any)['chartInit'] = true;
    }

    subscribeCrosshair((o, h, l, c, v) => {
      document.getElementById('chartOpen')!.textContent = `$${o.toFixed(2)}`;
      document.getElementById('chartHigh')!.textContent = `$${h.toFixed(2)}`;
      document.getElementById('chartLow')!.textContent = `$${l.toFixed(2)}`;
      document.getElementById('chartClose')!.textContent = `$${c.toFixed(2)}`;
      document.getElementById('chartVolume')!.textContent = formatVolume(v);
    });

    const data = await getChartData(symbol, days);
    updateChartData(data.data);
    renderChartStats(data);
  } catch (err) {
    console.error('Failed to load chart:', err);
    setError('Failed to load chart data. Please try again later.');
  }
}

function renderChartStats(chartData: ChartData): void {
  const el = document.getElementById('chartStats');
  if (!el) return;

  const data = chartData.data;
  if (!data.length) { el.innerHTML = ''; return; }

  const first = data[0];
  const last = data[data.length - 1];
  const change = last.close - first.open;
  const changePct = (change / first.open) * 100;
  const positive = change >= 0;
  const sign = positive ? '+' : '';
  const cls = positive ? 'positive' : 'negative';

  let periodHigh = -Infinity;
  let periodLow = Infinity;
  let totalVol = 0;
  data.forEach(d => {
    if (d.high > periodHigh) periodHigh = d.high;
    if (d.low < periodLow) periodLow = d.low;
    totalVol += d.volume;
  });
  const avgVol = totalVol / data.length;

  el.innerHTML = `
    <div class="chart-stat-item">
      <span class="chart-stat-label">Period Change</span>
      <span class="chart-stat-val ${cls}">${sign}$${change.toFixed(2)} (${sign}${changePct.toFixed(2)}%)</span>
    </div>
    <div class="chart-stat-item">
      <span class="chart-stat-label">Period High</span>
      <span class="chart-stat-val">$${periodHigh.toFixed(2)}</span>
    </div>
    <div class="chart-stat-item">
      <span class="chart-stat-label">Period Low</span>
      <span class="chart-stat-val">$${periodLow.toFixed(2)}</span>
    </div>
    <div class="chart-stat-item">
      <span class="chart-stat-label">Avg Volume</span>
      <span class="chart-stat-val">${formatVolume(avgVol)}</span>
    </div>
    <div class="chart-stat-item">
      <span class="chart-stat-label">Last Close</span>
      <span class="chart-stat-val">$${last.close.toFixed(2)}</span>
    </div>
  `;
}

// ─── Forecasts ──────────────────────────────────────────

async function loadForecasts(): Promise<void> {
  try {
    const predictions = await getPredictions();
    renderForecasts(predictions || []);
  } catch (err) {
    console.error('Failed to load forecasts:', err);
    const grid = document.getElementById('forecastGrid');
    if (grid) {
      grid.innerHTML = `<p class="forecast-empty">Forecasts are temporarily unavailable. Please retry in a few minutes.</p>`;
    }
  }
}

function renderForecasts(predictions: Prediction[]): void {
  const grid = document.getElementById('forecastGrid');
  if (!grid) return;

  if (!predictions.length) {
    grid.innerHTML = `<p class="forecast-empty">No forecasts available.</p>`;
    return;
  }

  grid.innerHTML = predictions.map(forecastCardHtml).join('');
}

function forecastCardHtml(p: Prediction): string {
  const delta = p.predicted - p.current;
  const pct = p.current ? (delta / p.current) * 100 : 0;
  const sign = delta >= 0 ? '+' : '';
  const dirClass =
    p.direction === 'bullish' ? 'bullish' :
    p.direction === 'bearish' ? 'bearish' :
    'neutral';
  const arrow =
    p.direction === 'bullish' ? '↑' :
    p.direction === 'bearish' ? '↓' :
    '→';

  const confidencePct = Math.round((p.confidence || 0) * 100);
  const confidenceLabel =
    confidencePct >= 70 ? 'High' :
    confidencePct >= 50 ? 'Moderate' :
    confidencePct > 0 ? 'Low' :
    'Unavailable';

  const low = p.predictedLow ?? p.predicted;
  const high = p.predictedHigh ?? p.predicted;
  const rangeBar = renderRangeBar(p.current, low, p.predicted, high);

  const sourceLabel = p.source === 'yahoo' ? 'NYMEX / ICE via Yahoo Finance' : 'Estimate';
  const modelLabel = p.model || 'statistical';

  return `
    <article class="forecast-card forecast-${dirClass}" data-symbol="${p.symbol}" role="listitem">
      <header class="forecast-card-header">
        <div>
          <div class="forecast-card-symbol">${p.symbol}</div>
          <div class="forecast-card-name">${p.name}</div>
        </div>
        <span class="forecast-direction-badge ${dirClass}" aria-label="Forecast direction: ${p.direction}">
          <span class="forecast-arrow">${arrow}</span>${p.direction}
        </span>
      </header>

      <div class="forecast-prices">
        <div class="forecast-price-block">
          <span class="forecast-price-label">Now</span>
          <span class="forecast-price-value">$${p.current.toFixed(2)}</span>
        </div>
        <div class="forecast-arrow-divider" aria-hidden="true">
          <svg width="20" height="14" viewBox="0 0 20 14" fill="none"><path d="M1 7h18m0 0L13 1m6 6l-6 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </div>
        <div class="forecast-price-block">
          <span class="forecast-price-label">${p.timeframe}</span>
          <span class="forecast-price-value forecast-${dirClass}-text">$${p.predicted.toFixed(2)}</span>
          <span class="forecast-price-delta ${dirClass}">${sign}${delta.toFixed(2)} (${sign}${pct.toFixed(2)}%)</span>
        </div>
      </div>

      ${rangeBar}

      <div class="forecast-confidence">
        <div class="forecast-confidence-row">
          <span class="forecast-meta-label">Confidence</span>
          <span class="forecast-confidence-value">${confidenceLabel} · ${confidencePct}%</span>
        </div>
        <div class="forecast-confidence-bar" role="progressbar" aria-valuenow="${confidencePct}" aria-valuemin="0" aria-valuemax="100">
          <div class="forecast-confidence-fill ${dirClass}" style="width: ${confidencePct}%"></div>
        </div>
      </div>

      <p class="forecast-analysis">${p.analysis}</p>

      <footer class="forecast-card-footer">
        <span class="forecast-footnote">Model: <code>${modelLabel}</code></span>
        <span class="forecast-footnote">Source: ${sourceLabel}</span>
      </footer>
    </article>
  `;
}

// renderRangeBar visualises low-mid-high prediction interval relative to the
// current price. The current price marker shows where we sit inside the band.
function renderRangeBar(current: number, low: number, predicted: number, high: number): string {
  if (!high || high <= low) {
    return '';
  }
  const span = high - low;
  const pos = (val: number) =>
    Math.max(0, Math.min(100, ((val - low) / span) * 100));

  const currentPos = pos(current);
  const predictedPos = pos(predicted);

  return `
    <div class="forecast-range" aria-label="80% prediction interval">
      <div class="forecast-range-row">
        <span class="forecast-range-label">80% range</span>
        <span class="forecast-range-values">$${low.toFixed(2)} – $${high.toFixed(2)}</span>
      </div>
      <div class="forecast-range-bar">
        <div class="forecast-range-track"></div>
        <div class="forecast-range-marker forecast-range-current" style="left: ${currentPos}%" title="Current $${current.toFixed(2)}"></div>
        <div class="forecast-range-marker forecast-range-predicted" style="left: ${predictedPos}%" title="Predicted $${predicted.toFixed(2)}"></div>
      </div>
      <div class="forecast-range-legend">
        <span class="forecast-legend-item"><span class="forecast-legend-dot current"></span>Now</span>
        <span class="forecast-legend-item"><span class="forecast-legend-dot predicted"></span>Forecast</span>
      </div>
    </div>
  `;
}

// ─── News ───────────────────────────────────────────────

async function loadNews(): Promise<void> {
  try {
    allNews = await getNews();
    renderFilteredNews();
  } catch (err) {
    console.error('Failed to load news:', err);
    setError('Failed to load news. Please try again later.');
  }
}

function setupNewsFilters(): void {
  document.getElementById('newsFilters')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.news-filter-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.news-filter-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentCategory = btn.dataset.category || 'all';
    renderFilteredNews();
  });
}

function renderFilteredNews(): void {
  const filtered = currentCategory === 'all'
    ? allNews
    : allNews.filter(a => a.category === currentCategory);

  if (filtered.length === 0) {
    const featured = document.getElementById('newsFeatured');
    if (featured) featured.innerHTML = '';
    const grid = document.getElementById('newsGrid');
    if (grid) grid.innerHTML = '<p style="color: var(--text-muted); text-align: center; padding: 48px 0;">No articles in this category.</p>';
    return;
  }

  renderFeaturedNews(filtered[0]);
  renderNewsCards(filtered.slice(1));
}

function renderFeaturedNews(article: NewsArticle): void {
  const el = document.getElementById('newsFeatured');
  if (!el) return;

  el.innerHTML = `
    <article class="news-featured-card" data-url="${article.sourceUrl || '#'}">
      <div class="news-featured-content">
        <div class="news-featured-badge">
          <span class="pulse"></span>
          Latest
        </div>
        <h3 class="news-featured-title">${article.title}</h3>
        <p class="news-featured-summary">${article.summary}</p>
        <div class="news-featured-meta">
          <span>${article.source}</span>
          <span>&middot;</span>
          <span>${timeAgo(article.publishedAt)}</span>
          <span>&middot;</span>
          <span>${article.readTime}</span>
        </div>
      </div>
      <div class="news-featured-aside">
        <span class="news-featured-category">${article.category}</span>
      </div>
    </article>
  `;
}

function renderNewsCards(articles: NewsArticle[]): void {
  const el = document.getElementById('newsGrid');
  if (!el) return;

  el.innerHTML = articles.map(a => `
    <article class="news-card" data-url="${a.sourceUrl || '#'}">
      <div class="news-card-top">
        <span class="news-category">${a.category}</span>
        <span class="news-time">${timeAgo(a.publishedAt)}</span>
      </div>
      <h3 class="news-title">${a.title}</h3>
      <p class="news-summary">${a.summary}</p>
      <div class="news-footer">
        <span class="news-source">${a.source}</span>
        <span class="news-read-link">Read article →</span>
      </div>
    </article>
  `).join('');
}

// ─── Navigation & Interaction ───────────────────────────

function setupNavigation(): void {
  const mobileBtn = document.getElementById('mobileMenuBtn');
  const navLinks = document.getElementById('navLinks');

  mobileBtn?.addEventListener('click', () => {
    mobileBtn.classList.toggle('active');
    navLinks?.classList.toggle('open');
  });

  navLinks?.addEventListener('click', (e) => {
    const link = (e.target as HTMLElement).closest('.nav-link');
    if (link) {
      mobileBtn?.classList.remove('active');
      navLinks.classList.remove('open');
    }
  });

  window.addEventListener('scroll', () => {
    const navbar = document.getElementById('navbar');
    if (navbar) {
      navbar.classList.toggle('scrolled', window.scrollY > 20);
    }
  });
}

function setupClickHandlers(): void {
  document.getElementById('priceGrid')?.addEventListener('click', (e) => {
    const card = (e.target as HTMLElement).closest('.price-card') as HTMLElement;
    if (card?.dataset.symbol) {
      window.location.href = `/commodity/${card.dataset.symbol}`;
    }
  });

  document.querySelectorAll('.hero-price-card').forEach(el => {
    el.addEventListener('click', () => {
      const sym = (el as HTMLElement).dataset.symbol;
      if (sym) window.location.href = `/commodity/${sym}`;
    });
  });

  document.getElementById('heroSecondaryPrices')?.addEventListener('click', (e) => {
    const item = (e.target as HTMLElement).closest('.hero-secondary-item') as HTMLElement;
    if (item?.dataset.symbol) {
      window.location.href = `/commodity/${item.dataset.symbol}`;
    }
  });

  document.getElementById('marketTableBody')?.addEventListener('click', (e) => {
    const row = (e.target as HTMLElement).closest('tr') as HTMLElement;
    if (row?.dataset.symbol) {
      window.location.href = `/commodity/${row.dataset.symbol}`;
    }
  });

  document.getElementById('chartSymbols')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-symbol-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-symbol-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentSymbol = btn.dataset.symbol || 'WTI';
    loadChart(currentSymbol, currentDays);
  });

  document.getElementById('chartTimeframes')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-tf-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-tf-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentDays = parseInt(btn.dataset.days || '90', 10);
    loadChart(currentSymbol, currentDays);
  });

  document.addEventListener('click', (e) => {
    const card = (e.target as HTMLElement).closest('.news-card, .news-featured-card') as HTMLElement;
    if (card?.dataset.url && card.dataset.url !== '#') {
      window.open(card.dataset.url, '_blank', 'noopener');
    }
  });
}

// ─── Utilities ──────────────────────────────────────────

function formatVolume(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(0) + 'K';
  return n.toString();
}

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
