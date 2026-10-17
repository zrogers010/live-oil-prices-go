// Test: tab shown during 404 backoff waits out remaining delay
import { HeroPoller, Clock, Timers, PollerConfig } from './poller';

class FakeClock implements Clock {
  private time = 0;
  now(): number { return this.time; }
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

function createPoller(ctx: TestContext): HeroPoller {
  return new HeroPoller({
    symbol: 'WTI',
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

// Test: resume during 404 backoff respects notBefore deadline
test('tab shown during 404 backoff waits out remaining delay', async () => {
  const ctx = createContext();
  ctx.payloads = [
    { mode: 'live', bars: [] },
    null,  // 404
    { mode: 'live', bars: [] }
  ];
  
  const poller = createPoller(ctx);
  poller.start();
  await new Promise(r => setTimeout(r, 0));
  
  // First fetch successful
  assertEquals(ctx.fetches.length, 1);
  
  // Advance to 404
  await ctx.timers.tick(10000);
  assertEquals(ctx.fetches.length, 2, 'second fetch (404)');
  
  // Now in backoff (20s). Hide for 5s, then show
  ctx.hidden = true;
  poller.pause();
  ctx.clock.setTime(ctx.clock.now() + 5000);
  
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  // Should NOT fetch immediately - should wait remaining 15s
  assertEquals(ctx.fetches.length, 2, 'no immediate fetch during backoff');
  
  // Verify timer is set for remaining delay
  assert(ctx.timers.hasTimer(), 'timer scheduled for remaining backoff');
  
  // Advance rest of backoff
  await ctx.timers.tick(20000);
  assertEquals(ctx.fetches.length, 3, 'fetch after backoff completes');
});

// Test: resume during 500 backoff respects notBefore deadline
test('tab shown during 500 backoff waits out remaining delay', async () => {
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
  
  // Normal, error (backoff 20s)
  await ctx.timers.tick(10000);
  await ctx.timers.tick(20000);
  
  assertEquals(ctx.fetches.length, 3, 'three fetches so far');
  
  // Now in 40s backoff. Hide for 10s, then show
  ctx.hidden = true;
  poller.pause();
  ctx.clock.setTime(ctx.clock.now() + 10000);
  
  ctx.hidden = false;
  poller.resume();
  await new Promise(r => setTimeout(r, 0));
  
  // Should NOT fetch immediately - should wait remaining 30s
  assertEquals(ctx.fetches.length, 3, 'no immediate fetch during backoff');
  
  // Advance rest of backoff
  await ctx.timers.tick(40000);
  assertEquals(ctx.fetches.length, 4, 'fetch after backoff completes');
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
