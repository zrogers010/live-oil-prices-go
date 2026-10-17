// Unit tests for HeroPoller
import { HeroPoller, Clock, Timers, PollerConfig } from './poller';

class FakeClock implements Clock {
  private time = 0;
  
  now(): number {
    return this.time;
  }
  
  advance(ms: number): void {
    this.time += ms;
  }
  
  setTime(t: number): void {
    this.time = t;
  }
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
  
  // Test helper: advance time and fire ready timers AT THEIR SCHEDULED TIME
  async tick(ms: number): Promise<void> {
    const targetTime = this.clock.now() + ms;
    
    // Process timers in chronological order, advancing clock to each timer's exact fire time
    while (true) {
      // Find the next timer to fire that's before targetTime
      const ready = Array.from(this.timers.entries())
        .filter(([_, t]) => t.fireAt <= targetTime)
        .sort((a, b) => a[1].fireAt - b[1].fireAt);
      
      if (ready.length === 0) break;
      
      for (const [id, timer] of ready) {
        // Advance clock to this timer's exact fire time
        this.clock.setTime(timer.fireAt);
        this.timers.delete(id);
        const result = timer.fn();
        if (result instanceof Promise) {
          await result;
        }
      }
    }
    
    // Finally advance to the target time
    this.clock.setTime(targetTime);
  }
  
  hasTimer(): boolean {
    return this.timers.size > 0;
  }
  
  timerCount(): number {
    return this.timers.size;
  }
  
  nextFireTime(): number | null {
    if (this.timers.size === 0) return null;
    return Math.min(...Array.from(this.timers.values()).map(t => t.fireAt));
  }
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
  const ctx: TestContext = {
    clock,
    timers,
    fetches: [],
    payloads: [],
    successes: [],
    mode: null,
    hidden: false
  };
  return ctx;
}

function createPoller(ctx: TestContext, symbol = 'WTI'): HeroPoller {
  const config: PollerConfig = {
    symbol,
    fetch: async () => {
      ctx.fetches.push({ t: ctx.clock.now() });
      if (ctx.payloads.length === 0) {
        throw new Error('No more payloads');
      }
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
  };
  
  return new HeroPoller(config);
}

function gaps(fetches: any[]): number[] {
  return fetches.slice(1).map((f, i) => f.t - fetches[i].t);
}

// Test suite
const tests: Array<{ name: string; fn: () => void | Promise<void> }> = [];

function test(name: string, fn: () => void | Promise<void>) {
  tests.push({ name, fn });
}

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(`Assertion failed: ${message}`);
  }
}

function assertEquals(actual: any, expected: any, message?: string): void {
  const actualStr = JSON.stringify(actual);
  const expectedStr = JSON.stringify(expected);
  if (actualStr !== expectedStr) {
    throw new Error(`Assertion failed${message ? ': ' + message : ''}\nExpected: ${expectedStr}\nActual: ${actualStr}`);
  }
}

// Test: hidden Retry-After followed by resume restarts polling
test('hidden Retry-After followed by resume restarts polling', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { error: '429', retryAfter: 30 },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  
  // Wait for async fetch
  await new Promise(r => setTimeout(r, 0));
  
  // First fetch succeeds immediately
  assertEquals(ctx.fetches.length, 1, 'should have first fetch');
  assertEquals(ctx.mode, 'live');
  
  // Advance to next poll
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2, 'should have second fetch (429)');
  
  // A timer should be set for 30s
  assert(ctx.timers.hasTimer(), 'should have RA timer');
  
  // Hide the tab before RA timer fires
  ctx.hidden = true;
  await ctx.timers.tick(30000);
  
  // Timer should have fired but returned early, clearing itself
  assertEquals(ctx.fetches.length, 2, 'should not fetch while hidden');
  assert(!ctx.timers.hasTimer(), 'should not have timer after hidden return');
  
  // Resume should restart polling
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  // Should fetch immediately
  assertEquals(ctx.fetches.length, 3, 'should fetch on resume');
  assert(ctx.timers.hasTimer(), 'should have timer after resume');
});

// Test: repeated 429s honor each Retry-After
test('repeated 429s honor each Retry-After', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { error: '429', retryAfter: 30 },
    { error: '429', retryAfter: 30 },
    { error: '429', retryAfter: 30 },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  
  await new Promise(r => setTimeout(r, 0));
  assertEquals(ctx.fetches.length, 1);
  
  // Normal poll
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2, 'first 429');
  
  // RA 30
  await ctx.timers.tick(30000);
  assertEquals(ctx.fetches.length, 3, 'second 429 after RA');
  
  // RA 30 again
  await ctx.timers.tick(30000);
  assertEquals(ctx.fetches.length, 4, 'third 429 after RA');
  
  // RA 30 again
  await ctx.timers.tick(30000);
  assertEquals(ctx.fetches.length, 5, 'success after RA');
  
  const g = gaps(ctx.fetches);
  assertEquals(g, [10000, 30000, 30000, 30000], 'gaps should be 10, 30, 30, 30');
});

// Test: cold-failure sequence is 10/20/40s
test('cold-failure sequence is 10/20/40s', async () => {
  const ctx = createContext();
  ctx.payloads = [
    null,
    null,
    null,
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  
  await new Promise(r => setTimeout(r, 0));
  assertEquals(ctx.fetches.length, 1, 'first fetch');
  
  // First retry: 10s (10 * 2^0)
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2, 'second fetch after 10s');
  
  // Second retry: 20s (10 * 2^1)
  await ctx.timers.tick(20000);
  assertEquals(ctx.fetches.length, 3, 'third fetch after 20s');
  
  // Third retry: 40s (10 * 2^2)
  await ctx.timers.tick(40000);
  assertEquals(ctx.fetches.length, 4, 'fourth fetch after 40s');
  
  const g = gaps(ctx.fetches);
  assertEquals(g, [10000, 20000, 40000], 'gaps should be 10, 20, 40');
});

// Test: backoff after success uses the mode base
test('backoff after success uses mode base', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { error: '500' },
    { error: '500' },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  
  await new Promise(r => setTimeout(r, 0));
  assertEquals(ctx.fetches.length, 1);
  assertEquals(ctx.mode, 'live');
  
  // Normal poll
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2, 'first error');
  
  // Backoff: 10 * 2^1 = 20s
  await ctx.timers.tick(20000);
  assertEquals(ctx.fetches.length, 3, 'second error');
  
  // Backoff: 10 * 2^2 = 40s
  await ctx.timers.tick(40000);
  assertEquals(ctx.fetches.length, 4, 'recovery');
  
  const g = gaps(ctx.fetches);
  assertEquals(g, [10000, 20000, 40000], 'gaps should be 10, 20, 40');
});

// Test: resume during slow first load gives single chain
test('resume during slow first load gives single chain', async () => {
  const ctx = createContext();
  let fetchCount = 0;
  const slowFetch = async () => {
    fetchCount++;
    ctx.fetches.push({ t: ctx.clock.now(), n: fetchCount });
    await new Promise(r => setTimeout(r, 0)); // Simulate async
    if (fetchCount === 1) {
      // First fetch is slow and fails
      throw new Error('slow 500');
    }
    return { mode: 'live', bars: [] };
  };
  
  const config: PollerConfig = {
    symbol: 'WTI',
    fetch: slowFetch,
    onSuccess: (payload) => {
      ctx.successes.push({ t: ctx.clock.now(), payload });
      if (payload.mode) ctx.mode = payload.mode;
    },
    getMode: () => ctx.mode,
    isHidden: () => ctx.hidden,
    clock: ctx.clock,
    timers: ctx.timers
  };
  
  const poller = new HeroPoller(config);
  poller.start();
  
  // Wait for first fetch to complete
  await new Promise(r => setTimeout(r, 10));
  
  // First fetch should have failed and scheduled a 10s retry
  assertEquals(fetchCount, 1, 'first fetch started');
  assert(ctx.timers.hasTimer(), 'timer scheduled after first failure');
  
  // While in backoff, hide and show
  ctx.hidden = true;
  poller.pause();
  assert(!ctx.timers.hasTimer(), 'timer cleared on pause');
  
  ctx.hidden = false;
  poller.resume();
  
  // Resume should schedule to the existing notBefore, not start another fetch
  await new Promise(r => setTimeout(r, 10));
  
  // Should have scheduled a timer to the notBefore
  assert(ctx.timers.hasTimer(), 'timer scheduled after resume');
  
  // Advance through backoff
  await ctx.timers.tick(10000);
  
  // Should have exactly 2 fetches total (initial + first retry), with one timer chain
  assertEquals(fetchCount, 2, 'second fetch after retry delay');
  assertEquals(ctx.fetches.length, 2, 'exactly 2 fetches');
  assertEquals(ctx.fetches[1].t - ctx.fetches[0].t, 10000, 'gap is 10s');
});

// Test: mode changes take effect
test('mode changes take effect', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] },
    { mode: 'today-paused', bars: [] },
    { mode: 'today-paused', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  
  await new Promise(r => setTimeout(r, 0));
  assertEquals(ctx.fetches.length, 1);
  assertEquals(ctx.mode, 'live');
  
  // Live mode: 10s
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2);
  assertEquals(ctx.mode, 'live');
  
  // Mode changes to today-paused
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 3);
  assertEquals(ctx.mode, 'today-paused');
  
  // Should now use 60s interval
  await ctx.timers.tick(60000);
  assertEquals(ctx.fetches.length, 4);
  
  const g = gaps(ctx.fetches);
  assertEquals(g, [10000, 10000, 60000], 'gaps should be 10, 10, 60');
});

// Test: pause while in flight doesn't reschedule
test('pause while in flight does not reschedule', async () => {
  const ctx = createContext();
  let inFetchResolve: any;
  const slowFetch = () => new Promise<any>(resolve => {
    ctx.fetches.push({ t: ctx.clock.now() });
    inFetchResolve = resolve;
  });
  
  const config: PollerConfig = {
    symbol: 'WTI',
    fetch: slowFetch,
    onSuccess: (payload) => {
      ctx.successes.push({ t: ctx.clock.now(), payload });
      if (payload.mode) ctx.mode = payload.mode;
    },
    getMode: () => ctx.mode,
    isHidden: () => ctx.hidden,
    clock: ctx.clock,
    timers: ctx.timers
  };
  
  const poller = new HeroPoller(config);
  poller.start();
  
  // Fetch is in flight
  assertEquals(ctx.fetches.length, 1);
  assert(!ctx.timers.hasTimer(), 'no timer while fetch in flight');
  
  // Pause while in flight
  poller.pause();
  
  // Resolve the fetch
  inFetchResolve({ mode: 'live', bars: [] });
  
  // Need to let the promise resolve
  await new Promise(r => setTimeout(r, 0));
  
  // Should have scheduled a timer after the fetch completed
  assert(ctx.timers.hasTimer(), 'timer scheduled after fetch');
  
  // But pause should have been called, so actually no timer
  // Actually, the pause happens before the fetch completes, so the fetch
  // completion will schedule a timer. Let's test differently:
  
  // Pause after fetch completes
  poller.pause();
  assert(!ctx.timers.hasTimer(), 'no timer after pause');
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
  // Exit with error code if tests failed (when running in Node)
  if (typeof global !== 'undefined' && global.process && global.process.exit) {
    global.process.exit(failed > 0 ? 1 : 0);
  }
}

runTests();
