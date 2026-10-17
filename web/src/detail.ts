import { getPrices, getChartData, getNews } from './api';
import { initChart, updateChartData, subscribeCrosshair } from './charts';
import type { Price, ChartData, NewsArticle, OHLCV } from './types';

let currentSymbol = '';
let currentDays = 90;

// ─── Bootstrap ──────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
  const parts = window.location.pathname.split('/');
  currentSymbol = (parts[parts.length - 1] || 'WTI').toUpperCase();

  setupNav();
  setupTimeframes();
  loadAll();
  setInterval(() => loadPriceHeader(), 15000);
});

function setupNav(): void {
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
}

function setupTimeframes(): void {
  document.getElementById('chartTimeframes')!.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-tf-btn') as HTMLElement;
    if (!btn) return;
    document.querySelectorAll('.chart-tf-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentDays = parseInt(btn.dataset.days!);
    loadChart();
  });
}

async function loadAll(): Promise<void> {
  await Promise.all([
    loadPriceHeader(),
    loadChart(),
    loadRelatedNews(),
    loadOtherCommodities(),
  ]);
}

// ─── Price Header ───────────────────────────────────────

async function loadPriceHeader(): Promise<void> {
  try {
    const prices = await getPrices();
    const p = prices.find(x => x.symbol === currentSymbol);
    if (!p) return;
    
    // Don't update DOM if price unavailable - leave server-rendered '—'
    if (!p.available) {
      console.log('Price unavailable, keeping server-rendered content');
      return;
    }

    const sign2 = p.change >= 0 ? '+' : '';
    document.title = `${p.name} Price Today $${p.price.toFixed(2)} (${sign2}${p.changePct.toFixed(2)}%) — Live Oil Prices`;

    const metaDesc = document.querySelector('meta[name="description"]');
    if (metaDesc) metaDesc.setAttribute('content',
      `${p.name} price today is $${p.price.toFixed(2)} (${sign2}${p.changePct.toFixed(2)}%). Live ${p.symbol} chart, latest data, day high $${p.high.toFixed(2)}, day low $${p.low.toFixed(2)}.`);

    const canonical = document.querySelector('link[rel="canonical"]');
    if (!canonical) {
      const link = document.createElement('link');
      link.rel = 'canonical';
      link.href = `https://www.liveoilprices.com/commodity/${p.symbol}`;
      document.head.appendChild(link);
    }

    document.getElementById('breadcrumbName')!.textContent = p.name;
    document.getElementById('detailSymbol')!.textContent = p.symbol;
    document.getElementById('detailName')!.textContent = p.name;

    const isPos = p.change >= 0;
    const sign = isPos ? '+' : '';

    document.getElementById('detailPrice')!.textContent = `$${p.price.toFixed(2)}`;

    const changeEl = document.getElementById('detailChange')!;
    changeEl.textContent = `${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)`;
    changeEl.className = `detail-change ${isPos ? 'positive' : 'negative'}`;

    const statsEl = document.getElementById('detailStats')!;
    statsEl.innerHTML = `
      <div class="detail-stat-item">
        <span class="detail-stat-label">Day High</span>
        <span class="detail-stat-val">$${p.high.toFixed(2)}</span>
      </div>
      <div class="detail-stat-item">
        <span class="detail-stat-label">Day Low</span>
        <span class="detail-stat-val">$${p.low.toFixed(2)}</span>
      </div>
      <div class="detail-stat-item">
        <span class="detail-stat-label">Volume</span>
        <span class="detail-stat-val">${formatVolume(p.volume)}</span>
      </div>
      <div class="detail-stat-item">
        <span class="detail-stat-label">Day Range</span>
        <span class="detail-stat-val">$${p.low.toFixed(2)} — $${p.high.toFixed(2)}</span>
      </div>
    `;
  } catch (e) {
    console.error('Failed to load price header:', e);
  }
}

// ─── Chart ──────────────────────────────────────────────

async function loadChart(): Promise<void> {
  try {
    const container = document.getElementById('chartContainer')!;
    
    const data = await getChartData(currentSymbol, currentDays);
    
    // If no data available, show unavailable message
    if (!data.data || data.data.length === 0) {
      container.innerHTML = `
        <div style="display: flex; flex-direction: column; align-items: center; justify-content: center; height: 400px; color: var(--text-secondary);">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" style="opacity: 0.5; margin-bottom: 16px;">
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
          <div style="font-size: 16px; font-weight: 600;">Chart data unavailable</div>
          <div style="font-size: 14px; margin-top: 8px;">No historical data for ${data.name}</div>
        </div>
      `;
      document.getElementById('chartTitle')!.textContent = `${data.name} Price Chart`;
      const statsEl = document.getElementById('chartStats')!;
      statsEl.innerHTML = '';
      return;
    }
    
    initChart(container);

    subscribeCrosshair((o, h, l, c, v) => {
      document.getElementById('chartOpen')!.textContent = `$${o.toFixed(2)}`;
      document.getElementById('chartHigh')!.textContent = `$${h.toFixed(2)}`;
      document.getElementById('chartLow')!.textContent = `$${l.toFixed(2)}`;
      document.getElementById('chartClose')!.textContent = `$${c.toFixed(2)}`;
      document.getElementById('chartVolume')!.textContent = formatVolume(v);
    });

    updateChartData(data.data);
    document.getElementById('chartTitle')!.textContent = `${data.name} Price Chart`;
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
  const periodChangePct = (periodChange / first.open) * 100;
  const isPos = periodChange >= 0;
  const sign = isPos ? '+' : '';
  const cls = isPos ? 'positive' : 'negative';

  let periodHigh = -Infinity, periodLow = Infinity, totalVol = 0;
  d.forEach((c: OHLCV) => {
    if (c.high > periodHigh) periodHigh = c.high;
    if (c.low < periodLow) periodLow = c.low;
    totalVol += c.volume;
  });

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
      <span class="chart-stat-val">${formatVolume(totalVol / d.length)}</span>
    </div>
    <div class="chart-stat-item">
      <span class="chart-stat-label">Last Close</span>
      <span class="chart-stat-val">$${last.close.toFixed(2)}</span>
    </div>
  `;
}

// ─── Related News ───────────────────────────────────────

const SYMBOL_NEWS_MAP: Record<string, string[]> = {
  'WTI': ['OPEC', 'Supply', 'Analysis', 'Inventory', 'Demand'],
  'BRENT': ['OPEC', 'Geopolitical', 'Supply', 'Demand'],
  'NATGAS': ['Natural Gas'],
  'HEATING': ['Refining', 'Demand', 'Inventory'],
  'RBOB': ['Refining', 'Demand', 'Inventory'],
  'OPEC': ['OPEC'],
  'DUBAI': ['OPEC', 'Geopolitical'],
  'MURBAN': ['OPEC', 'Geopolitical'],
  'WCS': ['Supply'],
  'GASOIL': ['Refining', 'Demand'],
};

async function loadRelatedNews(): Promise<void> {
  try {
    const news = await getNews();
    const categories = SYMBOL_NEWS_MAP[currentSymbol] || [];
    let related = news.filter(a => categories.includes(a.category));
    if (related.length < 3) related = news.slice(0, 6);

    const title = document.getElementById('newsTitle')!;
    title.textContent = `Latest News`;

    const grid = document.getElementById('newsGrid')!;
    grid.innerHTML = related.map(a => `
      <a href="${a.sourceUrl || '#'}" target="_blank" rel="noopener" class="news-card-link">
        <article class="news-card">
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
      </a>
    `).join('');
  } catch (e) {
    console.error('Failed to load news:', e);
  }
}

// ─── Other Commodities ──────────────────────────────────

async function loadOtherCommodities(): Promise<void> {
  try {
    const prices = await getPrices();
    // Filter out unavailable prices and the current symbol
    const others = prices
      .filter(p => p.symbol !== currentSymbol && p.available)
      .slice(0, 6);

    const grid = document.getElementById('relatedGrid')!;
    
    if (others.length === 0) {
      grid.innerHTML = '<p class="text-muted" style="padding: 2rem; text-align: center;">Market data temporarily unavailable</p>';
      return;
    }
    
    grid.innerHTML = others.map(p => {
      const isPos = p.change >= 0;
      const sign = isPos ? '+' : '';
      const cls = isPos ? 'positive' : 'negative';
      return `
        <a href="/commodity/${p.symbol}" class="related-card">
          <div class="related-card-top">
            <span class="related-symbol">${p.symbol}</span>
            <span class="related-name">${p.name}</span>
          </div>
          <div class="related-card-bottom">
            <span class="related-price">$${p.price.toFixed(2)}</span>
            <span class="related-change ${cls}">${sign}${p.changePct.toFixed(2)}%</span>
          </div>
        </a>
      `;
    }).join('');
  } catch (e) {
    console.error('Failed to load other commodities:', e);
  }
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
