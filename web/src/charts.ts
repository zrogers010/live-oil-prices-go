import { createChart, ColorType, CrosshairMode, LineStyle, type IChartApi, type ISeriesApi } from 'lightweight-charts';
import type { OHLCV, PythCandle } from './types';

// ─── New-York-time (NYMEX/ICE exchange-local) formatters ────────────
//
// All commodities we render (WTI, Brent, NatGas, Heating, RBOB, etc.)
// are dated in America/New_York exchange time on the venue. The viewer's
// local timezone is irrelevant — a 14:30 NY tick is the same bar whether
// you're in Sydney or Berlin — so we force every chart on the site to
// label its time axis in ET. Otherwise a EU visitor would see a "2 PM"
// candle that doesn't match any NY-time chart anywhere else.
//
// lightweight-charts hands us UTC unix seconds; we convert with
// Intl.DateTimeFormat which DOES handle DST correctly for ET.
//
// `tickMarkFormatter` controls the labels along the bottom axis;
// `localization.timeFormatter` controls the date/time string in the
// crosshair tooltip when the user hovers over a candle.

const NY_TIMEZONE = 'America/New_York';

const nyTimeFmt = new Intl.DateTimeFormat('en-US', {
  timeZone: NY_TIMEZONE, hour: 'numeric', minute: '2-digit', hour12: false,
});
const nyDateFmt = new Intl.DateTimeFormat('en-US', {
  timeZone: NY_TIMEZONE, month: 'short', day: 'numeric',
});
const nyMonthFmt = new Intl.DateTimeFormat('en-US', {
  timeZone: NY_TIMEZONE, month: 'short', year: 'numeric',
});
const nyYearFmt = new Intl.DateTimeFormat('en-US', {
  timeZone: NY_TIMEZONE, year: 'numeric',
});
const nyFullFmt = new Intl.DateTimeFormat('en-US', {
  timeZone: NY_TIMEZONE,
  year: 'numeric', month: 'short', day: 'numeric',
  hour: 'numeric', minute: '2-digit', hour12: false,
});

// lightweight-charts' TickMarkType enum (kept as raw integers because v4
// doesn't reliably re-export the enum value at runtime in all bundlers).
//   0 = Year, 1 = Month, 2 = DayOfMonth, 3 = Time, 4 = TimeWithSeconds
function nyTickMarkFormatter(time: any, tickMarkType: number): string {
  const seconds = typeof time === 'number'
    ? time
    : (time && typeof time === 'object' && 'year' in time)
      ? Date.UTC(time.year, time.month - 1, time.day) / 1000
      : Number(time);
  const d = new Date(seconds * 1000);
  switch (tickMarkType) {
    case 0: return nyYearFmt.format(d);
    case 1: return nyMonthFmt.format(d);
    case 2: return nyDateFmt.format(d);
    case 3:
    case 4:
    default: return nyTimeFmt.format(d);
  }
}

function nyCrosshairTimeFormatter(time: any): string {
  const seconds = typeof time === 'number'
    ? time
    : (time && typeof time === 'object' && 'year' in time)
      ? Date.UTC(time.year, time.month - 1, time.day) / 1000
      : Number(time);
  return nyFullFmt.format(new Date(seconds * 1000)) + ' ET';
}

let chart: IChartApi | null = null;
let candleSeries: ISeriesApi<'Candlestick'> | null = null;
let volumeSeries: ISeriesApi<'Histogram'> | null = null;
let resizeObserver: ResizeObserver | null = null;
// crosshairCallback is the single live consumer of the main chart's
// crosshair-move events. We subscribe to lightweight-charts ONCE inside
// initChart() and dispatch to whatever callback is currently registered;
// callers swap their callback in via subscribeCrosshair() instead of
// stacking another lightweight-charts subscription on top, which would
// pile up one extra handler per chart-tab click.
type CrosshairCallback = (o: number, h: number, l: number, c: number, v: number) => void;
let crosshairCallback: CrosshairCallback | null = null;

/** Handle returned by createAreaChart so multiple area-chart instances can
 *  coexist (the page has both a hero chart and the main candlestick chart). */
export interface AreaChartHandle {
  chart: IChartApi;
  series: ISeriesApi<'Area'>;
  destroy(): void;
  setData(data: OHLCV[]): void;
}

/** Handle returned by createCandleChart for the live streaming hero chart.
 *  setData replaces the entire series; update merges a single bar (used to
 *  push the in-progress 1-minute candle on each poll without re-rendering
 *  the whole series). setSessionMarkers paints labelled vertical lines on
 *  top of the chart at fixed wall-clock times (CME open/close transitions);
 *  setSessionBands paints translucent shaded rectangles over time RANGES
 *  (e.g. the NYMEX pit-hours / institutional trading window). */
export interface CandleChartHandle {
  chart: IChartApi;
  series: ISeriesApi<'Candlestick'>;
  destroy(): void;
  setData(bars: PythCandle[]): void;
  update(bar: PythCandle): void;
  setSessionMarkers(markers: SessionMarker[]): void;
  setSessionBands(bands: SessionBand[]): void;
  /** Force a full fit-content re-zoom. Use after a wholesale mode change
   *  (e.g. live → prior-session) where the new data range is unrelated
   *  to whatever zoom level the user had before. */
  fitContent(): void;
}

/** SessionMarker draws a labelled vertical line at a wall-clock time on
 *  the hero chart. Used to mark the CME daily 5–6 PM ET maintenance break
 *  (and weekly Sunday-open / Friday-close on weekend boundaries). */
export interface SessionMarker {
  time: number;            // unix seconds
  label: string;           // short label, e.g. "Close 17:00 ET"
  kind: 'open' | 'close';  // controls the marker color
}

/** SessionBand draws a translucent shaded rectangle covering [start, end]
 *  on the chart. Used to highlight the NYMEX pit-hours / institutional
 *  trading window (09:00–14:30 ET) where most volume historically prints. */
export interface SessionBand {
  start: number;  // unix seconds (inclusive)
  end: number;    // unix seconds (exclusive)
  label: string;  // small caption shown at the top of the band
  color: string;  // any valid CSS color (translucent recommended)
}

export function initChart(container: HTMLElement): void {
  if (chart) {
    chart.remove();
    chart = null;
  }
  if (resizeObserver) {
    resizeObserver.disconnect();
    resizeObserver = null;
  }

  chart = createChart(container, {
    layout: {
      background: { type: ColorType.Solid, color: 'transparent' },
      textColor: '#94a3b8',
      fontFamily: "'Inter', -apple-system, sans-serif",
      fontSize: 12,
      attributionLogo: false,
    },
    localization: {
      timeFormatter: nyCrosshairTimeFormatter,
    },
    grid: {
      vertLines: { color: 'rgba(255, 255, 255, 0.04)' },
      horzLines: { color: 'rgba(255, 255, 255, 0.04)' },
    },
    crosshair: {
      mode: CrosshairMode.Normal,
      vertLine: {
        color: 'rgba(59, 130, 246, 0.3)',
        labelBackgroundColor: '#3b82f6',
      },
      horzLine: {
        color: 'rgba(59, 130, 246, 0.3)',
        labelBackgroundColor: '#3b82f6',
      },
    },
    rightPriceScale: {
      borderColor: 'rgba(255, 255, 255, 0.06)',
      scaleMargins: { top: 0.1, bottom: 0.2 },
    },
    timeScale: {
      borderColor: 'rgba(255, 255, 255, 0.06)',
      timeVisible: true,
      secondsVisible: false,
      fixLeftEdge: true,
      fixRightEdge: true,
      lockVisibleTimeRangeOnResize: true,
      tickMarkFormatter: nyTickMarkFormatter,
    },
    handleScroll: {
      vertTouchDrag: false,
      mouseWheel: false,
      pressedMouseMove: true,
      horzTouchDrag: true,
    },
    handleScale: {
      mouseWheel: true,
      pinch: true,
      axisPressedMouseMove: true,
      axisDoubleClickReset: true,
    },
  } as any);

  candleSeries = chart.addCandlestickSeries({
    upColor: '#10b981',
    downColor: '#ef4444',
    borderDownColor: '#ef4444',
    borderUpColor: '#10b981',
    wickDownColor: '#ef4444',
    wickUpColor: '#10b981',
  });

  volumeSeries = chart.addHistogramSeries({
    priceFormat: { type: 'volume' },
    priceScaleId: '',
  });

  volumeSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.85, bottom: 0 },
  });

  resizeObserver = new ResizeObserver(entries => {
    for (const entry of entries) {
      const { width, height } = entry.contentRect;
      chart?.applyOptions({ width, height });
    }
  });
  resizeObserver.observe(container);

  // Subscribe ONCE to crosshair moves and dispatch to whatever callback
  // is currently active. Re-subscribing on every chart switch would leak
  // handlers and fire the same DOM update N times per mouse move.
  chart.subscribeCrosshairMove(param => {
    if (!param.time || !candleSeries || !crosshairCallback) return;
    const data = param.seriesData.get(candleSeries) as any;
    if (data) {
      crosshairCallback(data.open, data.high, data.low, data.close, data.value ?? 0);
    }
  });
}

export function updateChartData(data: OHLCV[]): void {
  if (!candleSeries || !volumeSeries) return;

  const candles = data.map(d => ({
    time: d.time as any,
    open: d.open,
    high: d.high,
    low: d.low,
    close: d.close,
  }));

  const volumes = data.map(d => ({
    time: d.time as any,
    value: d.volume,
    color: d.close >= d.open
      ? 'rgba(16, 185, 129, 0.2)'
      : 'rgba(239, 68, 68, 0.2)',
  }));

  candleSeries.setData(candles);
  volumeSeries.setData(volumes);
  chart?.timeScale().fitContent();
}

export function subscribeCrosshair(callback: CrosshairCallback): void {
  // Single-slot replace, NOT additive — the lightweight-charts subscription
  // itself is set up once in initChart().
  crosshairCallback = callback;
}

/**
 * createAreaChart spins up an independent lightweight-charts instance with a
 * single gradient area series. Designed for the hero chart, where we want a
 * sleek line/area view of recent price action rather than full candlesticks.
 *
 * Returns a handle so the caller can update data, destroy, and so multiple
 * area charts can coexist on the page without colliding with the main chart
 * module-level singletons used by initChart()/updateChartData().
 */
export function createAreaChart(container: HTMLElement): AreaChartHandle {
  const c = createChart(container, {
    layout: {
      background: { type: ColorType.Solid, color: 'transparent' },
      textColor: '#94a3b8',
      fontFamily: "'Inter', -apple-system, sans-serif",
      fontSize: 11,
      attributionLogo: false,
    },
    localization: {
      timeFormatter: nyCrosshairTimeFormatter,
    },
    grid: {
      vertLines: { visible: false },
      horzLines: { color: 'rgba(255, 255, 255, 0.04)' },
    },
    crosshair: {
      mode: CrosshairMode.Magnet,
      vertLine: {
        color: 'rgba(59, 130, 246, 0.35)',
        width: 1,
        style: LineStyle.Solid,
        labelBackgroundColor: '#3b82f6',
      },
      horzLine: {
        color: 'rgba(59, 130, 246, 0.35)',
        width: 1,
        style: LineStyle.Solid,
        labelBackgroundColor: '#3b82f6',
      },
    },
    rightPriceScale: {
      borderColor: 'rgba(255, 255, 255, 0.06)',
      scaleMargins: { top: 0.18, bottom: 0.05 },
    },
    timeScale: {
      borderColor: 'rgba(255, 255, 255, 0.06)',
      timeVisible: false,
      secondsVisible: false,
      fixLeftEdge: true,
      fixRightEdge: true,
      lockVisibleTimeRangeOnResize: true,
      tickMarkFormatter: nyTickMarkFormatter,
    },
    handleScroll: {
      vertTouchDrag: false,
      mouseWheel: false,
      pressedMouseMove: false,
      horzTouchDrag: true,
    },
    handleScale: {
      mouseWheel: false,
      pinch: true,
      axisPressedMouseMove: false,
      axisDoubleClickReset: true,
    },
  } as any);

  const series = c.addAreaSeries({
    lineColor: '#60a5fa',
    lineWidth: 2,
    topColor: 'rgba(59, 130, 246, 0.55)',
    bottomColor: 'rgba(59, 130, 246, 0.0)',
    crosshairMarkerRadius: 5,
    crosshairMarkerBorderColor: '#0b1120',
    crosshairMarkerBackgroundColor: '#60a5fa',
    priceLineVisible: true,
    priceLineColor: 'rgba(96, 165, 250, 0.4)',
    priceLineWidth: 1,
    priceLineStyle: LineStyle.Dashed,
    lastValueVisible: false,
  });

  const ro = new ResizeObserver(entries => {
    for (const entry of entries) {
      const { width, height } = entry.contentRect;
      c.applyOptions({ width, height });
    }
  });
  ro.observe(container);

  return {
    chart: c,
    series,
    setData(data: OHLCV[]) {
      const points = data
        .filter(d => Number.isFinite(d.close) && d.close > 0)
        .map(d => ({ time: d.time as any, value: d.close }));
      series.setData(points);
      c.timeScale().fitContent();
    },
    destroy() {
      ro.disconnect();
      c.remove();
    },
  };
}

/**
 * createCandleChart spins up a standalone candlestick instance for the
 * homepage hero. The chart is sized to always show ALL bars in the loaded
 * series (today's NY trading session) without any horizontal scrolling —
 * we lean on lightweight-charts' fitContent() to compute barSpacing
 * dynamically so a 30-bar morning slice and a 280-bar full-day session
 * both render flush edge-to-edge.
 *
 *  1. `setData` replaces the whole series and re-fits.
 *  2. `update` mutates the in-progress bar in place (cheap, preserves
 *     the chart's rendered state) and re-fits only when the bucket
 *     count actually changes — typically once every 5 minutes — so we
 *     don't visibly breathe between intra-bucket ticks.
 *  3. `setSessionMarkers` paints labelled vertical lines on top of the
 *     chart via an absolutely-positioned overlay. lightweight-charts v4
 *     has no native vertical-line primitive, so the overlay reads
 *     timeScale().timeToCoordinate(t) to position each line and
 *     re-renders on every visible-range change + container resize.
 */
export function createCandleChart(container: HTMLElement): CandleChartHandle {
  const c = createChart(container, {
    layout: {
      background: { type: ColorType.Solid, color: 'transparent' },
      textColor: '#94a3b8',
      fontFamily: "'Inter', -apple-system, sans-serif",
      fontSize: 11,
      attributionLogo: false,
    },
    localization: {
      timeFormatter: nyCrosshairTimeFormatter,
    },
    grid: {
      vertLines: { color: 'rgba(255, 255, 255, 0.03)' },
      horzLines: { color: 'rgba(255, 255, 255, 0.05)' },
    },
    crosshair: {
      mode: CrosshairMode.Normal,
      vertLine: {
        color: 'rgba(59, 130, 246, 0.35)',
        width: 1,
        labelBackgroundColor: '#3b82f6',
      },
      horzLine: {
        color: 'rgba(59, 130, 246, 0.35)',
        width: 1,
        labelBackgroundColor: '#3b82f6',
      },
    },
    rightPriceScale: {
      borderColor: 'rgba(255, 255, 255, 0.06)',
      scaleMargins: { top: 0.12, bottom: 0.08 },
    },
    timeScale: {
      borderColor: 'rgba(255, 255, 255, 0.06)',
      timeVisible: true,
      secondsVisible: false,
      // Reserve ~4 bar-widths of empty space to the right of the most
      // recent candle. At a 5-min resolution that's about 20 minutes
      // of breathing room, which keeps the live bar from butting up
      // against the price axis without wasting screen real estate.
      rightOffset: 4,
      // Lock the right edge to "now" so wheel-zoom or click-drag can't
      // push the latest candle off the chart (or pan into empty future
      // space beyond the rightOffset margin). Users can still pan LEFT
      // freely to inspect older bars, and double-clicking the time
      // axis snaps back to a full fit.
      fixRightEdge: true,
      // When a new bucket arrives via update(), slide the visible
      // window forward by the bucket's width instead of compressing
      // every bar to fit. This preserves whatever zoom level the user
      // has dialled in while still keeping the live bar on-screen.
      shiftVisibleRangeOnNewBar: true,
      tickMarkFormatter: nyTickMarkFormatter,
    },
    handleScroll: {
      vertTouchDrag: false,
      mouseWheel: false,
      pressedMouseMove: true,
      horzTouchDrag: true,
    },
    handleScale: {
      mouseWheel: true,
      pinch: true,
      axisPressedMouseMove: true,
      axisDoubleClickReset: true,
    },
  } as any);

  const series = c.addCandlestickSeries({
    upColor: '#10b981',
    downColor: '#ef4444',
    borderUpColor: '#10b981',
    borderDownColor: '#ef4444',
    wickUpColor: '#10b981',
    wickDownColor: '#ef4444',
    priceLineVisible: true,
    priceLineColor: 'rgba(96, 165, 250, 0.5)',
    priceLineWidth: 1,
    priceLineStyle: LineStyle.Dashed,
    lastValueVisible: true,
  });

  // ─── Session-marker overlay ──────────────────────────────────────
  // Absolutely-positioned div sitting on top of the chart, used to draw
  // labelled vertical lines at wall-clock event times (CME open/close).
  // It receives no pointer events so the chart's crosshair / pan / scale
  // gestures keep working. Position is recomputed on every visible-range
  // change AND every container resize so the lines stay glued to the
  // correct candle.
  const containerStyle = window.getComputedStyle(container);
  if (containerStyle.position === 'static') {
    container.style.position = 'relative';
  }
  const overlay = document.createElement('div');
  overlay.className = 'hero-chart-session-overlay';
  overlay.style.cssText = [
    'position:absolute',
    'inset:0',
    'pointer-events:none',
    'overflow:hidden',
  ].join(';');
  container.appendChild(overlay);

  let sessionMarkers: SessionMarker[] = [];
  let sessionBands: SessionBand[] = [];

  function repaintOverlay(): void {
    overlay.innerHTML = '';
    if (sessionMarkers.length === 0 && sessionBands.length === 0) return;
    const ts = c.timeScale();
    const w = container.clientWidth;
    const h = container.clientHeight;
    if (w <= 0 || h <= 0) return;
    // Reserve ~24px at the bottom for the time axis and ~6px at the top
    // so labels don't collide with the chart border.
    const lineTop = 4;
    const lineBottom = 24;

    // ─── Session bands (rendered first → behind the marker lines) ─────
    // Each band covers a wall-clock time RANGE (e.g. NYMEX pit hours
    // 09:00–14:30 ET). We clip the rect to the visible chart area so a
    // band that extends past either edge still shows whatever sliver
    // is in view.
    for (const b of sessionBands) {
      const xs = ts.timeToCoordinate(b.start as any);
      const xe = ts.timeToCoordinate(b.end as any);
      // Either coordinate may be null when the boundary is outside the
      // visible range. Clip to the chart's left/right edge in that case
      // so the band still fills the visible portion.
      let left: number | null = xs === null ? null : xs;
      let right: number | null = xe === null ? null : xe;
      if (left === null && right === null) continue;
      if (left === null) left = 0;
      if (right === null) right = w;
      if (right <= 0 || left >= w) continue;
      left = Math.max(0, left);
      right = Math.min(w, right);
      const width = right - left;
      if (width < 1) continue;

      const band = document.createElement('div');
      band.style.cssText = [
        'position:absolute',
        `left:${Math.round(left)}px`,
        `width:${Math.round(width)}px`,
        `top:${lineTop}px`,
        `bottom:${lineBottom}px`,
        `background:${b.color}`,
        // Subtle dotted edges on each side help the band read as a
        // bounded "session" rather than a random gradient stripe.
        'border-left:1px dotted rgba(148, 163, 184, 0.35)',
        'border-right:1px dotted rgba(148, 163, 184, 0.35)',
      ].join(';');
      overlay.appendChild(band);

      // Centred caption — only render if there's room. Below ~80px the
      // band is too narrow for legible text.
      if (width >= 80 && b.label) {
        const cap = document.createElement('div');
        cap.style.cssText = [
          'position:absolute',
          `left:${Math.round(left + width / 2)}px`,
          'top:6px',
          'transform:translateX(-50%)',
          'font-size:10px',
          'font-family:var(--font-mono, ui-monospace, monospace)',
          'letter-spacing:0.06em',
          'text-transform:uppercase',
          'color:rgba(226, 232, 240, 0.85)',
          'background:rgba(11, 17, 32, 0.5)',
          'padding:2px 6px',
          'border-radius:3px',
          'white-space:nowrap',
          'pointer-events:none',
        ].join(';');
        cap.textContent = b.label;
        overlay.appendChild(cap);
      }
    }

    // ─── Session markers (drawn on top of bands) ──────────────────────
    for (const m of sessionMarkers) {
      const x = ts.timeToCoordinate(m.time as any);
      if (x === null || x < 0 || x > w) continue;
      const isOpen = m.kind === 'open';
      const accent = isOpen ? '#10b981' : '#f59e0b';
      const line = document.createElement('div');
      line.style.cssText = [
        'position:absolute',
        `left:${Math.round(x)}px`,
        `top:${lineTop}px`,
        `bottom:${lineBottom}px`,
        'width:0',
        `border-left:1px dashed ${accent}`,
        'opacity:0.55',
      ].join(';');
      overlay.appendChild(line);
      const label = document.createElement('div');
      // Flip the label to the LEFT side of the line if it would overflow
      // the right edge of the chart — keeps it readable on the rightmost
      // markers (the "current break" line during the live session).
      const labelOnLeft = x > w - 80;
      label.style.cssText = [
        'position:absolute',
        labelOnLeft ? `right:${Math.round(w - x + 4)}px` : `left:${Math.round(x + 4)}px`,
        'top:6px',
        'font-size:10px',
        'font-family:var(--font-mono, ui-monospace, monospace)',
        'letter-spacing:0.04em',
        'text-transform:uppercase',
        `color:${accent}`,
        'opacity:0.85',
        'background:rgba(11, 17, 32, 0.55)',
        'padding:2px 5px',
        'border-radius:3px',
        'white-space:nowrap',
      ].join(';');
      label.textContent = m.label;
      overlay.appendChild(label);
    }
  }

  c.timeScale().subscribeVisibleTimeRangeChange(() => repaintOverlay());

  const ro = new ResizeObserver(entries => {
    for (const entry of entries) {
      const { width, height } = entry.contentRect;
      c.applyOptions({ width, height });
    }
    // Repaint the DOM overlay after lightweight-charts recalculates
    // its time-to-pixel mapping. We deliberately do NOT call
    // fitContent() here — preserving the visible time range across a
    // window resize is what the user intuitively expects (the same
    // candles stay in view, just at a different bar-pixel width).
    Promise.resolve().then(repaintOverlay);
  });
  ro.observe(container);

  const toBar = (b: PythCandle) => ({
    time: b.time as any,
    open: b.open,
    high: b.high,
    low: b.low,
    close: b.close,
  });

  let lastBarTime = 0;
  // Track whether we've ever placed real data into this chart instance.
  // Auto-fit fires exactly once — on the first non-empty load — so the
  // user starts with all 24h visible. Every subsequent setData (mode
  // change, periodic refresh) leaves the visible range untouched, so
  // any zoom / pan the user has applied is preserved across polls.
  // shiftVisibleRangeOnNewBar (set in chart options) handles the live
  // ticking case so the rightmost bar always stays in view.
  let hasInitialFit = false;

  return {
    chart: c,
    series,
    setData(bars: PythCandle[]) {
      const filtered = bars
        .filter(b => Number.isFinite(b.close) && b.close > 0)
        .map(toBar);
      series.setData(filtered);
      lastBarTime = filtered.length > 0 ? Number(filtered[filtered.length - 1].time) : 0;
      if (filtered.length > 0 && !hasInitialFit) {
        c.timeScale().fitContent();
        hasInitialFit = true;
      }
      repaintOverlay();
    },
    update(bar: PythCandle) {
      if (!Number.isFinite(bar.close) || bar.close <= 0) return;
      const isNewBucket = bar.time > lastBarTime;
      series.update(toBar(bar));
      if (isNewBucket) {
        lastBarTime = bar.time;
        // shiftVisibleRangeOnNewBar (set in timeScale options) slides
        // the visible window forward by exactly one bar, so the new
        // candle becomes visible without compressing every other bar
        // to fit. The user's zoom level is preserved.
      }
    },
    fitContent() {
      // Public escape hatch for callers that explicitly want a full
      // re-fit (e.g. after a wholesale mode change). Re-arms the
      // initial-fit guard too so a subsequent setData with brand-new
      // data won't try to fit again.
      c.timeScale().fitContent();
      hasInitialFit = true;
    },
    setSessionMarkers(markers: SessionMarker[]) {
      sessionMarkers = markers.slice().sort((a, b) => a.time - b.time);
      repaintOverlay();
    },
    setSessionBands(bands: SessionBand[]) {
      sessionBands = bands.slice().sort((a, b) => a.start - b.start);
      repaintOverlay();
    },
    destroy() {
      ro.disconnect();
      overlay.remove();
      c.remove();
    },
  };
}
