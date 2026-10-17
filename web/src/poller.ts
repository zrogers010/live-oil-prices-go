// Polling scheduler for hero chart updates.
// Design: ONE tick() function handles all fetches. ONE schedule() function manages all timers.

export interface PollerConfig {
  symbol: string;
  fetch: () => Promise<any>;
  onSuccess: (payload: any) => void;
  onModeChange?: (mode: string) => void;
  getMode: () => string | null;
  isHidden: () => boolean;
  clock?: Clock;
  timers?: Timers;
}

export interface Clock {
  now: () => number;
}

export interface Timers {
  setTimeout: (fn: () => void, ms: number) => number;
  clearTimeout: (handle: number) => void;
}

const HERO_LIVE_POLL_MS = 10_000;      // 10s for live Pyth
const HERO_DELAYED_POLL_MS = 60_000;   // 60s for delayed Yahoo
const COLD_BASE_MS = 10_000;           // 10s base for cold first-load failures
const MAX_BACKOFF_MS = 300_000;        // 5 minutes cap

export class HeroPoller {
  private symbol: string;
  private fetchFn: () => Promise<any>;
  private onSuccess: (payload: any) => void;
  private onModeChange?: (mode: string) => void;
  private getMode: () => string | null;
  private isHidden: () => boolean;
  private clock: Clock;
  private timers: Timers;
  
  private pollTimer: number | null = null;
  private inFlight = false;
  private failures = 0;
  private hasSucceeded = false;
  private notBefore = 0;  // Timestamp: don't fetch before this time
  
  constructor(config: PollerConfig) {
    this.symbol = config.symbol;
    this.fetchFn = config.fetch;
    this.onSuccess = config.onSuccess;
    this.onModeChange = config.onModeChange;
    this.getMode = config.getMode;
    this.isHidden = config.isHidden;
    this.clock = config.clock || { now: () => Date.now() };
    // TEMPORARILY BROKEN: bare timers (should fail test)
    this.timers = config.timers || { setTimeout, clearTimeout };
  }
  
  // Start polling by doing an immediate first tick
  start(): void {
    this.tick();
  }
  
  // Pause clears the timer
  pause(): void {
    this.cancelTimer();
  }
  
  // Resume calls tick if not hidden, no timer, and nothing in flight
  // Also respects notBefore deadline (pending Retry-After or backoff)
  resume(): void {
    if (this.isHidden() || this.pollTimer !== null || this.inFlight) {
      return;
    }
    
    const now = this.clock.now();
    if (now < this.notBefore) {
      // There's a pending deadline, schedule to that time instead of fetching now
      this.schedule(this.notBefore - now);
    } else {
      // No pending deadline, fetch immediately
      this.tick();
    }
  }
  
  // tick is the ONLY function that fetches
  private async tick(): Promise<void> {
    // If hidden, clear timer and return (resume will restart)
    if (this.isHidden()) {
      this.cancelTimer();
      return;
    }
    
    // If already fetching, return
    if (this.inFlight) {
      return;
    }
    
    this.inFlight = true;
    let nextDelay: number;
    
    try {
      const payload = await this.fetchFn();
      
      // Classify result: null/404
      if (payload === null) {
        this.failures++;
        nextDelay = this.calculateBackoff();
        // Set notBefore so resume() respects the backoff deadline
        this.notBefore = this.clock.now() + nextDelay;
      } else {
        // Success: reset failures, extract mode
        this.failures = 0;
        this.hasSucceeded = true;
        this.onSuccess(payload);
        
        // Get the current mode and use it for the next delay
        const mode = this.getMode() || 'warming-up';
        nextDelay = this.getBaseInterval(mode);
        
        if (this.onModeChange && payload.mode) {
          this.onModeChange(payload.mode);
        }
      }
    } catch (err: any) {
      // Check for Retry-After on 429/503
      if (err.retryAfter) {
        // Retry-After replaces the normal interval
        // Note: 429 errors still increment failures, raising the backoff level
        this.failures++;
        nextDelay = err.retryAfter * 1000;
      } else {
        // Other errors: escalate backoff
        this.failures++;
        nextDelay = this.calculateBackoff();
      }
      
      // Set notBefore for resume() to respect failure/Retry-After deadlines
      this.notBefore = this.clock.now() + nextDelay;
      
      console.error(`Hero chart poll failed for ${this.symbol}:`, err);
    } finally {
      this.inFlight = false;
    }
    
    // Schedule next tick
    this.schedule(nextDelay);
  }
  
  private calculateBackoff(): number {
    if (!this.hasSucceeded) {
      // Cold start: use shorter base
      const backoff = COLD_BASE_MS * Math.pow(2, this.failures - 1);
      return Math.min(backoff, MAX_BACKOFF_MS);
    } else {
      // After success: use mode-based base
      const mode = this.getMode() || 'warming-up';
      const base = this.getBaseInterval(mode);
      const backoff = base * Math.pow(2, this.failures);
      return Math.min(backoff, MAX_BACKOFF_MS);
    }
  }
  
  private getBaseInterval(mode: string): number {
    if (mode === 'live') {
      return HERO_LIVE_POLL_MS;
    }
    // All other modes: today-paused, warming-up, delayed, prior-session
    return HERO_DELAYED_POLL_MS;
  }
  
  // schedule ALWAYS clears the existing timer before setting a new one
  private schedule(delayMs: number): void {
    this.cancelTimer();
    
    this.pollTimer = this.timers.setTimeout(() => {
      // Clear our own handle first
      this.pollTimer = null;
      // Then tick
      this.tick();
    }, delayMs);
  }
  
  private cancelTimer(): void {
    if (this.pollTimer !== null) {
      this.timers.clearTimeout(this.pollTimer);
      this.pollTimer = null;
    }
  }
  
  // For testing: expose internal state
  getState() {
    return {
      hasTimer: this.pollTimer !== null,
      inFlight: this.inFlight,
      failures: this.failures,
      hasSucceeded: this.hasSucceeded,
      notBefore: this.notBefore
    };
  }
}
