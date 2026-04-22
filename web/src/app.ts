import { getPrices, getChartData, getNews, getPredictions, getHeroChart, getConsensus } from './api';
import { initChart, updateChartData, subscribeCrosshair, createCandleChart, type CandleChartHandle, type SessionMarker, type SessionBand } from './charts';
import type { Price, ChartData, NewsArticle, OHLCV, Prediction, HeroChart, ConsensusForecast } from './types';

let currentSymbol = 'WTI';
let currentDays = 90;
let allNews: NewsArticle[] = [];
let currentCategory = 'all';

// Hero chart is a candlestick instance that always paints today's NY
// trading session at 5-min resolution (Yahoo intraday backfill + a live
// Pyth-driven in-progress bar). Kept independent of the larger chart in
// the #charts section so the two lightweight-charts instances don't
// collide on style or DOM.
let heroChart: CandleChartHandle | null = null;
// HERO_MAX_BARS is a generous upper bound for the bar count we'll
// accept from /api/hero. The server returns the full NY session so this
// is purely a sanity ceiling (a 23-hour session at 5-min ≈ 276 bars).
// Sent as the API's `max` param for backwards compat — the server now
// ignores it and always returns the full session.
const HERO_MAX_BARS = 600;
let heroPollTimer: number | null = null;
let heroMode: 'live' | 'today-paused' | 'prior-session' | 'warming-up' | null = null;
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
  loadAllData();
  setInterval(refreshPrices, 15000);
});

async function loadAllData(): Promise<void> {
  try {
    await Promise.all([
      loadPrices(),
      loadHeroChart(),
      loadChart(currentSymbol, currentDays),
      loadForecasts(),
      loadConsensus(),
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

// ─── Hero Chart (today's session, auto live/paused/prior) ──────────────
//
// The homepage hero chart spans today's NY trading session at 5-minute
// resolution. The server picks `mode`:
//   - "live": today's bars + Pyth driving the rightmost in-progress bar
//     in real time. Polled every 2s, only the last bar is mutated via
//     .update() so existing pan/zoom state is kept.
//   - "today-paused": today's bars but Pyth has gone quiet (CME daily
//     5–6 PM ET maintenance break, brief publisher hiccup). Same fast
//     poll cadence so we resume "live" the moment ticks return.
//   - "prior-session": no bars for today yet (pre-Sunday-reopen, full
//     weekend day). We show the most recent prior session and slow-poll
//     (60s) until today's bars appear.
//   - "warming-up": cold start — placeholder + fast polling so the first
//     real payload paints quickly.
//
// All mode decisions live on the server. The frontend just renders what
// the payload says and swaps UI chrome (pill / tagline).

async function loadHeroChart(): Promise<void> {
  const container = document.getElementById('heroChartContainer');
  if (!container) return;

  if (!heroChart) {
    heroChart = createCandleChart(container);
  }

  try {
    const payload = await getHeroChart(HERO_SYMBOL, HERO_MAX_BARS);
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
    // Mode change — wholesale replace the data so the chart redraws
    // with whatever interval / session the new mode brings. Also clears
    // any half-formed candle from the previous mode. Force a re-fit
    // because the new bar range is unrelated to whatever zoom level
    // the user had on the previous mode's data.
    heroChart.setData(payload.bars);
    heroChart.fitContent();
    heroMode = payload.mode;
    transitionPolling(payload.mode);
    applyHeroSessionOverlay(payload);
  } else if (payload.mode === 'live' || payload.mode === 'today-paused') {
    // Same intraday mode — only mutate the last bar so existing pan/zoom
    // state is preserved. In live mode the close ticks every 2s; in
    // today-paused it's effectively a no-op until ticks resume.
    const last = payload.bars[payload.bars.length - 1];
    heroChart.update(last);
  } else {
    // Same prior-session (or warming) — full replace is fine, the bar
    // set rarely changes (only when Yahoo backfills a late tick).
    heroChart.setData(payload.bars);
    applyHeroSessionOverlay(payload);
  }

  const last = payload.bars[payload.bars.length - 1];
  if (payload.mode === 'live') {
    setLiveState('live');
    const isNewTick = last.time !== lastSeenBarTime || last.close !== lastSeenBarClose;
    if (isNewTick) pulseLiveIndicator();
  } else if (payload.mode === 'today-paused') {
    setLiveState('today-paused', payload.sessionDate);
  } else if (payload.mode === 'prior-session') {
    setLiveState('paused', payload.sessionDate);
  } else {
    setLiveState('warming');
  }
  setHeroTagline(payload);
  lastSeenBarTime = last.time;
  lastSeenBarClose = last.close;
}

// ─── CME futures session-boundary markers ────────────────────────────
//
// CME Globex WTI runs Sun 18:00 ET → Fri 17:00 ET, with a daily 60-min
// maintenance break from 17:00 to 18:00 ET Mon–Thu. The natural absence
// of bars during that hour already shows "market closed", but a labelled
// vertical line on each transition makes the pattern impossible to miss
// at a glance — which is the whole point of the homepage hero.
//
// We compute the markers from the session date the server reports
// (already in NY-local time) so that a viewer in Tokyo and a viewer in
// London both see "17:00 ET — Daily close" labelled at the same candle.

// nyWallClockToUnix returns the unix-second timestamp for a given NY
// wall-clock date+time. Convergence-by-iteration handles DST transitions
// correctly without hard-coding offsets — guess UTC, format it as NY,
// adjust by the diff, repeat. Two passes is always enough in practice.
function nyWallClockToUnix(yyyy: number, mm: number, dd: number, hour: number, minute: number = 0): number {
  let guess = Date.UTC(yyyy, mm - 1, dd, hour, minute);
  for (let i = 0; i < 3; i++) {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone: 'America/New_York',
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', hour12: false,
    }).formatToParts(new Date(guess));
    const get = (t: string) => parseInt(parts.find(p => p.type === t)!.value, 10);
    const aY = get('year'), aM = get('month'), aD = get('day');
    let aH = get('hour'); if (aH === 24) aH = 0;
    const aMin = get('minute');
    const targetUtc = Date.UTC(yyyy, mm - 1, dd, hour, minute);
    const actualUtc = Date.UTC(aY, aM - 1, aD, aH, aMin);
    const diff = targetUtc - actualUtc;
    if (diff === 0) break;
    guess += diff;
  }
  return Math.floor(guess / 1000);
}

// weekdayNY returns 0=Sun..6=Sat for the given YYYY-MM-DD interpreted as
// a NY-local date. Anchored on NY-noon to stay clear of midnight and DST
// transition windows. Used to special-case Friday (no daily reopen) and
// the weekend days (no daily close).
function weekdayNY(yyyymmdd: string): number {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(yyyymmdd);
  if (!m) return -1;
  const noon = nyWallClockToUnix(
    parseInt(m[1]!, 10), parseInt(m[2]!, 10), parseInt(m[3]!, 10), 12, 0,
  );
  const wd = new Intl.DateTimeFormat('en-US', {
    timeZone: 'America/New_York', weekday: 'short',
  }).format(new Date(noon * 1000));
  return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].indexOf(wd);
}

// PIT_OPEN_HOUR / PIT_CLOSE_HOUR define the NYMEX crude-oil pit / floor
// session — the historical "institutional trading window" when the
// majority of price discovery still concentrates even in the modern
// electronic era. Hard-coded because it hasn't changed in decades and
// shouldn't be a per-request lookup.
const PIT_OPEN_HOUR = 9;       // 09:00 ET
const PIT_OPEN_MIN = 0;
const PIT_CLOSE_HOUR = 14;     // 14:30 ET
const PIT_CLOSE_MIN = 30;

function applyHeroSessionOverlay(payload: HeroChart): void {
  if (!heroChart) return;
  // Overlays only make sense for the rolling-24h intraday view. For
  // prior-session / warming-up modes there's no consistent anchor, so
  // clear both layers.
  if (payload.mode !== 'live' && payload.mode !== 'today-paused') {
    heroChart.setSessionMarkers([]);
    heroChart.setSessionBands([]);
    return;
  }
  const dateStr = payload.sessionDate;
  if (!dateStr) {
    heroChart.setSessionMarkers([]);
    heroChart.setSessionBands([]);
    return;
  }
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(dateStr);
  if (!m) return;
  const todayY = parseInt(m[1]!, 10);
  const todayM = parseInt(m[2]!, 10);
  const todayD = parseInt(m[3]!, 10);
  // The chart spans the rolling last 24 hours, so both yesterday's and
  // today's CME session boundaries can fall inside the view. Compute
  // overlays for both NY calendar days and clip to the rendered range.
  const yesterday = new Date(Date.UTC(todayY, todayM - 1, todayD));
  yesterday.setUTCDate(yesterday.getUTCDate() - 1);
  const yY = yesterday.getUTCFullYear();
  const yM = yesterday.getUTCMonth() + 1;
  const yD = yesterday.getUTCDate();

  const markers: SessionMarker[] = [];
  pushSessionMarkersForDay(markers, yY, yM, yD);
  pushSessionMarkersForDay(markers, todayY, todayM, todayD);

  const bands: SessionBand[] = [];
  pushPitBandForDay(bands, yY, yM, yD);
  pushPitBandForDay(bands, todayY, todayM, todayD);

  // Clip both layers to the time range we actually rendered, so we
  // don't paint e.g. tomorrow's pit window into empty space at the
  // right edge of the chart.
  const bars = payload.bars;
  if (bars.length > 0) {
    const minT = bars[0]!.time;
    const maxT = bars[bars.length - 1]!.time;
    heroChart.setSessionMarkers(
      markers.filter(mk => mk.time >= minT && mk.time <= maxT)
    );
    heroChart.setSessionBands(
      bands.filter(b => b.end >= minT && b.start <= maxT)
    );
  } else {
    heroChart.setSessionMarkers(markers);
    heroChart.setSessionBands(bands);
  }
}

// pushSessionMarkersForDay appends the CME WTI session-boundary markers
// for one NY-local calendar day. CME schedule reference:
//   - Mon–Thu: 17:00 ET = daily close,   18:00 ET = daily reopen (1h break)
//   - Friday : 17:00 ET = weekly close,  no reopen until Sunday 18:00 ET
//   - Saturday: closed all day
//   - Sunday  : 18:00 ET = weekly open,  no 17:00 close
//
// We deliberately ONLY emit the 17:00 ET "close" boundary — the matching
// 18:00 ET reopen is implicit (the next bar after the visible 1-hour gap
// IS the reopen) and adding a second vertical line for it just clutters
// the chart without adding information.
function pushSessionMarkersForDay(out: SessionMarker[], y: number, m: number, d: number): void {
  const dow = weekdayNY(`${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`);
  if (dow < 1 || dow > 5) return;
  out.push({
    time: nyWallClockToUnix(y, m, d, 17, 0),
    label: dow === 5 ? 'Weekly Close · 17:00 ET' : 'Close · 17:00 ET',
    kind: 'close',
  });
}

// pushPitBandForDay highlights the NYMEX crude-oil pit-hours window
// (09:00–14:30 ET, Mon–Fri) for one NY-local calendar day. This is the
// "institutional trading window" — when most volume historically prints,
// settlement reference prices form, and EIA inventory releases land
// (Wednesdays at 10:30 ET fall right inside it). We DON'T draw a band
// on Sat/Sun since there's no pit session at all on weekends.
function pushPitBandForDay(out: SessionBand[], y: number, m: number, d: number): void {
  const dow = weekdayNY(`${y}-${String(m).padStart(2, '0')}-${String(d).padStart(2, '0')}`);
  if (dow < 1 || dow > 5) return; // weekends: no pit
  out.push({
    start: nyWallClockToUnix(y, m, d, PIT_OPEN_HOUR, PIT_OPEN_MIN),
    end: nyWallClockToUnix(y, m, d, PIT_CLOSE_HOUR, PIT_CLOSE_MIN),
    label: 'NYMEX Pit · 09:00–14:30 ET',
    // Soft amber tint — high enough contrast against the dark chart bg
    // to be obvious, low enough alpha that the candles inside the band
    // remain perfectly readable.
    color: 'rgba(245, 158, 11, 0.07)',
  });
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
      accent.textContent = 'Streaming Real-time';
      accent.className = 'hero-tagline-accent hero-tagline-accent-live';
      break;
    case 'today-paused':
      // Today's bars are showing but Pyth is quiet — usually the daily
      // 5–6 PM ET CME maintenance break.
      accent.textContent = "today's session — feed paused (CME daily break)";
      accent.className = 'hero-tagline-accent hero-tagline-accent-paused';
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

function transitionPolling(mode: 'live' | 'today-paused' | 'prior-session' | 'warming-up'): void {
  if (heroPollTimer != null) {
    window.clearInterval(heroPollTimer);
    heroPollTimer = null;
  }
  // Fast-poll in every mode that's "intra-session" so we resume
  // ticking the moment Pyth wakes up. Only true prior-session
  // (markets fully closed) drops to slow polling.
  const interval = mode === 'prior-session' ? HERO_PAUSED_POLL_MS : HERO_LIVE_POLL_MS;
  heroPollTimer = window.setInterval(async () => {
    try {
      const payload = await getHeroChart(HERO_SYMBOL, HERO_MAX_BARS);
      applyHeroChartPayload(payload);
    } catch {
      // Transient network error — next tick will recover.
    }
  }, interval);
}

// setLiveState toggles the chart card's pill between four appearances.
// In paused / today-paused states the optional sessionDate is shown so
// users know which trading day's data they're looking at. CSS handles the
// visual treatment for each state.
function setLiveState(state: 'live' | 'today-paused' | 'paused' | 'warming', sessionDate?: string): void {
  const pill = document.getElementById('heroLivePill');
  if (!pill) return;
  const dot = pill.firstElementChild;
  const label = pill.lastChild!;
  pill.classList.remove('hero-live-pill-paused', 'hero-live-pill-warming');
  dot?.classList.remove('hero-live-dot-paused', 'hero-live-dot-warming');
  switch (state) {
    case 'live':
      label.textContent = ' LIVE \u00b7 5m';
      break;
    case 'today-paused':
      // Today's session is on screen but the live tick is paused —
      // typically the daily 5–6 PM ET CME maintenance break. We use the
      // same paused styling as full prior-session, but a more specific
      // label so users know data is current as of a few minutes ago.
      pill.classList.add('hero-live-pill-paused');
      dot?.classList.add('hero-live-dot-paused');
      label.textContent = ' FEED PAUSED \u00b7 today';
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
    const container = document.getElementById('chartContainer');
    if (!container) return; // page doesn't have the main chart panel
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
  const dirLabel =
    p.direction === 'bullish' ? 'Bullish bias' :
    p.direction === 'bearish' ? 'Bearish bias' :
    'Mixed signals';

  const confidencePct = Math.round((p.confidence || 0) * 100);
  const confidenceLabel =
    confidencePct >= 70 ? 'High' :
    confidencePct >= 50 ? 'Moderate' :
    confidencePct > 0 ? 'Low' :
    'Unavailable';

  const sourceLabelText = forecastSourceLabel(p.source);
  const modelLabel = p.model || 'statistical';

  const signalChips = buildSignalChipsFromPrediction(p)
    .map(c => `
      <li class="signal-chip signal-${c.tone}">
        <span class="signal-chip-label">${c.label}</span>
        <span class="signal-chip-value">${c.value}</span>
      </li>`)
    .join('');

  const backtestBlock = renderBacktestBlock(p);

  return `
    <article class="forecast-card forecast-${dirClass}" data-symbol="${p.symbol}" role="listitem">
      <header class="forecast-card-header">
        <div>
          <div class="forecast-card-symbol">${p.symbol}</div>
          <div class="forecast-card-name">${p.name}</div>
        </div>
        <span class="forecast-direction-badge ${dirClass}" aria-label="Outlook: ${dirLabel}">
          <span class="forecast-arrow">${arrow}</span>${dirLabel}
        </span>
      </header>

      <div class="forecast-spot">
        <div class="forecast-spot-label">Spot</div>
        <div class="forecast-spot-value">$${p.current.toFixed(2)}</div>
      </div>

      <ul class="signal-stack" aria-label="Technical signal stack">${signalChips}</ul>

      <details class="forecast-projection">
        <summary class="forecast-projection-summary">
          <span>Model projection · ${p.timeframe}</span>
          <span class="forecast-projection-headline forecast-${dirClass}-text">$${p.predicted.toFixed(2)} (${sign}${pct.toFixed(2)}%)</span>
        </summary>
        <div class="forecast-projection-body">
          <p class="forecast-projection-note">
            Damped-Holt point forecast ${p.timeframe} ahead, anchored to the live spot price.
            The signal stack above carries the bulk of the directional information; treat the
            dollar value as one input among many.
          </p>
          <div class="forecast-confidence">
            <div class="forecast-confidence-row">
              <span class="forecast-meta-label">Model confidence</span>
              <span class="forecast-confidence-value">${confidenceLabel} · ${confidencePct}%</span>
            </div>
            <div class="forecast-confidence-bar" role="progressbar" aria-valuenow="${confidencePct}" aria-valuemin="0" aria-valuemax="100">
              <div class="forecast-confidence-fill ${dirClass}" style="width: ${confidencePct}%"></div>
            </div>
          </div>
        </div>
      </details>

      ${backtestBlock}

      <p class="forecast-analysis">${p.analysis}</p>

      <footer class="forecast-card-footer">
        <span class="forecast-footnote">Model: <code>${modelLabel}</code></span>
        <span class="forecast-footnote">Source: ${sourceLabelText}</span>
      </footer>
    </article>
  `;
}

interface SignalChipView { label: string; value: string; tone: 'bullish' | 'bearish' | 'neutral'; }

function buildSignalChipsFromPrediction(p: Prediction): SignalChipView[] {
  const chips: SignalChipView[] = [];

  // Trend chip.
  let trendTone: 'bullish' | 'bearish' | 'neutral' = 'neutral';
  let trendValue = 'Sideways';
  if (p.trendLabel === 'uptrend') { trendTone = 'bullish'; trendValue = 'Uptrend'; }
  else if (p.trendLabel === 'downtrend') { trendTone = 'bearish'; trendValue = 'Downtrend'; }
  chips.push({ label: 'Trend', value: trendValue, tone: trendTone });

  // RSI / momentum chip.
  let rsiTone: 'bullish' | 'bearish' | 'neutral' = 'neutral';
  const rsiNum = Math.round(p.rsi14 ?? 50);
  let rsiValue = `RSI ${rsiNum}`;
  switch (p.rsiLabel) {
    case 'bullish': rsiTone = 'bullish'; rsiValue = 'Bullish'; break;
    case 'overbought': rsiTone = 'bearish'; rsiValue = 'Overbought'; break;
    case 'bearish': rsiTone = 'bearish'; rsiValue = 'Bearish'; break;
    case 'oversold': rsiTone = 'bullish'; rsiValue = 'Oversold'; break;
  }
  chips.push({ label: 'Momentum', value: rsiValue, tone: rsiTone });

  // MACD chip.
  let macdTone: 'bullish' | 'bearish' | 'neutral' = 'neutral';
  let macdValue = 'Flat';
  if (p.macdLabel === 'above signal (bullish)') { macdTone = 'bullish'; macdValue = 'Above signal'; }
  else if (p.macdLabel === 'below signal (bearish)') { macdTone = 'bearish'; macdValue = 'Below signal'; }
  chips.push({ label: 'MACD', value: macdValue, tone: macdTone });

  // MA configuration chip.
  let maTone: 'bullish' | 'bearish' | 'neutral' = 'neutral';
  let maValue = 'Range-bound';
  const cfg = p.maConfig || '';
  if (cfg.includes('golden-cross')) { maTone = 'bullish'; maValue = 'Golden cross'; }
  else if (cfg.includes('above 200DMA')) { maTone = 'bullish'; maValue = '50DMA > 200DMA'; }
  else if (cfg.includes('death-cross')) { maTone = 'bearish'; maValue = 'Death cross'; }
  else if (cfg.includes('below 200DMA')) { maTone = 'bearish'; maValue = '50DMA < 200DMA'; }
  chips.push({ label: 'Regime', value: maValue, tone: maTone });

  return chips;
}

function renderBacktestBlock(p: Prediction): string {
  const steps = p.backtestSteps || 0;
  const mape = p.mape || 0;
  if (steps <= 0 || mape <= 0) return '';

  const mapePct = mape * 100;
  const naivePct = (p.naiveMape || 0) * 100;
  const skillPct = (p.skill || 0) * 100;

  let verdict = 'Roughly matches naive baseline';
  if (skillPct >= 10) verdict = `Beats naive baseline by ${skillPct.toFixed(0)}%`;
  else if (skillPct <= -5) verdict = `Underperforms naive baseline by ${(-skillPct).toFixed(0)}%`;

  const sentence = `Out-of-sample backtest over the last ${steps} trading sessions: ${mapePct.toFixed(1)}% MAPE on ${p.timeframe} forecasts vs ${naivePct.toFixed(1)}% MAPE for a naive 'no change' baseline.`;
  const skillSign = skillPct >= 0 ? '+' : '';

  return `
    <div class="backtest-credibility" aria-label="Backtest credibility">
      <div class="backtest-verdict">
        <span class="backtest-verdict-icon" aria-hidden="true">📊</span>
        <span class="backtest-verdict-text">${verdict}</span>
      </div>
      <dl class="backtest-stats">
        <div class="backtest-stat"><dt>Model MAPE</dt><dd>${mapePct.toFixed(1)}%</dd></div>
        <div class="backtest-stat"><dt>Naive MAPE</dt><dd>${naivePct.toFixed(1)}%</dd></div>
        <div class="backtest-stat"><dt>Skill</dt><dd>${skillSign}${skillPct.toFixed(0)}%</dd></div>
        <div class="backtest-stat"><dt>Sessions</dt><dd>${steps}</dd></div>
      </dl>
      <p class="backtest-sentence">${sentence}</p>
    </div>
  `;
}

// ─── Institutional outlook (EIA STEO) ───────────────────

async function loadConsensus(): Promise<void> {
  const grid = document.getElementById('consensusGrid');
  if (!grid) return;
  try {
    const data = await getConsensus();
    renderConsensus(data || []);
  } catch (err) {
    console.error('Failed to load consensus outlook:', err);
    grid.innerHTML = `<p class="consensus-empty">Institutional outlook unavailable right now.</p>`;
  }
}

function renderConsensus(items: ConsensusForecast[]): void {
  const grid = document.getElementById('consensusGrid');
  if (!grid) return;

  if (!items.length) {
    grid.innerHTML = `<p class="consensus-empty">EIA Short-Term Energy Outlook will appear here when available.</p>`;
    return;
  }

  grid.innerHTML = items.map(consensusCardHtml).join('');
}

const CONSENSUS_NAMES: Record<string, string> = {
  WTI: 'WTI Crude Oil',
  BRENT: 'Brent Crude Oil',
  NATGAS: 'Henry Hub Natural Gas',
  HEATING: 'Heating Oil',
};

function consensusCardHtml(c: ConsensusForecast): string {
  const name = CONSENSUS_NAMES[c.symbol] || c.symbol;
  const months = (c.months || []).slice(0, 6);
  const release = formatConsensusDate(c.releaseDate);
  const rows = months.map(m => `
    <tr>
      <td>${formatConsensusPeriod(m.period)}</td>
      <td class="consensus-value">$${m.value.toFixed(2)}</td>
    </tr>`).join('');

  return `
    <article class="consensus-card" data-symbol="${c.symbol}">
      <header class="consensus-card-header">
        <div>
          <div class="consensus-card-symbol">${c.symbol}</div>
          <div class="consensus-card-name">${name}</div>
        </div>
        <span class="consensus-release">${release}</span>
      </header>
      <table class="consensus-table">
        <thead>
          <tr><th>Month</th><th style="text-align:right">Forecast (${c.unit || ''})</th></tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </article>
  `;
}

function formatConsensusPeriod(period: string): string {
  // "2026-05" -> "May 2026"
  const [y, m] = period.split('-');
  const idx = parseInt(m, 10) - 1;
  const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
  if (idx < 0 || idx > 11 || !y) return period;
  return `${months[idx]} ${y}`;
}

function formatConsensusDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return `Released ${d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })}`;
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
        return `<span class="source-pill source-pill-realtime" title="Streaming real-time exchange data">Real-Time</span>`;
      }
      return `<span class="source-pill source-pill-paused" title="Markets closed — showing last published tick">Last Tick</span>`;
    case 'yahoo':
      return `<span class="source-pill source-pill-delayed" title="Yahoo Finance — typically 15 minutes delayed from the exchange">15-min Delayed</span>`;
    default:
      return `<span class="source-pill source-pill-estimate" title="Indicative estimate (no live exchange feed available for this benchmark)">Estimate</span>`;
  }
}

function forecastSourceLabel(source?: string): string {
  switch (source) {
    case 'pyth':
      return 'Real-time streaming + Yahoo history';
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
