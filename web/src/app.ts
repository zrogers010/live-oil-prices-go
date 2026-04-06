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
<<<<<<< Updated upstream
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
=======
  try {
    await Promise.all([
      loadPrices(),
      loadChart(currentSymbol, currentDays),
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
>>>>>>> Stashed changes

// ─── Prices ─────────────────────────────────────────────

async function loadPrices(): Promise<void> {
  try {
    const prices = await getPrices();
    renderHeroPrices(prices);
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
    renderHeroPrices(prices);
    renderTicker(prices);
    updatePriceValues(prices);
    renderMarketTable(prices);
<<<<<<< Updated upstream
  } catch (e) {
    console.error('Failed to refresh prices:', e);
  }
}

function renderTicker(prices: Price[]): void {
  const track = document.getElementById('tickerTrack')!;
  const items = prices.map(p => {
    const isPositive = p.change >= 0;
    const sign = isPositive ? '+' : '';
=======
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
>>>>>>> Stashed changes
    return `
      <a href="/commodity/${p.symbol}" class="ticker-item">
        <span class="ticker-symbol">${p.symbol}</span>
        <span class="ticker-price">$${p.price.toFixed(2)}</span>
<<<<<<< Updated upstream
        <span class="ticker-change ${isPositive ? 'positive' : 'negative'}">${sign}${p.changePct.toFixed(2)}%</span>
=======
        <span class="ticker-change ${positive ? 'positive' : 'negative'}">${sign}${p.changePct.toFixed(2)}%</span>
>>>>>>> Stashed changes
      </a>
      <div class="ticker-divider"></div>
    `;
  }).join('');

<<<<<<< Updated upstream
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
=======
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

>>>>>>> Stashed changes
    return `
      <div class="price-card" data-symbol="${p.symbol}">
        <div class="price-card-header">
          <div>
            <div class="price-card-symbol">${p.symbol}</div>
            <div class="price-card-name">${p.name}</div>
<<<<<<< Updated upstream
          </div>
          <span class="price-card-badge ${isPositive ? 'positive' : 'negative'}">${arrow} ${sign}${p.changePct.toFixed(2)}%</span>
        </div>
        <div class="price-card-price" data-field="price">$${p.price.toFixed(2)}</div>
        <div class="price-card-change ${isPositive ? 'positive' : 'negative'}" data-field="change">${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)</div>
=======
            ${contractHtml}
          </div>
          <span class="price-card-badge ${positive ? 'positive' : 'negative'}">${arrow} ${sign}${p.changePct.toFixed(2)}%</span>
        </div>
        <div class="price-card-price" data-field="price">$${p.price.toFixed(2)}</div>
        <div class="price-card-change ${positive ? 'positive' : 'negative'}" data-field="change">${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)</div>
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
function renderMarketTable(prices: Price[]): void {
  const tbody = document.getElementById('marketTableBody')!;
  tbody.innerHTML = prices.map(p => {
    const isPositive = p.change >= 0;
    const sign = isPositive ? '+' : '';
    const cls = isPositive ? 'positive' : 'negative';
    return `
      <tr>
=======
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
>>>>>>> Stashed changes
        <td>
          <div class="table-commodity">
            <div class="table-commodity-icon">${p.symbol.substring(0, 2)}</div>
            <div>
              <div class="table-commodity-name">${p.name}</div>
              <div class="table-commodity-symbol">${p.symbol}</div>
            </div>
          </div>
        </td>
<<<<<<< Updated upstream
=======
        <td><span class="${contractCls}">${contractText}</span></td>
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
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
=======
// ─── Chart ──────────────────────────────────────────────
>>>>>>> Stashed changes

async function loadChart(symbol: string, days: number): Promise<void> {
  try {
    const container = document.getElementById('chartContainer')!;
<<<<<<< Updated upstream
    initChart(container);
=======
    if (!(window as any)['chartInit']) {
      initChart(container);
      (window as any)['chartInit'] = true;
    }
>>>>>>> Stashed changes

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
<<<<<<< Updated upstream
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
=======
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
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
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

=======
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
function renderFeaturedNews(a: NewsArticle): void {
  const el = document.getElementById('newsFeatured')!;
  el.innerHTML = `
    <article class="news-featured-card" data-url="${a.sourceUrl || '#'}">
=======
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
  renderNewsGrid(filtered.slice(1));
}

function renderFeaturedNews(article: NewsArticle): void {
  const el = document.getElementById('newsFeatured');
  if (!el) return;

  el.innerHTML = `
    <article class="news-featured-card" data-url="${article.sourceUrl || '#'}">
>>>>>>> Stashed changes
      <div class="news-featured-content">
        <div class="news-featured-badge">
          <span class="pulse"></span>
          Latest
        </div>
<<<<<<< Updated upstream
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
=======
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
>>>>>>> Stashed changes
      </div>
    </article>
  `;
}

<<<<<<< Updated upstream
function renderNewsCards(articles: NewsArticle[]): void {
  const grid = document.getElementById('newsGrid')!;
  grid.innerHTML = articles.map(a => `
    <article class="news-card" data-url="${a.sourceUrl || '#'}">
      <div class="news-card-top">
        <span class="news-category">${a.category}</span>
        <span class="news-time">${formatTimeAgo(a.publishedAt)}</span>
=======
function renderNewsGrid(articles: NewsArticle[]): void {
  const el = document.getElementById('newsGrid');
  if (!el) return;

  el.innerHTML = articles.map(a => `
    <article class="news-card" data-url="${a.sourceUrl || '#'}">
      <div class="news-card-top">
        <span class="news-category">${a.category}</span>
        <span class="news-time">${timeAgo(a.publishedAt)}</span>
>>>>>>> Stashed changes
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

<<<<<<< Updated upstream
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
=======
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
  // Price cards → commodity page
  document.getElementById('priceGrid')?.addEventListener('click', (e) => {
    const card = (e.target as HTMLElement).closest('.price-card') as HTMLElement;
    if (card?.dataset.symbol) {
      window.location.href = `/commodity/${card.dataset.symbol}`;
    }
  });

  // Hero price cards → commodity page
  document.querySelectorAll('.hero-price-card').forEach(el => {
    el.addEventListener('click', () => {
      const sym = (el as HTMLElement).dataset.symbol;
      if (sym) window.location.href = `/commodity/${sym}`;
    });
  });

  // Hero secondary prices → commodity page
  document.getElementById('heroSecondaryPrices')?.addEventListener('click', (e) => {
    const item = (e.target as HTMLElement).closest('.hero-secondary-item') as HTMLElement;
    if (item?.dataset.symbol) {
      window.location.href = `/commodity/${item.dataset.symbol}`;
    }
  });

  // Market table rows → commodity page
  document.getElementById('marketTableBody')?.addEventListener('click', (e) => {
    const row = (e.target as HTMLElement).closest('tr') as HTMLElement;
    if (row?.dataset.symbol) {
      window.location.href = `/commodity/${row.dataset.symbol}`;
    }
  });

  // Chart symbol buttons
  document.getElementById('chartSymbols')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-symbol-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-symbol-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentSymbol = btn.dataset.symbol || 'WTI';
    loadChart(currentSymbol, currentDays);
  });

  // Chart timeframe buttons
  document.getElementById('chartTimeframes')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-tf-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-tf-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentDays = parseInt(btn.dataset.days || '90', 10);
    loadChart(currentSymbol, currentDays);
  });

  // News cards → open source URL
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
>>>>>>> Stashed changes
}
