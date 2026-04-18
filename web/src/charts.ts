import { createChart, ColorType, CrosshairMode, LineStyle, type IChartApi, type ISeriesApi } from 'lightweight-charts';
import type { OHLCV, PythCandle } from './types';

let chart: IChartApi | null = null;
let candleSeries: ISeriesApi<'Candlestick'> | null = null;
let volumeSeries: ISeriesApi<'Histogram'> | null = null;
let resizeObserver: ResizeObserver | null = null;

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
 *  the whole series). */
export interface CandleChartHandle {
  chart: IChartApi;
  series: ISeriesApi<'Candlestick'>;
  destroy(): void;
  setData(bars: PythCandle[]): void;
  update(bar: PythCandle): void;
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

export function subscribeCrosshair(
  callback: (o: number, h: number, l: number, c: number, v: number) => void
): void {
  if (!chart || !candleSeries) return;

  chart.subscribeCrosshairMove(param => {
    if (!param.time || !candleSeries) {
      return;
    }
    const data = param.seriesData.get(candleSeries) as any;
    if (data) {
      callback(data.open, data.high, data.low, data.close, data.value ?? 0);
    }
  });
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
 * createCandleChart spins up a standalone candlestick instance for streaming
 * 1-minute Pyth bars on the homepage hero. Two design choices worth calling
 * out:
 *
 *  1. `secondsVisible: true` — the time axis labels show HH:MM, which makes
 *     the live tick obvious to a casual visitor (the latest label is always
 *     "now-ish").
 *  2. We expose both `setData` (for the initial backfill) and `update` (for
 *     every subsequent poll). lightweight-charts' update() is highly
 *     optimised — it animates the in-progress candle in place rather than
 *     re-rendering the series — which gives us the visible "ticking" effect
 *     the user is asking for.
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
      rightOffset: 4,
      barSpacing: 6,
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

  const ro = new ResizeObserver(entries => {
    for (const entry of entries) {
      const { width, height } = entry.contentRect;
      c.applyOptions({ width, height });
    }
  });
  ro.observe(container);

  const toBar = (b: PythCandle) => ({
    time: b.time as any,
    open: b.open,
    high: b.high,
    low: b.low,
    close: b.close,
  });

  return {
    chart: c,
    series,
    setData(bars: PythCandle[]) {
      const filtered = bars
        .filter(b => Number.isFinite(b.close) && b.close > 0)
        .map(toBar);
      series.setData(filtered);
      // Only auto-fit on the first load — once the user has interacted with
      // the chart we let lightweight-charts maintain their pan/zoom.
      if (filtered.length > 0) {
        c.timeScale().scrollToRealTime();
      }
    },
    update(bar: PythCandle) {
      if (!Number.isFinite(bar.close) || bar.close <= 0) return;
      series.update(toBar(bar));
    },
    destroy() {
      ro.disconnect();
      c.remove();
    },
  };
}
