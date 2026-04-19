import { getPrices, getChartData, getNews, getPredictions, getHeroChart } from './api';
import { initChart, updateChartData, subscribeCrosshair, createCandleChart, type CandleChartHandle } from './charts';
import type { Price, ChartData, NewsArticle, OHLCV, Prediction, HeroChart } from './types';

let currentSymbol = 'WTI';
let currentDays = 90;
let allNews: NewsArticle[] = [];
let currentCategory = 'all';

// Hero chart is a candlestick instance shared between two modes (live Pyth
// 1-min vs prior-session Yahoo intraday). Independent of the larger chart
// in the #charts section so the two lightweight-charts instances don't
// collide on style or DOM.
let heroChart: CandleChartHandle | null = null;
// heroChartMinutes is the lookback window in minutes for LIVE mode (e.g.
// 60 = "show me the last hour of 1-min bars"). Ignored in prior-session
// mode, which always shows the full session.
let heroChartMinutes = 60;
let heroPollTimer: number | null = null;
let heroMode: 'live' | 'prior-session' | 'warming-up' | null = null;
const HERO_SYMBOL = 'WTI';
// HERO_LIVE_POLL_MS is how often we re-fetch the candle buffer in LIVE
// mode. 2s lines up with the Pyth poll cadence on the server, so each
// request typically picks up at least one new tick on the in-progress
// candle.
const HERO_LIVE_POLL_MS = 2000;
// HERO_PAUSED_POLL_MS is how often we re-fetch in prior-session mode. We
// poll much less aggressively because the data only changes when markets
// reopen — at which point we want to catch the transition within a minute.
const HERO_PAUSED_POLL_MS = 60_000;

// ─── Bootstrap ──────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
  setupNavigation();
  setupNewsFilters();
  setupClickHandlers();
  setupHeroChartTabs();
  loadAllData();
  setInterval(refreshPrices, 15000);
});

async function loadAllData(): Promise<void> {
  try {
    await Promise.all([
      loadPrices(),
      loadHeroChart(heroChartMinutes),
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

// ─── Hero (WTI chart header) ────────────────────────────

function renderHeroPrices(prices: Price[]): void {
  const wti = prices.find(p => p.symbol === HERO_SYMBOL);
  if (wti) renderHeroChartHeader(wti);
}

function renderHeroChartHeader(p: Price): void {
  const priceEl = document.getElementById('heroChartPrice');
  const changeEl = document.getElementById('heroChartChange');
  const contractEl = document.getElementById('heroChartContract');
  const sourceEl = document.getElementById('heroChartSource');
  const updatedEl = document.getElementById('heroChartUpdated');

  if (!priceEl) return;

  const positive = p.change >= 0;
  const sign = positive ? '+' : '';
  const arrow = positive ? '▲' : '▼';

  priceEl.textContent = `$${p.price.toFixed(2)}`;

  if (changeEl) {
    changeEl.textContent = `${arrow} ${sign}${p.change.toFixed(2)} (${sign}${p.changePct.toFixed(2)}%)`;
    changeEl.className = `hero-chart-change ${positive ? 'positive' : 'negative'}`;
  }

  if (contractEl) {
    contractEl.textContent = p.contract
      ? `${p.contract} — Front Month`
      : '';
  }

  if (sourceEl) {
    sourceEl.textContent = sourceLabel(p.source, p.updatedAt);
    sourceEl.className = `hero-chart-source source-${effectiveSource(p.source, p.updatedAt)}`;
  }

  if (updatedEl) {
    updatedEl.textContent = p.updatedAt ? timeAgo(p.updatedAt) : '';
  }
}

// ─── Hero Chart (auto live ↔ prior-session) ──────────────
//
// The homepage hero chart has two modes, picked SERVER-SIDE by /api/hero:
//   - "live": streaming 1-minute Pyth candles. Polled every 2s, only the
//     last bar is mutated via .update() so the chart visibly ticks. This
//     is the "yes this is real-time market data" experience.
//   - "prior-session": intraday Yahoo bars from the most recent complete
//     trading day, when Pyth is paused (weekend/holiday). Re-rendered with
//     setData() and slow-polled (60s) so the page automatically swaps
//     itself back to live mode the moment markets reopen.
//
// All mode decisions live on the server. The frontend just renders what
// the payload says and adjusts UI chrome (pill text, tab disable state).

function setupHeroChartTabs(): void {
  document.querySelectorAll<HTMLButtonElement>('#heroChartTabs .hero-tf-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const minutes = parseInt(btn.dataset['minutes'] || '60', 10);
      if (minutes === heroChartMinutes) return;
      heroChartMinutes = minutes;
      document.querySelectorAll('#heroChartTabs .hero-tf-btn').forEach(b => {
        b.classList.toggle('active', b === btn);
        b.setAttribute('aria-selected', b === btn ? 'true' : 'false');
      });
      // Tabs only meaningfully affect LIVE mode (lookback window). In
      // prior-session mode we always show the full session — but kicking
      // off another fetch costs nothing and keeps the code simple.
      loadHeroChart(minutes);
    });
  });
}

async function loadHeroChart(minutes: number): Promise<void> {
  const container = document.getElementById('heroChartContainer');
  if (!container) return;

  if (!heroChart) {
    heroChart = createCandleChart(container);
  }

  try {
    const payload = await getHeroChart(HERO_SYMBOL, minutes);
    applyHeroChartPayload(payload);
  } catch (err) {
    console.error('Failed to load hero chart:', err);
  }
}

// applyHeroChartPayload is the single point that paints the chart and the
// surrounding UI for a fresh /api/hero response. It also handles mode
// transitions (live ↔ prior-session) — when the mode changes we tear down
// the old polling cadence and start the appropriate new one.
let lastSeenBarTime = 0;
let lastSeenBarClose = 0;
function applyHeroChartPayload(payload: HeroChart): void {
  if (!heroChart) return;

  if (!payload.bars || payload.bars.length === 0) {
    setHeroChartEmpty(true);
    setLiveState('warming');
    setHeroTagline(payload);
    if (heroMode !== 'warming-up') {
      heroMode = 'warming-up';
      transitionPolling('warming-up');
    }
    return;
  }
  setHeroChartEmpty(false);

  if (payload.mode !== heroMode) {
    // Mode change — wholesale replace the data so the chart redraws with
    // the new interval (1m vs 5m bars look quite different). This also
    // clears any half-formed candle from the previous mode.
    heroChart.setData(payload.bars);
    heroMode = payload.mode;
    updateChartTabsAvailability(payload.mode);
    transitionPolling(payload.mode);
  } else if (payload.mode === 'live') {
    // Same mode and it's live — only mutate the last bar, leaving the
    // rest of the series untouched so existing pan/zoom state is kept.
    const last = payload.bars[payload.bars.length - 1];
    heroChart.update(last);
  } else {
    // Same prior-session mode — full replace is fine, the bar set rarely
    // changes (only when Yahoo backfills a late tick).
    heroChart.setData(payload.bars);
  }

  const last = payload.bars[payload.bars.length - 1];
  if (payload.mode === 'live') {
    setLiveState('live');
    const isNewTick = last.time !== lastSeenBarTime || last.close !== lastSeenBarClose;
    if (isNewTick) pulseLiveIndicator();
  } else if (payload.mode === 'prior-session') {
    setLiveState('paused', payload.sessionDate);
  } else {
    setLiveState('warming');
  }
  setHeroTagline(payload);
  lastSeenBarTime = last.time;
  lastSeenBarClose = last.close;
}

// setHeroTagline rewrites the hero subtitle to reflect what the user is
// actually looking at right now. We deliberately keep the lead-in stable
// ("Live oil prices & energy market data —") and only swap the accent
// span so layout doesn't visibly jump between renders.
function setHeroTagline(payload: HeroChart): void {
  const accent = document.getElementById('heroTaglineAccent');
  if (!accent) return;
  switch (payload.mode) {
    case 'live':
      accent.textContent =
        'streaming real-time WTI ticks via Pyth Network — markets are open';
      accent.className = 'hero-tagline-accent hero-tagline-accent-live';
      break;
    case 'prior-session':
      accent.textContent = payload.sessionDate
        ? 'markets are closed — showing the prior session (' +
          formatSessionDate(payload.sessionDate) +
          ') from Yahoo Finance'
        : 'markets are closed — showing the prior trading session from Yahoo Finance';
      accent.className = 'hero-tagline-accent hero-tagline-accent-paused';
      break;
    default:
      accent.textContent = 'connecting to the live feed…';
      accent.className = 'hero-tagline-accent hero-tagline-accent-warming';
  }
}

function transitionPolling(mode: 'live' | 'prior-session' | 'warming-up'): void {
  if (heroPollTimer != null) {
    window.clearInterval(heroPollTimer);
    heroPollTimer = null;
  }
  // 'warming-up' falls back to fast polling — we want to catch the first
  // tick the moment Pyth wakes up.
  const interval = mode === 'prior-session' ? HERO_PAUSED_POLL_MS : HERO_LIVE_POLL_MS;
  heroPollTimer = window.setInterval(async () => {
    try {
      const payload = await getHeroChart(HERO_SYMBOL, heroChartMinutes);
      applyHeroChartPayload(payload);
    } catch {
      // Transient network error — next tick will recover.
    }
  }, interval);
}

// updateChartTabsAvailability disables the lookback tabs when there's no
// LIVE stream to look back over. The tabs make no sense in prior-session
// mode (we always show the whole session) and would confuse the user.
function updateChartTabsAvailability(mode: 'live' | 'prior-session' | 'warming-up'): void {
  const tabs = document.querySelectorAll<HTMLButtonElement>('#heroChartTabs .hero-tf-btn');
  const disabled = mode !== 'live';
  tabs.forEach(b => {
    b.disabled = disabled;
    b.classList.toggle('disabled', disabled);
  });
}

// setLiveState toggles the chart card's pill between three appearances.
// In paused state, the optional sessionDate is shown so users know which
// trading day's data they're looking at. CSS handles the visual treatment
// for each state.
function setLiveState(state: 'live' | 'paused' | 'warming', sessionDate?: string): void {
  const pill = document.getElementById('heroLivePill');
  if (!pill) return;
  const dot = pill.firstElementChild;
  const label = pill.lastChild!;
  pill.classList.remove('hero-live-pill-paused', 'hero-live-pill-warming');
  dot?.classList.remove('hero-live-dot-paused', 'hero-live-dot-warming');
  switch (state) {
    case 'live':
      label.textContent = ' LIVE \u00b7 1m';
      break;
    case 'paused':
      pill.classList.add('hero-live-pill-paused');
      dot?.classList.add('hero-live-dot-paused');
      label.textContent = sessionDate
        ? ' MARKET CLOSED \u00b7 ' + formatSessionDate(sessionDate)
        : ' MARKET CLOSED';
      break;
    case 'warming':
      pill.classList.add('hero-live-pill-warming');
      dot?.classList.add('hero-live-dot-warming');
      label.textContent = ' WARMING UP';
      break;
  }
}

// formatSessionDate turns a "YYYY-MM-DD" string into a human label like
// "Fri Apr 17". We render in UTC to avoid the user's local TZ surprising
// them with a one-day shift around midnight (the underlying string is
// already in NYMEX-local time, not the viewer's).
function formatSessionDate(yyyymmdd: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(yyyymmdd);
  if (!m) return yyyymmdd;
  const d = new Date(Date.UTC(parseInt(m[1]!, 10), parseInt(m[2]!, 10) - 1, parseInt(m[3]!, 10)));
  return d.toLocaleDateString('en-US', { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' });
}

function setHeroChartEmpty(isEmpty: boolean): void {
  const empty = document.getElementById('heroChartEmpty');
  if (!empty) return;
  empty.style.display = isEmpty ? 'flex' : 'none';
}

// pulseLiveIndicator briefly flashes the green dot in the chart card header
// each time we receive a fresh candle update. The CSS handles the actual
// pulse animation; we just toggle a class for one frame.
function pulseLiveIndicator(): void {
  const dot = document.getElementById('heroLiveDot');
  if (!dot) return;
  dot.classList.remove('pulse-flash');
  void dot.offsetWidth;
  dot.classList.add('pulse-flash');
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
    const contractCls = p.source === 'estimate' || !p.source
      ? 'table-contract estimate'
      : 'table-contract';
    const sourceBadge = sourceBadgeHtml(p.source, p.updatedAt);

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
        <td>
          <span class="${contractCls}">${contractText}</span>
          ${sourceBadge}
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

// ─── Chart ──────────────────────────────────────────────

// chartLoadSeq guards against a stale response overwriting a newer one when
// the user clicks tabs faster than the network responds. Each loadChart()
// call bumps the counter and saves a local copy; if the in-flight fetch
// returns and the counter has moved on, we throw the result away.
let chartLoadSeq = 0;

async function loadChart(symbol: string, days: number): Promise<void> {
  const seq = ++chartLoadSeq;
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
    if (seq !== chartLoadSeq) return; // a newer click superseded us
    updateChartData(data.data);
    renderChartStats(data);
    primeChartOhlcDisplay(data);
  } catch (err) {
    console.error('Failed to load chart:', err);
    setError('Failed to load chart data. Please try again later.');
  }
}

// primeChartOhlcDisplay fills the Open/High/Low/Close/Volume row above the
// chart with the LATEST bar's values whenever a new dataset loads. Without
// this, the row keeps showing the last hovered candle from the previously
// selected commodity/timeframe — which makes it look to the user like the
// chart didn't actually update because the visible numbers are unchanged.
function primeChartOhlcDisplay(chartData: ChartData): void {
  const data = chartData.data;
  if (!data || data.length === 0) {
    ['chartOpen', 'chartHigh', 'chartLow', 'chartClose', 'chartVolume'].forEach(id => {
      const el = document.getElementById(id);
      if (el) el.textContent = '—';
    });
    return;
  }
  const last = data[data.length - 1]!;
  const setIf = (id: string, txt: string) => {
    const el = document.getElementById(id);
    if (el) el.textContent = txt;
  };
  setIf('chartOpen', `$${last.open.toFixed(2)}`);
  setIf('chartHigh', `$${last.high.toFixed(2)}`);
  setIf('chartLow', `$${last.low.toFixed(2)}`);
  setIf('chartClose', `$${last.close.toFixed(2)}`);
  setIf('chartVolume', formatVolume(last.volume));
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

  const sourceLabelText = forecastSourceLabel(p.source);
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
        <span class="forecast-footnote">Source: ${sourceLabelText}</span>
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

  document.getElementById('marketTableBody')?.addEventListener('click', (e) => {
    const row = (e.target as HTMLElement).closest('tr') as HTMLElement;
    if (row?.dataset.symbol) {
      window.location.href = `/commodity/${row.dataset.symbol}`;
    }
  });

  document.getElementById('chartSymbols')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-symbol-btn') as HTMLElement;
    if (!btn) return;
    const next = btn.dataset.symbol || 'WTI';
    if (next === currentSymbol) return; // no-op; avoids spamming the API and resetting OHLC
    document.querySelectorAll('.chart-symbol-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentSymbol = next;
    loadChart(currentSymbol, currentDays);
  });

  document.getElementById('chartTimeframes')?.addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('.chart-tf-btn') as HTMLElement;
    if (!btn) return;
    const next = parseInt(btn.dataset.days || '90', 10);
    if (next === currentDays) return; // no-op; avoids spamming the API and resetting OHLC
    document.querySelectorAll('.chart-tf-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    currentDays = next;
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

// PYTH_LIVE_WINDOW_MS is how recent a Pyth publish must be (relative to
// "now" in the browser) to qualify as a live tick. Outside this window the
// underlying CFD market is typically closed (e.g. weekend) and we treat the
// price as a paused last-known value rather than streaming.
const PYTH_LIVE_WINDOW_MS = 60_000;

function isPythLive(source?: string, updatedAt?: string): boolean {
  if (source !== 'pyth' || !updatedAt) return false;
  const age = Date.now() - new Date(updatedAt).getTime();
  return age >= 0 && age <= PYTH_LIVE_WINDOW_MS;
}

// effectiveSource collapses the (source, freshness) tuple into a single
// CSS-class-friendly key. We use this so the styling reacts to whether a
// Pyth quote is actively streaming or paused.
function effectiveSource(source?: string, updatedAt?: string): string {
  if (source === 'pyth') {
    return isPythLive(source, updatedAt) ? 'pyth' : 'pyth-paused';
  }
  return source || 'estimate';
}

// sourceLabel returns the short, human-readable label shown beneath the hero
// price. The colour treatment is applied via the `source-${value}` class set
// on the same element.
function sourceLabel(source?: string, updatedAt?: string): string {
  switch (source) {
    case 'pyth':
      return isPythLive(source, updatedAt)
        ? 'NYMEX / ICE • Real-Time'
        : 'NYMEX / ICE • Last Tick';
    case 'yahoo':
      return 'NYMEX / ICE • 15-min Delayed';
    default:
      return 'Estimate';
  }
}

// sourceBadgeHtml returns a small inline pill displayed next to the contract
// label inside the market table so users can immediately see which rows are
// real-time and which are delayed/estimated.
function sourceBadgeHtml(source?: string, updatedAt?: string): string {
  switch (source) {
    case 'pyth':
      if (isPythLive(source, updatedAt)) {
        return `<span class="source-pill source-pill-realtime" title="Streamed from Pyth Network publishers (CME, Cboe, Jane Street, ...)">Real-Time</span>`;
      }
      return `<span class="source-pill source-pill-paused" title="Pyth feed (markets closed — showing last published tick)">Last Tick</span>`;
    case 'yahoo':
      return `<span class="source-pill source-pill-delayed" title="Yahoo Finance — typically 15 minutes delayed from the exchange">15-min Delayed</span>`;
    default:
      return `<span class="source-pill source-pill-estimate" title="Indicative estimate (no live exchange feed available for this benchmark)">Estimate</span>`;
  }
}

function forecastSourceLabel(source?: string): string {
  switch (source) {
    case 'pyth':
      return 'Pyth Network (real-time) + Yahoo history';
    case 'yahoo':
      return 'Yahoo Finance (15-min delayed)';
    default:
      return 'Estimate';
  }
}

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
