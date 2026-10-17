// Mutation tests for HeroPoller
import { HeroPoller, Clock, Timers, PollerConfig } from './poller';

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

// Mutation: cancel-before-set (only one pending timer)
test('schedule cancels existing timer before setting new one', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  // Should have exactly 1 timer
  assertEquals(ctx.timers.timerCount(), 1, 'exactly one timer after start');
  
  // Advance and check again
  await ctx.timers.tick(10000);
  assertEquals(ctx.timers.timerCount(), 1, 'still exactly one timer after tick');
  
  // Advance again
  await ctx.timers.tick(10000);
  assertEquals(ctx.timers.timerCount(), 1, 'still exactly one timer');
  
  // Should never have more than 1 timer
  assert(ctx.fetches.length === 3, 'three fetches occurred');
});

// Mutation: timer callback clears its own handle
test('timer callback nulls pollTimer handle', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  const state1 = poller.getState();
  assert(state1.hasTimer, 'timer exists after first fetch');
  
  // Advance time - timer should fire and set a new one
  await ctx.timers.tick(10000);
  
  const state2 = poller.getState();
  assert(state2.hasTimer, 'new timer exists after second fetch');
});

// Mutation: resume does not fetch when already in flight
test('resume does not fetch when already in flight', async () => {
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
  assertEquals(fetchCount, 1, 'first fetch started');
  
  const state1 = poller.getState();
  assert(state1.inFlight, 'in flight');
  
  // Try to resume while in flight - should be blocked
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  assertEquals(fetchCount, 1, 'resume blocked while in flight');
  
  // Complete first fetch
  resolveFirst({ mode: 'live', bars: [] });
  await new Promise(r => setTimeout(r, 10));
  
  // Now timer should be set for next poll
  const state2 = poller.getState();
  assert(state2.hasTimer, 'timer set after fetch completes');
  assert(!state2.inFlight, 'no longer in flight');
});

// Mutation: resume does not fetch when timer already exists
test('resume does not fetch when timer already exists', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  const state1 = poller.getState();
  assert(state1.hasTimer, 'timer exists after first fetch');
  assertEquals(ctx.fetches.length, 1, 'one fetch so far');
  
  // Try to resume when timer exists - should be blocked
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  const state2 = poller.getState();
  assert(state2.hasTimer, 'timer still exists');
  assertEquals(ctx.fetches.length, 1, 'resume blocked, no second fetch');
});

// Mutation: resume does not reset failure count
test('resume does not reset failure count', async () => {
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
  
  // Two failures
  await ctx.timers.tick(10000);
  await ctx.timers.tick(20000);
  
  const state1 = poller.getState();
  assertEquals(state1.failures, 2, 'failures = 2 after two errors');
  
  // Hide and resume - failure count should persist
  ctx.hidden = true;
  poller.pause();
  await new Promise(r => setTimeout(r, 0));
  
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  const state2 = poller.getState();
  assertEquals(state2.failures, 2, 'failures still = 2 after resume (not reset)');
  
  // Next delay should still be at backoff level 2 (40s), not reset to 10s
  // We're at notBefore deadline, so resume should schedule to that time
  assert(state2.hasTimer || state2.inFlight, 'timer or fetch in progress');
});

// Test: tab coming back after short hide should fetch immediately (no failure pending)
test('resume after short hide with no pending failure fetches immediately', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  // Wait for first fetch
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2, 'two fetches');
  
  // Hide for 2 seconds (shorter than the 10s interval)
  ctx.hidden = true;
  poller.pause();
  ctx.clock.advance(2000);
  
  // Resume - should fetch immediately since no failure pending
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  assertEquals(ctx.fetches.length, 3, 'immediate fetch on resume');
  assertEquals(ctx.fetches[2].t - ctx.fetches[1].t, 2000, 'fetched immediately (2s gap, not 10s)');
});

// Test: mode flip while hidden should fetch immediately on show (no failure pending)
test('resume after mode change while hidden fetches immediately', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    { mode: 'live', bars: [] },
    { mode: 'today-paused', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  // First fetch
  assertEquals(ctx.mode, 'live', 'mode is live');
  
  // Hide
  ctx.hidden = true;
  poller.pause();
  
  // Change mode while hidden
  ctx.mode = 'today-paused';
  ctx.clock.advance(5000);
  
  // Resume - should fetch immediately to pick up new mode
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  assertEquals(ctx.fetches.length, 2, 'immediate fetch on resume after mode change');
  assertEquals(ctx.fetches[1].t - ctx.fetches[0].t, 5000, 'fetched immediately (5s gap)');
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
