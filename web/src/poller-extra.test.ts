// Additional comprehensive tests for HeroPoller
import { HeroPoller, Clock, Timers, PollerConfig } from './poller';

// Reuse test infrastructure from poller.test.ts
class FakeClock implements Clock {
  private time = 0;
  now(): number { return this.time; }
  advance(ms: number): void { this.time += ms; }
  setTime(t: number): void { this.time = t; }
}

class FakeTimers implements Timers {
  private nextId = 1;
  private timers = new Map<number, { fn: () => void | Promise<void>; fireAt: number }>();
  
  constructor(private clock: FakeClock) {}
  
  setTimeout(fn: () => void | Promise<void>, ms: number): number {
    const id = this.nextId++;
    this.timers.set(id, { fn, fireAt: this.clock.now() + ms });
    return id;
  }
  
  clearTimeout(handle: number): void {
    this.timers.delete(handle);
  }
  
  async tick(ms: number): Promise<void> {
    const targetTime = this.clock.now() + ms;
    while (true) {
      const ready = Array.from(this.timers.entries())
        .filter(([_, t]) => t.fireAt <= targetTime)
        .sort((a, b) => a[1].fireAt - b[1].fireAt);
      if (ready.length === 0) break;
      for (const [id, timer] of ready) {
        this.clock.setTime(timer.fireAt);
        this.timers.delete(id);
        const result = timer.fn();
        if (result instanceof Promise) await result;
      }
    }
    this.clock.setTime(targetTime);
  }
  
  hasTimer(): boolean { return this.timers.size > 0; }
  timerCount(): number { return this.timers.size; }
}

interface TestContext {
  clock: FakeClock;
  timers: FakeTimers;
  fetches: any[];
  payloads: any[];
  successes: any[];
  mode: string | null;
  hidden: boolean;
}

function createContext(): TestContext {
  const clock = new FakeClock();
  const timers = new FakeTimers(clock);
  return { clock, timers, fetches: [], payloads: [], successes: [], mode: null, hidden: false };
}

function createPoller(ctx: TestContext, symbol = 'WTI'): HeroPoller {
  return new HeroPoller({
    symbol,
    fetch: async () => {
      ctx.fetches.push({ t: ctx.clock.now() });
      if (ctx.payloads.length === 0) throw new Error('No more payloads');
      const payload = ctx.payloads.shift();
      if (payload === null) return null;
      if (payload.error) {
        const err: any = new Error(payload.error);
        if (payload.retryAfter) err.retryAfter = payload.retryAfter;
        throw err;
      }
      return payload;
    },
    onSuccess: (payload) => {
      ctx.successes.push({ t: ctx.clock.now(), payload });
      if (payload.mode) ctx.mode = payload.mode;
    },
    getMode: () => ctx.mode,
    isHidden: () => ctx.hidden,
    clock: ctx.clock,
    timers: ctx.timers
  });
}

function gaps(fetches: any[]): number[] {
  return fetches.slice(1).map((f, i) => f.t - fetches[i].t);
}

const tests: Array<{ name: string; fn: () => void | Promise<void> }> = [];
function test(name: string, fn: () => void | Promise<void>) { tests.push({ name, fn }); }
function assert(condition: boolean, message: string): void {
  if (!condition) throw new Error(`Assertion failed: ${message}`);
}
function assertEquals(actual: any, expected: any, message?: string): void {
  const actualStr = JSON.stringify(actual);
  const expectedStr = JSON.stringify(expected);
  if (actualStr !== expectedStr) {
    throw new Error(`Assertion failed${message ? ': ' + message : ''}\nExpected: ${expectedStr}\nActual: ${actualStr}`);
  }
}

// Test: Retry-After 10 then 120
test('Retry-After 10 then 120', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { error: '429', retryAfter: 10 },
    { error: '429', retryAfter: 120 },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  await ctx.timers.tick(10000); // Normal poll
  await ctx.timers.tick(10000); // RA 10
  await ctx.timers.tick(120000); // RA 120
  
  const g = gaps(ctx.fetches);
  // Note: the clock doesn't advance during awaits, so times are at scheduled times
  assertEquals(g.length, 3, 'should have 3 gaps');
  // First gap: 10s from start to normal poll
  // Second gap: 10s from RA
  // Third gap: 120s from RA
  assertEquals(g, [10000, 10000, 120000], 'gaps should be 10, 10, 120');
});

// Test: backoff reset on success
test('backoff reset on success', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { error: '500' },
    { error: '500' },
    { mode: 'live', bars: [] },
    { error: '500' },
    { error: '500' },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  // Start -> error -> error -> success (gaps: 10, 20, 40)
  await ctx.timers.tick(10000);
  await ctx.timers.tick(20000);
  await ctx.timers.tick(40000);
  
  // Success resets failures
  const state1 = poller.getState();
  assertEquals(state1.failures, 0, 'failures reset after success');
  
  // Next error should use base interval from live mode (10s), not continue from 80s
  await ctx.timers.tick(10000);
  // Then backoff should be base * 2^1 = 20s
  await ctx.timers.tick(20000);
  // Then another success
  await ctx.timers.tick(40000);
  // Then back to normal 10s
  await ctx.timers.tick(10000);
  
  const g = gaps(ctx.fetches);
  assertEquals(g.slice(0, 7), [10000, 20000, 40000, 10000, 20000, 40000, 10000], 'backoff resets after success');
});

// Test: 5-minute cap
test('5-minute cap on backoff', async () => {
  const ctx = createContext();
  ctx.payloads = Array(10).fill(null).concat([{ mode: 'live', bars: [] }]);
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  // Cold start: 10, 20, 40, 80, 160, 300 (capped), 300...
  const expected = [10000, 20000, 40000, 80000, 160000, 300000, 300000, 300000, 300000, 300000];
  for (const delay of expected) {
    await ctx.timers.tick(delay);
  }
  
  const g = gaps(ctx.fetches);
  assertEquals(g, expected, 'backoff caps at 300s');
});

// Test: only one pending timer (cancel-before-set)
test('only one pending timer at any time', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  assertEquals(ctx.timers.timerCount(), 1, 'exactly one timer after start');
  
  await ctx.timers.tick(10000);
  assertEquals(ctx.timers.timerCount(), 1, 'still one timer after tick');
  
  await ctx.timers.tick(10000);
  assertEquals(ctx.timers.timerCount(), 1, 'still one timer');
});

// Test: resume guards - in flight
test('resume respects in-flight guard', async () => {
  const ctx = createContext();
  let resolveFirst: any;
  let fetchCount = 0;
  
  const config: PollerConfig = {
    symbol: 'WTI',
    fetch: async () => {
      fetchCount++;
      ctx.fetches.push({ t: ctx.clock.now() });
      if (fetchCount === 1) {
        await new Promise(r => { resolveFirst = r; });
      }
      return { mode: 'live', bars: [] };
    },
    onSuccess: (payload) => {
      if (payload.mode) ctx.mode = payload.mode;
    },
    getMode: () => ctx.mode,
    isHidden: () => ctx.hidden,
    clock: ctx.clock,
    timers: ctx.timers
  };
  
  const poller = new HeroPoller(config);
  poller.start();
  
  // First fetch is in flight
  await new Promise(r => setTimeout(r, 0));
  assertEquals(fetchCount, 1);
  
  // Try to resume - should be ignored
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  assertEquals(fetchCount, 1, 'resume ignored while in flight');
  
  // Complete first fetch
  resolveFirst({ mode: 'live', bars: [] });
  await new Promise(r => setTimeout(r, 10));
});

// Test: resume guards - existing timer
test('resume respects existing timer', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  assert(ctx.timers.hasTimer(), 'timer exists');
  
  // Try to resume - should be ignored
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  assertEquals(ctx.fetches.length, 1, 'resume ignored when timer exists');
});

// Test: resume guards - pending notBefore
test('resume respects notBefore deadline', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { error: '429', retryAfter: 30 },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  await ctx.timers.tick(10000);
  
  // Now in Retry-After wait. Hide and show immediately
  ctx.hidden = true;
  poller.pause();
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  // Should NOT have fetched immediately
  assertEquals(ctx.fetches.length, 2, 'no immediate fetch on resume during RA');
  
  // Should have scheduled to complete the RA wait
  assert(ctx.timers.hasTimer(), 'timer scheduled');
  
  // Advance rest of RA
  await ctx.timers.tick(30000);
  assertEquals(ctx.fetches.length, 3, 'fetch after RA completes');
});

// Test: tick's in-flight guard
test('tick respects in-flight guard', async () => {
  const ctx = createContext();
  let resolveFirst: any;
  let fetchCount = 0;
  
  const config: PollerConfig = {
    symbol: 'WTI',
    fetch: async () => {
      fetchCount++;
      ctx.fetches.push({ t: ctx.clock.now() });
      if (fetchCount === 1) {
        await new Promise(r => { resolveFirst = r; });
      }
      return { mode: 'live', bars: [] };
    },
    onSuccess: (payload) => {
      if (payload.mode) ctx.mode = payload.mode;
    },
    getMode: () => ctx.mode,
    isHidden: () => ctx.hidden,
    clock: ctx.clock,
    timers: ctx.timers
  };
  
  const poller = new HeroPoller(config);
  poller.start();
  
  // First fetch in flight
  await new Promise(r => setTimeout(r, 0));
  
  // Try to call start again (which calls tick)
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  assertEquals(fetchCount, 1, 'second tick call ignored while in flight');
  
  resolveFirst({ mode: 'live', bars: [] });
  await new Promise(r => setTimeout(r, 10));
});

// Test: default timers with bound context
test('default timers work without injection', async () => {
  // Create a setTimeout that throws if `this` is wrong
  const originalSetTimeout = globalThis.setTimeout;
  const originalClearTimeout = globalThis.clearTimeout;
  let setCalled = false;
  let clearCalled = false;
  
  try {
    // Override globalThis.setTimeout to track calls
    globalThis.setTimeout = function(this: any, fn: any, ms: number) {
      setCalled = true;
      return originalSetTimeout.call(this, fn, ms);
    } as any;
    
    globalThis.clearTimeout = function(this: any, handle: number) {
      clearCalled = true;
      return originalClearTimeout.call(this, handle);
    } as any;
    
    let fetchCalled = false;
    const poller = new HeroPoller({
      symbol: 'TEST',
      fetch: async () => {
        fetchCalled = true;
        return { mode: 'live', bars: [] };
      },
      onSuccess: () => {},
      getMode: () => 'live',
      isHidden: () => false
      // No timers injected - should use defaults
    });
    
    poller.start();
    await new Promise(r => setTimeout(r, 10));
    
    assert(fetchCalled, 'fetch was called');
    assert(setCalled, 'setTimeout was called');
    
    poller.pause();
    assert(clearCalled, 'clearTimeout was called');
  } finally {
    globalThis.setTimeout = originalSetTimeout;
    globalThis.clearTimeout = originalClearTimeout;
  }
});

// Run all tests
async function runTests() {
  let passed = 0;
  let failed = 0;
  
  for (const { name, fn } of tests) {
    try {
      await fn();
      console.log(`✓ ${name}`);
      passed++;
    } catch (err) {
      console.error(`✗ ${name}`);
      console.error(`  ${err}`);
      failed++;
    }
  }
  
  console.log(`\n${passed} passed, ${failed} failed`);
  if (typeof global !== 'undefined' && global.process && global.process.exit) {
    global.process.exit(failed > 0 ? 1 : 0);
  }
}

runTests();
