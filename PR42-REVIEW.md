# PR42 review: v1 (head dd7b905) is the first section, v2 (head 9965e65) is appended below

# PR #42 review: zrogers010/live-oil-prices-go
Head dd7b9056 / base efb177f2. Read-only review: nothing pushed, merged, approved, commented on, or deployed.

## Verdict: FAIL

Blockers:
1. `npm run test:types` (tsc --noEmit) FAILS on head: `web/src/api.ts(1,58): error TS2305: Module '"./types"' has no exported member 'MarketAnalysis'`. The type was removed from types.ts but is still imported. Because `make test` = test-go + test-ts, `make test` fails. esbuild still builds because it strips type-only imports. The PR's "Frontend builds" claim is true only for esbuild.
2. A new timer-stacking bug in `resumePolling` (head web/src/app.ts:484-497). The `pollTimer != null` guard runs before an `await`, and `startPolling` (app.ts:464) overwrites `pollTimer` without clearing it. Two things follow:
   - A hidden→visible→hidden→visible toggle, or any mode change during the resume fetch (applyPayload→transitionPolling→startPolling, then line 496 calls startPolling again), leaks an interval. Measured: 12/min per symbol instead of 6/min.
   - The leaked interval cannot be cleared by pausePolling, so it **keeps polling while hidden**. Measured: 11/min per symbol while hidden.
   - resumePolling also never re-checks `document.hidden` after its await, so going hidden during the resume fetch still starts a timer.
3. There is no error backoff (goal C), and the PR body's "existing error handling continues to work" means a fixed cadence on errors.
4. Goal 3 (LIVE badge) has no code change in the diff. The PR body says "JavaScript updates the text and visibility". The diff has no badge changes and nothing sets the pill's display. The existing behavior is safe (it never shows a false LIVE), but the claim is inaccurate.

## Provenance / hash verification
- The connector read the repo, which is public (`private:false`). I got full trees from GitHub codeload tarballs at the exact SHAs. Every blob was then hash-verified with `git hash-object` against connector `get_git_tree(recursive)` for both SHAs: base 71/71 match, head 71/71 match, not truncated.
- All 12 PR files (list_pull_request_files `sha`) match the head blobs. `diff -rq base head` shows exactly those 12 files (+74/-185).
- Note: /workspace/lop-review/ root already held an older loose checkout from someone else (README.md, cmd/, internal/ …). I left it alone. My trees are in base/ and head/.

## A. Root cause (BASE)
- There is one timer per controller. `transitionPolling` clears before it creates (base app.ts:416-420) and is called only on a mode change (app.ts:346-349, 354-363), not per tick or render. `initHeroControllers` dedupes by symbol (app.ts:60). `app.js` is included once (layout.html:68). Hero cards exist only on home.html:9,11. No SSR/hydration framework is involved: SSR is Go templates plus one DOMContentLoaded bootstrap (app.ts:43-50).
- The real cause is the interval choice. Every mode except prior-session uses 2s (app.ts:20, 424). Prod uses Yahoo with no Pyth, so `GetHeroChart` returns mode=today-paused, delayed=true (services/market_data.go:737, 773-777), which polls every 2s. Base never pauses on hidden tabs (no visibilitychange anywhere).
- Per client: 30 req/min per symbol per open home tab (60/min total for 2 symbols), visible or hidden. Measured below: exactly 2.0s gaps. Async callbacks can overlap on slow responses, but that never multiplies timers.
- So "timers stacked" is NOT supported by the base code. One tab gives 30/min. About 430/min per symbol works out to ~14-15 concurrently open home tabs, or a bot/scraper. One normal client cannot explain it. Background tabs that never pause make it easy for a modest number of visitors to reach that.

## B. HEAD timer correctness
- Good: transitionPolling clears first (app.ts:435-443), skips start when hidden (441), and pausePolling clears (475-480). The visibility listener is attached once (53, 72-82).
- Bad: the resumePolling race described above (484-497). There is no in-flight/re-entry guard in the interval callback (464-471). There is no unmount concept (static page), which is acceptable.
- Interval map: live 10s, today-paused/warming-up 60s, prior-session 60s (452-462). Delayed Yahoo maps through today-paused.

## C. Backoff
Missing. Errors are swallowed and the next fixed tick fires (app.ts:468-470, 492-494). fetchJSON throws on non-OK and returns null on 404 (api.ts:8-26). Measured: 500s keep a flat 10s cadence in live mode and 60s in delayed mode. Retry-After handling is commented out (api.ts:18-22). Also, if the very first load fails, `mode` stays null and nothing ever polls again; resume is a no-op (486). This is pre-existing.

## D. Measured rates (headless Chromium, Playwright 1.64, Go servers built from each tree, real Yahoo backend; /api/hero stubbed via page.route only where noted)
| Scenario | WTI /min | BRENT /min | gaps |
|---|---|---|---|
| HEAD real (Yahoo delayed), visible 180s | 1 | 1 | 60s |
| HEAD stub mode=live, visible 60s | 6 | 6 | 10s |
| HEAD stub live, hidden 60s | 0 | 0 | - |
| HEAD stub live, visible again 60s | 6 | 6 | 10s |
| HEAD stub live → 500s, 150s | 6 | 6 | flat 10s (no backoff) |
| HEAD stub delayed → 500s, 180s | 1 | 1 | flat 60s |
| HEAD race: rapid hide/show toggles, then visible 60s | 11.9 | 11.9 | 0.2s/9.8s pairs = 2 timers |
| HEAD race: toggles then hidden 60s | 11 | 11 | polling WHILE HIDDEN |
| BASE real, visible 90s | 30 | 30 | 2s |
| BASE real, hidden 90s | 30 | 30 | 2s (no pause) |
| BASE stub live → 500s | 30 | 30 | flat 2s |
Visibility was simulated by overriding Document.prototype.visibilityState/hidden plus dispatching `visibilitychange`. Harness: harness/run.js; raw data: harness/results.json.

## E. Tests
- go test: base 82 top-level (97 incl. subtests), all pass. Head 80 top-level (94), all pass.
- Removed tests, all analysis-specific:
  - models.TestMarketAnalysisJSONShape
  - services.TestGetAnalysisIncludesCoreFields
  - subtest server.TestNewServerHandlerWiresRoutesAndMiddleware/analysis
- An analysis assertion block was also removed from inside the handlers_test.go JSON endpoints test (the `/api/analysis` decode + score check).
- No unrelated deletions, no new t.Skip (the existing t.Skip at handlers_test.go:285 is pre-existing), and no weakened non-analysis assertions.
- Added: none. No test asserts that /api/analysis now 404s (I verified it manually: base 200, head 404).
- TS: the repo has no TS unit tests. Only the `tsc --noEmit` type check exists, and it fails on head.

## F. LIVE badge
- SSR: the pill is hidden unless `.Price.IsLive` (partials/hero_chart_card.html:7). IsLive = source=="pyth" && age ≤60s (handlers/pages.go:409-419). With Yahoo, SSR is always `display:none`. Verified on both servers.
- Client: setLiveState('live') only when payload.mode=='live' (app.ts ~398). The server sets live only when a Pyth quote is ≤90s old (market_data.go:739-745, 769-771). Delayed maps to 'DELAYED' (app.ts:408). JS never changes the pill's `display`, so a hidden pill stays hidden even with label LIVE/DELAYED. Measured: label "DELAYED"/"LIVE", visible=false.
- Result: with Yahoo/delayed, LIVE is never shown (safe), both before and after the first poll. The flip side: DELAYED / MARKET CLOSED are never visible either when SSR hid the pill, and a real Pyth-live page shows LIVE only if SSR saw it live. The pill title "Streaming live 1-minute candles" is static. None of this changed in this PR.

## G. Made-up data / leftovers
- Fake analysis text is gone (services/market_data.go). Route, handler, and model removed. README API row removed.
- Leftovers:
  - `web/src/api.ts:1` still imports `MarketAnalysis`, which breaks tsc.
  - README.md:3 still advertises "AI-powered analysis".
  - Hardcoded `basePrices` (market_data.go:35-46) are now dead, written but never read in non-test code. They were only used by GetAnalysis.
  - `generateDaily`/`generateIntraday` random OHLC generators (market_data.go:348, 382) are dead code, used only by a test.
  - None of these are served. Recommend deleting them.
- No /api/analysis in routes, templates, nav, sitemap (main.go:87+), robots.txt, or scripts.

## H. Commands
| Command | base | head |
|---|---|---|
| go vet ./... | exit 0 | exit 0 |
| go test -count=1 -json ./... | exit 0, 82 pass | exit 0, 80 pass |
| npm ci | 0 | 0 |
| npm run build (esbuild) | 0 | 0 |
| npm run test:types (tsc --noEmit) | 0 | **2 (TS2305)** |
| lint | no lint config/script in repo | same |

## Suggested fixes
1. Remove `MarketAnalysis` from the api.ts import.
2. In resumePolling: clear any existing timer inside startPolling, re-check `document.hidden` and `pollTimer` after the await, and add a resume-in-flight flag. Or replace setInterval with a single self-scheduling setTimeout chain that has an in-flight guard.
3. Add exponential backoff with a cap (e.g. ×2 up to 5 min, reset on success) and honor Retry-After/429. Retry the initial load on failure.
4. Either implement JS pill visibility (show for LIVE/DELAYED/CLOSED) or correct the PR description.
5. Add a test asserting GET /api/analysis returns 404.
6. Delete dead basePrices/generate* code and fix the README tagline.

---

# v2 re-review: head 9965e65ff1955f30a02e62f942a5ef2f0cb8e447 (base efb177f)
Read-only again: nothing pushed, merged, approved, commented on, or deployed.

## Verdict: FAIL

Blockers:
1. **Stale-mode polling cadence.** The `scheduleNextPoll(mode)` callback reschedules with the `mode` it captured (app.ts:491, 507, 520), not `this.mode`. When a poll response changes mode, applyPayload→transitionPolling schedules the new cadence, and line 520 then cancels it and re-schedules the OLD one.
   - live→today-paused stays at 10s instead of 60s (measured, no latency).
   - today-paused→live stays at 60s instead of 10s while the badge says LIVE (measured).
   - prior-session→live (Sunday open) or the CME-break return would therefore update a "LIVE" chart only once a minute until reload.
   - Prod today is Yahoo-only, so mode never changes and this does not show there, but it is wrong as soon as Pyth is on.
2. **A failed first load does not retry robustly.** load() retries exactly once after 2s (app.ts:230-241). If both attempts fail, `mode` stays null, no timer is ever scheduled, and resumePolling bails at `if (!this.mode) return` (app.ts:532). Measured: first 3 requests 500 → 1 retry, then 0 requests for 120s, even after a hidden/visible nudge. This is a permanent stall until reload.
3. **Dead placeholder data was NOT removed** (requested item 5; market_data.go is unchanged between v1 and v2):
   - `"math/rand"` at market_data.go:9
   - `rng` at :15, :48
   - hardcoded `basePrices` (WTI 72.45, BRENT 76.82 …) at :35-46, :49
   - `generateDaily` at :348
   - `generateIntraday` at :382
   They aren't served, but they are still there.

## Provenance / hash verification
- PR head is now 9965e65ff1955f30a02e62f942a5ef2f0cb8e447, base efb177f. 2 commits, 13 files, +162/-216, still a draft.
- head2/ comes from the codeload tarball at that exact SHA. Hash-verified against connector `get_git_tree(recursive)`: 71/71 blobs match, not truncated. All 13 `list_pull_request_files` blob SHAs match.
- `diff -rq base head2` gives exactly those 13 files.
- I re-verified after the box restarted mid-review: still 71/71.

## dd7b905..9965e65 delta (5 files)
- README.md:3 tagline: "AI-powered analysis" → "live prices, interactive charts, and institutional forecasts".
- handlers_test.go:97-108: new `TestAnalysisEndpointRemoved`.
- api.ts: the `MarketAnalysis` import was dropped (fixes tsc). On 429/503 it attaches `err.retryAfter = parseInt(header) || 60` (api.ts:18-24). An HTTP-date Retry-After is treated as 60s. 404 still returns null (api.ts:14-16).
- app.ts:
  - setTimeout chain with an in-flight guard and backoff (447-546)
  - initial-load single retry (230-241)
  - setLiveState forces `style.display=''` (554)
  - pulse only fires in live mode
- hero_chart_card.html:7-9: SSR pill is always visible when HasPrice. Label is LIVE or DELAYED, plus classes.
- **Unrelated / scope creep to flag:**
  - setLiveState drops the session date from "MARKET CLOSED · <date>" and "FEED PAUSED · today". The `sessionDate` param is now unused at app.ts:550.
  - The pill `title` tooltip was removed.
  - SSR labels every non-live price "DELAYED", including a stale Pyth source, which JS would call FEED PAUSED. It is corrected after the first poll.
  - None of this is harmful, but it isn't in the request.

## 1. Single timer under hide/show (PASS for stacking)
- cancelPolling runs before every schedule (473), there's an in-flight guard (489-493, 495), resumePolling bails when a timer or fetch exists (531), and `document.hidden` is re-checked before scheduling (476) and at fire time (485).
- Measured with a 2s stubbed latency and 3 bursts of 4 hide/show toggles, each landing about 300ms into a fetch:
  - visible afterwards: 12s gaps only (10s + 2s latency), with no paired gaps
  - ending hidden mid-fetch: 0/min
  - visible again: single 12s cadence
- Mode change mid-fetch (with a toggle): still a single timer, no pairs, but with the wrong cadence (blocker 1).
- Minor: if the timer ever fires while hidden (485-486) it returns without nulling `pollTimer`, and a later resume would bail at 531. This is unreachable in practice because pause clears the timer first.

## 2. Backoff (PASS with notes)
- base × 2^level, capped at 300s, level ≤ 5 (478-481, 513-514); resets on success (499).
- 500s in live mode: gaps 20, 40, 80, 160s. The next would be 300s (cap, by code).
- 500 ×2 then OK: 20, 40, then back to a 10s cadence.
- Delayed mode: first error, then the next is at least 120s later.
- 404: fetchJSON returns null, and `applyPayload(null)` throws a TypeError on `payload.bars`, which reaches the catch. Measured gaps 20, 40, 80, and recovery 20, 40 → 10s. So 404 does back off, but only by accident through a TypeError. Recommend throwing on 404 for the hero endpoint.
- 429 with Retry-After: 30: gaps 40, 40, 40 (= 30s Retry-After + 10s base; the level isn't incremented), then 10s. Honored (≥30s), but additive rather than exact, and it doesn't escalate across repeated 429s.
- The resume path (539-541) doesn't bump backoff, but each resume costs only one request.

## 3. Failed first load (FAIL)
- 1 failure, then OK: polling starts (10s cadence).
- 3 failures, then OK: only the single retry, then a permanent stall (0 requests over 120s, even after a visibility nudge).

## 4. Badges (PASS)
- **Real Yahoo (delayed).** Today's Saturday session is still inside the rolling 24h window, so the server returns mode=today-paused, delayed=true.
  - SSR with JS disabled: "DELAYED", display:flex, visible, 79×24, class `hero-live-pill hero-live-pill-paused`, dot animation none, no `hidden` attribute.
  - After JS, after 2 polls, and after hide/show: still "DELAYED", same computed visibility.
  - LIVE never appeared.
- Stubbed live: SSR shows DELAYED, and after the first poll it shows "LIVE", display:flex 58×24, green pulse.
- Stubbed prior-session: "MARKET CLOSED", visible (no date, see above).
- Template: hero_chart_card.html:7-9. Hidden only when there is no price.

## 5. Dead code (FAIL)
- market_data.go:9 `"math/rand"`, :15/:48 `rng`, :35-46 basePrices, :348 generateDaily, :382 generateIntraday are all still present. They are the only math/rand use in non-test Go.
- No Math.random in web/src.
- README tagline is fixed (README.md:3), and there are no other "analysis" claims in the README.

## 6. Tests (PASS)
- 81 top-level (95 with subtests), all pass; base was 82 (97).
- Removed:
  - models.TestMarketAnalysisJSONShape
  - services.TestGetAnalysisIncludesCoreFields
  - server.TestNewServerHandlerWiresRoutesAndMiddleware/analysis
  - the analysis decode/score asserts inside handlers.TestRegisterRoutesPriceAndAnalysis
- Added: handlers.TestAnalysisEndpointRemoved, which passes and asserts 404. Live server: GET /api/analysis → 404.
- t.Fatal/t.Error counts per file:
  - handlers_test 25→24 (−2 analysis, +1 new)
  - models_test 6→3
  - market_data_test 33→28
  - all other files unchanged
- No new t.Skip (the only one is pre-existing, handlers_test.go:298).
- No TS unit tests exist.

## 7. Commands (head2): all pass
| Command | Result |
|---|---|
| go vet ./... | 0 |
| go test -count=1 -json ./... | 0, 81 tests / 95 incl. subtests |
| npm ci | 0 |
| npm run test:types | 0 |
| npm run build | 0 |
| make test | 0 (go test + tsc) |
- Toolchain notes: after the restart the box had go1.27.2 (go1.24.4 before). I re-ran everything on the new toolchain with the same results. make was installed via apt.

## Measured rates (per symbol; WTI and BRENT were identical)
| Scenario | Rate | Gaps |
|---|---|---|
| Real Yahoo, visible 125s | ~1/min | 60s |
| Real Yahoo, hidden 60s | 0 | - |
| Real Yahoo, visible again | immediate + 60s | 0, 60 |
| Live stub 2s latency, baseline | 5/min | 12s |
| ...after 3 mid-fetch toggle bursts | 5/min | 12s only, no pairs |
| ...hidden after mid-fetch toggle | 0 | - |
| ...visible again | 5/min | 12s |
| Live→today-paused flip (no latency) | 6/min (should be 1) | 10s |
| Live→today-paused flip (2s latency + toggle) | 4.8/min (should be 1) | 12s |
| Today-paused→live flip (2s latency) | ~1/min (should be 6) | 62s |
| 500s, live | backing off | 20, 40, 80, 160 |
| 500 ×2 then OK | recovers | 20, 40, 10, 10… |
| 404 ×2 then OK | recovers | 20, 40, 10, 10… |
| 429 Retry-After 30 ×3 then OK | | 40, 40, 40, 10, 10… |
| Initial 500 ×1 then OK | 6/min | 10s |
| Initial 500 ×3 then OK | 0 (stalled) | - |

Harness: harness/run2.js and run3.js. Output: harness/out2.txt, out3.txt; raw data: results2.json, results3.json. Only /api/hero/* is stubbed, via Playwright page.route. Servers run from the head2 dir on the real Yahoo backend.

## Suggested fixes
1. Reschedule with `this.mode` instead of the captured `mode`. Or skip line 520 when applyPayload already rescheduled, e.g. by comparing `this.pollTimer`.
2. Initial load: fall into the normal backoff schedule, e.g. set a provisional mode and call scheduleNextPoll, instead of a single retry.
3. Delete basePrices, rng, generateDaily/generateIntraday, the math/rand import, and their test.
4. Throw on 404 for hero. Treat Retry-After as an absolute delay (and parse HTTP-date).
5. Restore the session date in the labels, or call that change out in the PR.

---

# v3 re-review: head c9d0d31fc8acf1827d9c2c476d7cf9a15368dd4d (base efb177f)
Read-only: nothing pushed, merged, approved, commented on, or deployed. The PR is now marked ready for review (draft=false). 3 commits, 13 files, +208/-347.

## Verdict: FAIL
- The one blocker is item 4: Retry-After does NOT replace the interval.
  - The Retry-After timer (app.ts:513-515) calls `scheduleNextPoll()`, which waits a *further* `base × 2^level` before fetching (app.ts:479-483).
  - Retry-After: 30 measured 50, 70, 110s, not ~30s. The code comment at app.ts:508 ("replaces the normal interval") is wrong.
- Everything else passes, some with notes (below).

## Provenance
- head3/ comes from the codeload tarball at c9d0d31. Connector `get_git_tree(recursive)`: 71/71 blobs match, not truncated. All 13 `list_pull_request_files` SHAs match.
- `diff -rq base head3` gives the same 13 files.

## 9965e65..c9d0d31 delta (5 files): all in scope
- market_data.go: removed `math/rand`, `rng`, `basePrices`, `generateDaily`, `generateIntraday` (−117 lines).
- market_data_test.go: removed `TestGenerateDailySkipsWeekends` and the rand/basePrices fixture.
- api.ts:21-35: Retry-After parsing (integer or HTTP-date, 60s fallback).
- app.ts:
  - `scheduleNextPoll()` reads `this.mode` (469-529)
  - explicit 404/null branch (498-500, 549-550)
  - first-load failure enters the schedule (230-237)
  - resume works with no mode (538-562)
  - Retry-After escalation (512)
  - badge titles and dates restored (577-606)
  - trailing newline
- hero_chart_card.html:7-9: title attr. DELAYED only when Source=="yahoo"; otherwise an empty label.
- Nothing unrelated found.

## 1. Mode transitions use the current mode: PASS
- Code: `const mode = this.mode || 'warming-up'` (app.ts:476); every reschedule calls `scheduleNextPoll()` (491, 514, 527, 562, 448).
- live→today-paused: 10s, then 60, 60.
- today-paused→live flipped mid-fetch (2s latency): 12s cadence (10s + latency).
- During backoff: live + 500×2, then OK as today-paused: 20, 40, then 60, 60. today-paused + 500×1, then OK as live: 120, then 10s.
- While hidden: live, hide, flip to today-paused, show → 0 hidden, immediate fetch, then 60, 60. Reverse → immediate, then 10s.

## 2. First-load failure enters backoff; resume with no prior load: PASS with notes
- Code: app.ts:230-237 sets `mode='warming-up'` and `backoffLevel=1`, then calls scheduleNextPoll. Resume defaults the mode (542-544).
- F1, 4×500 then OK (no latency): first retry +120s, then 261, 300, 300, then OK → 10s, and the badge goes LIVE.
  - I expected 240 for the second gap. 261 was measured in both F1 and F3, deterministically; the extra ~21s is unexplained but within the cap.
- F2b, 500×3 with 2s latency and 2 hide/show toggles during the first fetch: one resume fetch, then gaps 242, 302, then OK → 12s cadence. No double-start, no stall.
- F2, nudges every 30s while failing: each show gives exactly one fetch, with no extra timers.
- Notes:
  - With the warming-up base of 60s, the first retry after a cold failure is 120s, and 4 failures mean ~16 minutes of an empty chart.
  - load() unconditionally sets level=1 (236).

## 3. Dead code gone: PASS
- No `math/rand`, `rand.`, `rng`, `basePrices`, `generateDaily` or `generateIntraday` in non-test Go.
- The only `math/rand` left is in a test, the pre-existing forecast_status_test.go:6,16 (seeded RNG fixture).
- No Math.random in web/src.
- Go test names, base (82 top-level / 97 with subtests) vs head3 (80 / 94):
  - Removed:
    - models.TestMarketAnalysisJSONShape
    - services.TestGetAnalysisIncludesCoreFields
    - server.TestNewServerHandlerWiresRoutesAndMiddleware/analysis (subtest)
    - services.TestGenerateDailySkipsWeekends
  - Added: handlers.TestAnalysisEndpointRemoved
  - The full removed set is the analysis tests plus TestGenerateDailySkipsWeekends, as expected.
- t.Fatal/t.Error counts:
  - handlers_test 25→24
  - models_test 6→3
  - market_data_test 33→26 (−5 analysis, −2 generateDaily)
  - all other files unchanged
- No new t.Skip (the pre-existing one is at handlers_test.go:298).

## 4. Retry-After: FAIL (additive, not replacing)
- 429 Retry-After: 30, ×3 then OK (live): gaps 50, 70, 110, then 10, 10. That is 30 + 10·2^level, with level escalating 1→2→3. Escalation (512) and recovery work, but the interval is not replaced.
- HTTP-date +25s: parsed correctly (api.ts:28-30 → ~24s), but the gap was 44 (= 24 + 20).
- 503 Retry-After: 30 → 50, then 10s.
- Fix: have the Retry-After timer run the fetch directly, e.g. factor the timer body into `pollOnce()` and use `setTimeout(pollOnce, retryAfterMs)`, instead of calling `scheduleNextPoll()`.

## 5. 404 explicit backoff: PASS with note
- Poll path: app.ts:498-500 (`if (payload === null) backoffLevel++`). Resume path: app.ts:549-550. fetchJSON still returns null on 404 (api.ts:14-16).
- Measured 404×3 then OK: 20, 40, 80, then 10s, identical to 500s.
- Note: the *initial* load() still has no null check (227-228). `applyPayload(null)` throws a TypeError at app.ts:366 (`payload.bars`), which lands in the catch at 230. The behavior is correct (F3: initial 404×2 → 120, 261, then 10s), but that path is still TypeError-driven.

## 6. Badge: PASS with note
- SSR, real Yahoo (JS off, CSS loaded): "DELAYED", title "15-minute delayed exchange data", display:flex, visible, 79×24, grey paused class.
- After JS + polls + hide/show: still DELAYED and visible. LIVE never appeared on Yahoo.
- Stale Pyth SSR (scratch Go test rendering home with source=pyth, updatedAt −10min; not part of the PR):
  - It does NOT say DELAYED.
  - It renders an EMPTY grey chip: dot only, text "", title "Feed status loading", display:flex, 25×15. Screenshot: harness/ssr-pyth-stale.png.
  - With JS it becomes "FEED PAUSED · today" (title "Feed temporarily paused"). With JS off it stays a label-less chip.
- Fresh Pyth SSR: "LIVE", green, title "Streaming real-time exchange data".
- Dates and tooltips restored:
  - prior-session → "MARKET CLOSED · Sat, Oct 17", title "Market is closed" (app.ts:594-600)
  - today-paused, non-delayed → "FEED PAUSED · today" (582-587)
  - warming-up → "WARMING UP", amber pulse
  - stubbed live → "LIVE", visible, green pulse
- Suggestion: have SSR render "FEED PAUSED" or "LAST TICK" for a stale Pyth price instead of an empty chip.

## 7. Regressions: PASS
- Live stub with 2s latency and 3 bursts of 4 hide/show toggles mid-fetch: 12s gaps only (no pairs), 0/min when hidden after a mid-fetch toggle, a single 12s cadence on show.
- Real Yahoo: ~1/min (60s gaps) visible, 0/min hidden, immediate fetch on show, then 60s.

## 8. Commands (head3): all pass
| Command | Result |
|---|---|
| go vet ./... | 0 |
| go test -count=1 -json ./... | 0, 80 top-level / 94 incl. subtests, all pass |
| npm ci | 0 |
| npm run test:types | 0 |
| npm run build | 0 |
| make test | 0 |
- Toolchain: go1.27.2, node 20.19.2.

## Measured rates (per symbol; WTI = BRENT)
| Scenario | Gaps / rate |
|---|---|
| Real Yahoo, visible | 60s (~1/min) |
| Real Yahoo, hidden | 0/min |
| Real Yahoo, show | immediate, then 60s |
| Live stub 2s latency, after toggle bursts | 12s only, no pairs |
| Same, hidden after mid-fetch toggle | 0/min |
| live→today-paused | 60, 60 |
| today-paused→live (mid-fetch) | 12s |
| live + 500×2 → today-paused | 20, 40, 60, 60 |
| today-paused + 500×1 → live | 120, 10… |
| hidden: live→today-paused, show | immediate, 60, 60 |
| hidden: today-paused→live, show | immediate, 10… |
| 500×3 then OK | 20, 40, 80, 10… |
| 404×3 then OK | 20, 40, 80, 10… |
| 429 RA 30 ×3 then OK | **50, 70, 110**, 10… (expected ~30, 30, 30) |
| 429 RA HTTP-date +25s | **44**, 10… (expected ~25) |
| 503 RA 30 | **50**, 10… |
| First load 500×4 then OK | +120, 261, 300, 300, 10… |
| First load 404×2 then OK | +120, 261, 10… |
| First load 500×3 + toggles (2s latency) | 242, 302, 12… |

- Harness: harness/run4.js (output out4.txt; F2b in out4b.txt and out4b-F2b.json), harness/ssr_fixture.js plus screenshots ssr-*.png.
- The SSR fixtures came from a scratch Go test in a copy at /tmp/head3-ssr; head3/ was not modified.
- Server ran from the head3 dir on port 8144 with the real Yahoo backend. Only /api/hero/* and, for the fixtures, the HTML document were stubbed.

---

# v4 re-review: head e1ffecafa01bd3f0465b4364fb4468f1078af0ef (base efb177f)
Read-only: nothing pushed, merged, approved, commented on, or deployed. 4 commits, 13 files, +239/-348. Ready for review (not draft), not merged.

## Verdict: FAIL
1. **New stall: a Retry-After timer that fires while the tab is hidden kills polling for good.**
   - The RA callback returns at app.ts:527 (`if (document.hidden) return;`) without clearing `pollTimer`.
   - On show, `resumePolling` bails at app.ts:569 (`pollTimer != null`).
   - Measured (R4): 429 RA 10 lands while hidden, the tab is shown 20s later → **0 requests in the next 90s**.
2. **A 429 returned by the Retry-After fetch itself has its Retry-After ignored.** Its catch (app.ts:538-540) only bumps the level. The next poll is base×2^level instead of RA, so repeated 429s alternate between RA and exponential backoff:
   - RA 30 ×3 → 30, 40, 30
   - RA 10 ×6 → 10, 40, 10, then 160 (predicted)
   - If the server sent RA 120 on the second 429, the client would come back after 40s, violating it.
3. **The cold first-load retry is not 10s and does not go 10, 20, 40.**
   - app.ts:236/249 set a 10s timer that calls `scheduleNextPoll()`, which then waits a further warming-up base of 60s × 2^0 (app.ts:488-495). First retry ≈ 70s, then 120, 240, 300 (measured F1, F3).
   - The same lines assign `pollTimer` without `cancelPolling()` and reset `backoffLevel = 0`. When a hide/show resume finishes before a slow first load fails, there are briefly two timer chains (F2c: requests at 0.6, 72.9, 121.1s), and resume's escalation is discarded. It self-heals at the next shared-slot cancel, but it is a double start.

## Provenance
- head4/ comes from the codeload tarball at e1ffeca. Connector `get_git_tree(recursive)`: 71/71 blobs match. All 13 `list_pull_request_files` SHAs match.
- `diff -rq base head4` gives the same 13 files.

## c9d0d31..e1ffeca delta (2 files): in scope, nothing unrelated
- app.ts:
  - first-load null/404 branch and a 10s first retry (226-250)
  - Retry-After callback now fetches directly (520-547)
- hero_chart_card.html:7-9: title "Feed temporarily paused" and label "FEED PAUSED" when Source=="pyth" and not live.

## 1. Retry-After: FAIL
- Single-RA cases are correct now:
  - RA 30 → 30s
  - HTTP-date +25s → 24s
  - 503 RA 30 → 30s
  - each then recovers to 10s
  - 429 RA 10 then 500 ×2 → 10, 40, 80, then 10s
- One timer: the RA timer occupies the only `pollTimer` slot, and resume is blocked while it is set (569).
  - Hide/show 5s into a 30s RA wait → immediate fetch, then a single 10s cadence (R5).
  - No overlap with a normal timer was observed.
  - The RA callback doesn't check `inFlight` itself (529), but nothing else can run while it owns the slot.
- Escalation: the level goes +1 per 429 (525, 540), but RA is honored only on 429s seen by the normal poll path, so the gaps alternate (see blocker 2).
- Hidden stall: see blocker 1.

## 2. Cold first load: FAIL
| Run | Gaps |
|---|---|
| F1: 500 ×5 | +68.8 (≈10 + 60), 120, 240, 300 |
| F3: 404 ×3 then OK | +69.3, 120, 240, then 10s |
| F2b: 2 toggles during a failing first fetch (2s latency) | resume fetch at 0.6s, then 122, 242, then 12s |
| F2c: first fetch slower than the resume fetch | double chain, as in blocker 3 |
- F2b had no stall and no double start.
- Fix: make the first-load retry schedule the fetch directly at 10·2^n (cap), via `cancelPolling()` plus a dedicated `pollOnce`, and don't reset the level when resume has already escalated.

## 3. Explicit null/404 on first load: PASS
- app.ts:228-236: `if (payload === null) { … mode='warming-up'; backoffLevel=0; setTimeout(scheduleNextPoll, 10_000) }`. It no longer relies on the TypeError.
- Poll path: 510-512. Resume path: 584-585.

## 4. Badges: PASS
- SSR fixtures via /tmp/head4-ssr scratch test plus harness/ssr_fixture4.js, CSS loaded, JS off:
  - stale Pyth → "FEED PAUSED", title "Feed temporarily paused", flex, 106×24, grey
  - fresh Pyth → "LIVE", green
  - Yahoo → "DELAYED", title "15-minute delayed exchange data", flex, 79×24
- Real Yahoo after JS, polls and hide/show: still DELAYED, visible. LIVE never appeared on Yahoo.
- Restored labels and titles still present:
  - "MARKET CLOSED · Sat, Oct 17" / "Market is closed"
  - "FEED PAUSED · today" / "Feed temporarily paused"
  - "WARMING UP" / "Loading chart data" (amber pulse)
  - stub live → "LIVE" / "Streaming real-time exchange data"

## 5. Regressions: PASS
- Mid-fetch hide/show bursts (2s latency): 12s gaps only, 0/min hidden, single cadence on show.
- Mode transitions:
  - live→today-paused: 60, 60
  - today-paused→live mid-fetch: 12s
  - during backoff: 20, 40, 60, 60 and 120, then 10s
  - while hidden: immediate, then 60s / 10s
- Real Yahoo: 1/min visible, 0 hidden, immediate fetch on show.
- 500 ×3 and 404 ×3 then OK: 20, 40, 80, then 10s (same as v3).

## 6. Tests and commands: PASS
- Go test names are identical to v3: 80 top-level / 94 with subtests, all pass. t.Fatal/t.Error/t.Skip counts per file unchanged vs v3.
| Command | Result |
|---|---|
| go vet ./... | 0 |
| go test -count=1 -json ./... | 0 (80/94) |
| npm ci | 0 |
| npm run test:types | 0 |
| npm run build | 0 |
| make test | 0 |

## Measured rates (per symbol; WTI = BRENT)
| Scenario | Gaps |
|---|---|
| 429 RA 30 ×3 then OK | 30, **40**, 30, 10… |
| 429 RA 10 ×6 | 10, **40**, 10, (next ~160) |
| 429 RA HTTP-date +25s | 24, 10… |
| 503 RA 30 | 30, 10… |
| 429 RA 10 → 500 → 500 → OK | 10, 40, 80, 10… |
| 429 RA 10 arrives while hidden, show after 20s | **0 requests in 90s (stall)** |
| Hide/show during RA 30 wait | immediate, 10… |
| Cold first load 500 ×5 | **+69, 120, 240, 300** |
| Cold first load 404 ×3 then OK | **+69, 120, 240**, 10… |
| First load failing + toggles (equal latency) | 122, 242, 12… |
| First load slower than resume | requests 0.6, 72.9, 121.1 (**two chains**), then 240.5, 10.5… |
| Mid-fetch toggle bursts | 12s only; hidden 0 |
| live→today-paused / today-paused→live | 60 / 12 |
| Backoff transitions | 20, 40, 60, 60 / 120, 10 |
| Hidden transitions | immediate, then 60 / 10 |
| Real Yahoo visible / hidden / show | 60s / 0 / immediate |
| 500 ×3, 404 ×3 then OK | 20, 40, 80, 10… |

Harness: harness/run5.js → out5.txt (plus per-scenario JSON). Server ran from the head4 dir on :8145 with the real Yahoo backend. Only /api/hero/* and, for the fixtures, the HTML document were stubbed.

## Suggested fixes
1. RA timer: if hidden, set `pollTimer = null` before returning (or call `cancelPolling()`). Better, route all fetches through one `pollOnce()` that handles hidden, inFlight, null, Retry-After and backoff, and have every timer call it.
2. In the RA fetch's catch, honor `retryErr.retryAfter` the same way as the main path.
3. First-load retry: `cancelPolling()`, then `setTimeout(pollOnce, min(10s·2^n, cap))`. Don't reset a level that resume already raised.

---

# v5 re-review: head 1d7f4d5b189cb16c1e1205f178f64d587f568215 (base efb177f), 2026-10-17 ~03:00–03:46 PDT

## Verdict: FAIL

The new `HeroPoller` logic is mostly right when it can schedule. But as shipped it can't schedule in a browser. `web/src/poller.ts:52` defaults to `{ setTimeout, clearTimeout }`, and `schedule()` then calls `this.timers.setTimeout(...)` (poller.ts:156) with `this` = that object. Chromium throws `TypeError: Illegal invocation`. app.ts:226-232 never passes `timers`, so this default path is always used in production. Measured on head5 (run6, out6.txt):
- every page logs `pageerror: Illegal invocation` ×2 (WTI+BRENT) right after the first fetch;
- no hero request ever follows the first fetch (Yahoo 0/min visible; live 0/min; cold-failure retries 0);
- the only later requests are one fetch per hidden→visible transition, after which polling dies again.

The unit tests always inject FakeTimers, so they never touch this path.

## Provenance
- codeload tarball @1d7f4d5 → head5/. 73/73 tree blobs and 18/18 PR files hash-match the cursor-github `get_git_tree` / `list_pull_request_files` output.
- The PR is 5 commits, +774/-365, 18 files, open, not draft.

## e1ffeca..1d7f4d5 delta (6 files): in scope, with one dependency note
- `web/src/app.ts` (+65/-43):
  - imports HeroPoller;
  - removes pollTimer/inFlight/backoffLevel, transitionPolling, cancelPolling, getBaseInterval and scheduleNextPoll (with its RA inner timer);
  - pause/resume now delegate (app.ts:439-451).
  - `rg 'setTimeout|backoff|pollTimer|inFlight' app.ts` shows no scheduling code left in HeroController. No parallel scheduler remains.
- New `web/src/poller.ts` (180 lines) and `web/src/poller.test.ts` (458 lines).
- `Makefile`: `test: test-go test-ts test-poller` (Makefile:31) and `test-poller: npx tsx web/src/poller.test.ts` (Makefile:39-40).
- `package.json` / `package-lock.json`: devDep `@types/node ^26.6.5` (+ undici-types). **`tsx` is not a dependency.** `npx tsx` downloaded an unpinned latest tsx from the npm registry at test time (it landed in ~/.npm/_npx/fd45a72a545557e9). So `make test` needs the network and isn't reproducible. That's a supply-chain concern. Pin tsx in devDependencies.
- Nothing unrelated. Go code is unchanged.

## 1. Scheduler behaviour (browser, as shipped): FAIL
See the verdict. Root cause is poller.ts:52, the unbound native `setTimeout`/`clearTimeout`. The exception is thrown inside `tick()` after `finally`, so the rejected promise surfaces as an uncaught pageerror and `pollTimer` stays null. That's why `resume()` (poller.ts:66-70) can still fire one fetch per show. Reproduced standalone in Chromium: `const t={setTimeout,clearTimeout}; t.setTimeout(()=>{},1)` → `Illegal invocation`.

Seven scenarios that wait for a subsequent poll (R4, R5, T, M2, N1, A1, A2) never got one. They hung for more than 16 minutes and were killed.

Fix (one line, used for the preview below): `{ setTimeout: (f, ms) => window.setTimeout(f, ms), clearTimeout: h => window.clearTimeout(h) }`, or `.bind(window)`. Also add a test that constructs HeroPoller **without** `timers` (e.g. under jsdom or a real Node timer with a short delay).

## 1b. Preview with only that line patched (/tmp/head5-fix, port 8147, out6fix.txt; not the PR's code)
Every v4 blocker is fixed in the patched build:
- R4: RA expiring while hidden, then show → immediate fetch, then 12s. No stall.
- R8: 429 RA 10, then the retry fetch gets 429 RA 120 → gaps 10, 120, 10.
- R1: RA 30 ×3 → 30, 30, 30, 10. R2: HTTP-date gives 24. R6: RA 10 ×6 gives 10s each.
- F1 / F2c cold → 10, 20, 40, 80, then 160 (+~54s, see below). F2c gave one chain: resume during the in-flight slow first load is blocked by `inFlight`.
- N1: show during backoff → immediate fetch that fails, then 80s. The level is not reset.
- A1 (hide during the in-flight retry fetch): 0 while hidden, immediate fetch on show, then 10s.
- A3 (retry fetch returns today-paused) → 10, then 60, 60.
- M1–M5, E1/E2 (20/40/80, then 10), T (12s gaps, 0 while hidden) and Y (60s visible, 0 hidden, immediate on show) are all correct.

New issues in the patched build:
- **A2 — rapid toggles during an RA 30 wait: Retry-After is bypassed.** `pause()` cancels the RA timer and `resume()` sees `pollTimer===null` and ticks immediately. 8 hide/show toggles produced 8 fetches 0.1s apart (gaps `[5.1, 0.1×7]`) against a server that had said to wait 30s. R5 (one hide/show 5s into the RA wait) also fetched immediately. The same path lets visibility toggles bypass backoff (N1 fetched immediately at failure level 2). Suggest that `resume()` honour a stored `nextAllowedAt` (the RA/backoff deadline) and schedule the remainder instead of ticking.
- Minor: if the tab is hidden mid-fetch and shown before the post-fetch timer fires, `schedule()` has already armed a timer while hidden. `resume()` then won't fetch immediately; it waits for that timer (≤ interval). It's harmless, but tick could skip `schedule()` when `isHidden()`.
- Some ~54–64s gaps (E1/R1/F2c/N1/F1/Y) line up across scenarios at the same wall-clock times. They come from slow real-Yahoo upstream responses through `route.fetch`, not from the scheduler. In each case the request after the gap followed the slow one by exactly the expected interval.

## 2. Badges: PASS (SSR unchanged; after-JS pills unchanged)
- SSR (ssr_fixture5.js, waitUntil load), with titles intact:
  - Yahoo: `DELAYED`, title "15-minute delayed exchange data", flex;
  - stale Pyth: `FEED PAUSED`, title "Feed temporarily paused";
  - fresh Pyth: `LIVE`, title "Streaming real-time exchange data".
- After JS:
  - B4 `FEED PAUSED · today`
  - B5 `MARKET CLOSED · Sat, Oct 17`
  - B6 `LIVE`
  - B7 `WARMING UP`
  - Yahoo is always `DELAYED`, never LIVE.

## 3. Poller unit tests
- They import the shipped module: `import { HeroPoller, ... } from './poller'` (poller.test.ts:2). It isn't a copy.
- `make test` runs them: Makefile:31,39-40. Output `7 passed, 0 failed`; the runner exits non-zero on failure (poller.test.ts:453-455).
- Weak tests:
  - `pause while in flight does not reschedule` (poller.test.ts:385-432) asserts the opposite of its name (`timer scheduled after fetch`, :423).
  - `resume during slow first load gives single chain` (:292-347) only asserts `fetchCount >= 2`. Its own comment (:335) admits the fetch already completed, so it checks no chain count.
  - `repeated 429s` uses RA 30 three times, so it can't catch "first RA reused" (no 10 → 120 case).
- FakeTimers.tick (:33-53) moves the clock to the end of the window *before* firing callbacks. Fetches are timestamped at the window end, so the gap assertions mostly echo the test's own tick sizes. That's why several timing mutations survive.
- Mutation results (/tmp/head5-mut, one change at a time; the shipped file was verified unchanged afterward; head5-mutations.txt):

| mutation | result |
|---|---|
| RA additive (RA+backoff) | KILLED (2 tests) |
| RA ignored (use backoff) | KILLED |
| tick: hidden check removed | KILLED |
| tick hidden branch: no cancelTimer | survived (equivalent: callback already nulls handle) |
| schedule: no cancel-before-set | **survived** |
| timer callback doesn't null own handle | **survived** |
| no backoff reset on success (`failures=0` removed) | **survived** |
| cap removed | **survived** |
| cap 300s→600s | **survived** |
| cold base 10s→60s | KILLED |
| cold exponent off by one | KILLED |
| warm exponent off by one | **survived** |
| resume ignores inFlight | **survived** |
| resume ignores pollTimer | **survived** |
| resume resets failures | **survived** |
| tick inFlight guard removed | **survived** |
| pause() no-op | KILLED |
| mode read before onSuccess | KILLED |
| live 10s→60s | KILLED |
| null payload not counted as failure | **survived** |
| (real bug) default timers unbound | not covered: every test injects timers |

  9 killed, 11 survived (10 if you exclude the equivalent mutant). The user named five behaviours: Retry-After replace was killed, while hidden-clear (equivalent), cancel-before-set, backoff reset and cap all **survive**.

- Build/embed isolation: PASS.
  - The esbuild sourcemap sources are api.ts, charts.ts, poller.ts and app.ts. poller.test.ts and tsx are not bundled.
  - app.js is 201,596 B vs 202,132 B in v4.
  - Go has no `go:embed`.
  - scripts/deploy.sh is unchanged (npm ci + build; tests are not run).
  - The only side effect: tsconfig `include: web/src/**/*.ts` now type-checks the test file, and @types/node globals leak into the browser type environment. That's harmless today.

## 4. Commands (head5): all pass
- `go vet ./...`: clean.
- `go test ./...`: ok. 80 top-level / 94 incl. subtests, all pass. Names are identical to v4; per-file assertion counts are unchanged.
- Go test changes vs base are still the same as v4. Removed: models.TestMarketAnalysisJSONShape, services.TestGetAnalysisIncludesCoreFields, server.…/analysis, services.TestGenerateDailySkipsWeekends. Added: handlers.TestAnalysisEndpointRemoved.
- `npm ci`, `npm run test:types` (tsc) and `npm run build`: exit 0.
- `make test`: exit 0 (go test, tsc, then poller `7 passed, 0 failed`; tsx was fetched via npx).

## Measured rates, per symbol (WTI = BRENT)
| scenario | head5 as shipped (run6) | head5 + timer-bind fix (preview) | expected |
|---|---|---|---|
| Y Yahoo visible / hidden / show | 0/min after load; 0; 1 fetch on show then 0 | 60s; 0; immediate then 60s | 1/min, 0, immediate |
| B6 live stub 16s | 1 fetch only | n/a | — |
| F1 cold 500×5 | 1 fetch, no retries (936s) | 10, 20, 40, 80, 160*, then 10 | 10, 20, 40… capped |
| F3 cold 404×3 | 1 fetch, no retries | n/a | 10, 20, 40 |
| F2b / F2c slow first load + toggles | 2 / 1 fetches total, no retries | F2c: one chain 10, 20, 40, 80, then 10 | one chain |
| R4 RA expires hidden, show | never got a 2nd poll (hung) | immediate, then 12s | resume |
| R8 RA 10 then 120 | 0 polls after load | 10, 120, 10 | 10, 120 |
| R1 RA30×3 / R2 date / R3 503 | 0 polls after load | 30, 30, 30, 10 / 24 / n/a | RA |
| R6 RA10×6 | 0 | 10×6, then 10 | 10s |
| R7 429 + 500×2 | 0 | n/a | — |
| A1 hide during retry fetch | hung | 0 hidden; immediate on show, then 10 | ✓ |
| A2 toggles ×8 during RA30 | hung | **8 fetches 0.1s apart** | none until RA |
| A3 mode change on retry fetch | 0 | 10, then 60, 60 | 60 |
| N1 resume during backoff | hung | immediate 500, then 80 (level kept) | level kept |
| T toggle bursts (2s latency) | hung | 12s gaps; 0 while hidden | 12s |
| M1 / M3 / M3b | 0 polls | 60, 60 / 20, 40, 60 / n/a | ✓ |
| M2 | hung | 12s | 12s |
| M4 / M5 hidden flip | 1 fetch on show, then 0 | immediate, then 60 / immediate, then 10 | ✓ |
| E1 500×3 / E2 404×3 | 0 | 20, 40, 80, then 10 | ✓ |

\* plus a ~54s upstream Yahoo stall in that window (213.8s measured).

## Suggested fixes
1. poller.ts:52: bind the default timers to `window`/`globalThis`, and add a test that uses the default timers.
2. Have resume() honour the pending RA/backoff deadline instead of ticking immediately (A2/R5).
3. Pin `tsx` in devDependencies (or use `node --experimental-strip-types`).
4. Strengthen the tests: fire FakeTimers at `fireAt` (set the clock to each timer's time), and add cases for 10 → 120 RA, backoff reset after recovery, the 300s cap, a real single-chain assertion and resume guards. Fix the misnamed pause test.
