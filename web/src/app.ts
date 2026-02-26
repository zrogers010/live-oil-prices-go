import { getPrices, getChartData, getNews } from './api';
import { initChart, updateChartData, subscribeCrosshair } from './charts';
import type { Price, ChartData, NewsArticle, OHLCV } from './types';

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
  await Promise.all([
    loadPrices(),
    loadChart(currentSymbol, currentDays),
    loadNews(),
  ]);
}

// ─── Navigation ─────────────────────────────────────────

function setupNavigation(): void {
  const navbar = document.getElementById('navbar')!;
  const menuBtn = document.getElementById('mobileMenuBtn')!;
  const navLinks = document.getElementById('navLinks')!;

  window.addEventListener('scroll', () => {
    navbar.classList.toggle('scrolled', window.scrollY > 20);
  });

  menuBtn.addEventListener('click', () => {
    menuBtn.classList.toggle('active');
    navLinks.classList.toggle('open');
    document.body.style.overflow = navLinks.classList.contains('open') ? 'hidden' : '';
  });

  navLinks.querySelectorAll('.nav-link').forEach(link => {
    link.addEventListener('click', () => {
      menuBtn.classList.remove('active');
      navLinks.classList.remove('open');
      document.body.style.overflow = '';
    });
  });

  document.getElementById('chartSymbols')!.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-symbol-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-symbol-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentSymbol = btn.dataset.symbol!;
    loadChart(currentSymbol, currentDays);
  });

  document.getElementById('chartTimeframes')!.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-tf-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-tf-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentDays = parseInt(btn.dataset.days!);
    loadChart(currentSymbol, currentDays);
  });
}

// ─── Click Handlers ─────────────────────────────────────

function setupClickHandlers(): void {
  // Price cards → open commodity page
  document.getElementById('priceGrid')!.addEventListener('click', (e) => {
    const card = (e.target as HTMLElement).closest('.price-card') as HTMLElement;
    if (!card) return;
    window.location.href = `/commodity/${card.dataset.symbol}`;
  });

  // Market table rows → open commodity page
  document.getElementById('marketTableBody')!.addEventListener('click', (e) => {
    const row = (e.target as HTMLElement).closest('tr') as HTMLElement;
    if (!row) return;
    const symbolEl = row.querySelector('.table-commodity-symbol');
    if (symbolEl) window.location.href = `/commodity/${symbolEl.textContent!.trim()}`;
  });

  // News cards → open source article
  document.getElementById('newsGrid')!.addEventListener('click', (e) => {
    const card = (e.target as HTMLElement).closest('.news-card') as HTMLElement;
    if (!card || !card.dataset.url) return;
    window.open(card.dataset.url, '_blank', 'noopener');
  });

  // Featured news → open source article
  document.getElementById('newsFeatured')!.addEventListener('click', (e) => {
    const card = (e.target as HTMLElement).closest('.news-featured-card') as HTMLElement;
    if (!card || !card.dataset.url) return;
    window.open(card.dataset.url, '_blank', 'noopener');
  });
}

// ─── Prices ─────────────────────────────────────────────

async function loadPrices(): Promise<void> {
  try {
    const prices = await getPrices();
    renderTicker(prices);
    renderPriceCards(prices);
    renderMarketTable(prices);
  } catch (e) {
    console.error('Failed to load prices:', e);
  }
}

async function refreshPrices(): Promise<void> {
  try {
    const prices = await getPrices();
    renderTicker(prices);
    updatePriceValues(prices);
    renderMarketTable(prices);
  } catch (e) {
    console.error('Failed to refresh prices:', e);
  }
}

function renderTicker(prices: Price[]): void {
  const track = document.getElementById('tickerTrack')!;
  const items = prices.map(p => {
    const isPositive = p.change >= 0;
    const sign = isPositive ? '+' : '';
    return `
      <a href="/commodity/${p.symbol}" class="ticker-item">
        <span class="ticker-symbol">${p.symbol}</span>
        <span class="ticker-price">$${p.price.toFixed(2)}</span>
        <span class="ticker-change ${isPositive ? 'positive' : 'negative'}">${sign}${p.changePct.toFixed(2)}%</span>
      </a>
      <div class="ticker-divider"></div>
    `;
  }).join('');

  track.innerHTML = items + items;
}

const TOP_CARD_SYMBOLS = ['WTI', 'BRENT', 'NATGAS', 'HEATING', 'RBOB', 'OPEC'];

function renderPriceCards(prices: Price[]): void {
  const grid = document.getElementById('priceGrid')!;
  const topPrices = prices.filter(p => TOP_CARD_SYMBOLS.includes(p.symbol));
  grid.innerHTML = topPrices.map(p => {
    const isPositive = p.change >= 0;
    const sign = isPositive ? '+' : '';
    const arrow = isPositive ? '↑' : '↓';
    return `
      <div class="price-card" data-symbol="${p.symbol}">
        <div class="price-card-header">
          <div>
            <div class="price-card-symbol">${p.symbol}</div>
            <div class="price-card-name">${p.name}</div>
          </div>
          <span class="price-card-badge ${isPositive ? 'positive' : 'negative'}">${arrow} ${sign}${p.changePct.toFixed(2)}%</span>
        </div>
        <div class="price-card-price" data-field="price">$${p.price.toFixed(2)}</div>
        <div class="price-card-change ${isPositive ? 'positive' : 'negative'}" data-field="change">${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)</div>
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

function renderMarketTable(prices: Price[]): void {
  const tbody = document.getElementById('marketTableBody')!;
  tbody.innerHTML = prices.map(p => {
    const isPositive = p.change >= 0;
    const sign = isPositive ? '+' : '';
    const cls = isPositive ? 'positive' : 'negative';
    return `
      <tr>
        <td>
          <div class="table-commodity">
            <div class="table-commodity-icon">${p.symbol.substring(0, 2)}</div>
            <div>
              <div class="table-commodity-name">${p.name}</div>
              <div class="table-commodity-symbol">${p.symbol}</div>
            </div>
          </div>
        </td>
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

function updatePriceValues(prices: Price[]): void {
  prices.forEach(p => {
    const card = document.querySelector(`.price-card[data-symbol="${p.symbol}"]`);
    if (!card) return;

    const isPositive = p.change >= 0;
    const sign = isPositive ? '+' : '';

    const priceEl = card.querySelector('[data-field="price"]')!;
    priceEl.textContent = `$${p.price.toFixed(2)}`;

    const changeEl = card.querySelector('[data-field="change"]')!;
    changeEl.textContent = `${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)`;
    changeEl.className = `price-card-change ${isPositive ? 'positive' : 'negative'}`;

    const badge = card.querySelector('.price-card-badge')!;
    const arrow = isPositive ? '↑' : '↓';
    badge.textContent = `${arrow} ${sign}${p.changePct.toFixed(2)}%`;
    badge.className = `price-card-badge ${isPositive ? 'positive' : 'negative'}`;

    card.querySelector('[data-field="high"]')!.textContent = `$${p.high.toFixed(2)}`;
    card.querySelector('[data-field="low"]')!.textContent = `$${p.low.toFixed(2)}`;
    card.querySelector('[data-field="volume"]')!.textContent = formatVolume(p.volume);
  });
}

// ─── Charts ─────────────────────────────────────────────

async function loadChart(symbol: string, days: number): Promise<void> {
  try {
    const container = document.getElementById('chartContainer')!;
    initChart(container);

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
  } catch (e) {
    console.error('Failed to load chart:', e);
  }
}

function renderChartStats(chartData: ChartData): void {
  const statsEl = document.getElementById('chartStats')!;
  const d = chartData.data;
  if (!d.length) { statsEl.innerHTML = ''; return; }

  const first = d[0];
  const last = d[d.length - 1];
  const periodChange = last.close - first.open;
  const periodChangePct = ((periodChange / first.open) * 100);
  const isPositive = periodChange >= 0;
  const sign = isPositive ? '+' : '';
  const cls = isPositive ? 'positive' : 'negative';

  let periodHigh = -Infinity, periodLow = Infinity, totalVol = 0;
  d.forEach((c: OHLCV) => {
    if (c.high > periodHigh) periodHigh = c.high;
    if (c.low < periodLow) periodLow = c.low;
    totalVol += c.volume;
  });

  const avgVol = totalVol / d.length;

  statsEl.innerHTML = `
    <div class="chart-stat-item">
      <span class="chart-stat-label">Period Change</span>
      <span class="chart-stat-val ${cls}">${sign}$${periodChange.toFixed(2)} (${sign}${periodChangePct.toFixed(2)}%)</span>
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

// ─── News ───────────────────────────────────────────────

function setupNewsFilters(): void {
  document.getElementById('newsFilters')!.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.news-filter-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.news-filter-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentCategory = btn.dataset.category!;
    renderFilteredNews();
  });
}

async function loadNews(): Promise<void> {
  try {
    allNews = await getNews();
    renderFilteredNews();
  } catch (e) {
    console.error('Failed to load news:', e);
  }
}

function renderFilteredNews(): void {
  const filtered = currentCategory === 'all'
    ? allNews
    : allNews.filter(a => a.category === currentCategory);

  if (filtered.length === 0) {
    document.getElementById('newsFeatured')!.innerHTML = '';
    document.getElementById('newsGrid')!.innerHTML =
      '<p style="color: var(--text-muted); text-align: center; padding: 48px 0;">No articles in this category.</p>';
    return;
  }

  const featured = filtered[0];
  const rest = filtered.slice(1);

  renderFeaturedNews(featured);
  renderNewsCards(rest);
}

function renderFeaturedNews(a: NewsArticle): void {
  const el = document.getElementById('newsFeatured')!;
  el.innerHTML = `
    <article class="news-featured-card" data-url="${a.sourceUrl || '#'}">
      <div class="news-featured-content">
        <div class="news-featured-badge">
          <span class="pulse"></span>
          Latest
        </div>
        <h3 class="news-featured-title">${a.title}</h3>
        <p class="news-featured-summary">${a.summary}</p>
        <div class="news-featured-meta">
          <span>${a.source}</span>
          <span>·</span>
          <span>${formatTimeAgo(a.publishedAt)}</span>
          <span>·</span>
          <span>${a.readTime}</span>
        </div>
      </div>
      <div class="news-featured-aside">
        <span class="news-featured-category">${a.category}</span>
      </div>
    </article>
  `;
}

function renderNewsCards(articles: NewsArticle[]): void {
  const grid = document.getElementById('newsGrid')!;
  grid.innerHTML = articles.map(a => `
    <article class="news-card" data-url="${a.sourceUrl || '#'}">
      <div class="news-card-top">
        <span class="news-category">${a.category}</span>
        <span class="news-time">${formatTimeAgo(a.publishedAt)}</span>
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

// ─── Helpers ────────────────────────────────────────────

function formatVolume(v: number): string {
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + 'M';
  if (v >= 1_000) return (v / 1_000).toFixed(0) + 'K';
  return v.toString();
}

function formatTimeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}
