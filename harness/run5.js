// PR42 v3 harness. Only /api/hero/* is stubbed (page.route) where `stub` is set; `doc` overrides the HTML document (SSR fixture).
const { chromium } = require('/workspace/lop-review/head4/node_modules/playwright');
const fs = require('fs');
const H = process.env.TARGET || 'http://localhost:8145';
const OUT = process.env.OUT || '/workspace/lop-review/harness/out5.txt';
const ONLY = process.env.ONLY ? new RegExp(process.env.ONLY) : null;
const sleep = ms => new Promise(r => setTimeout(r, ms));
const VIS_INIT = () => {
  window.__vis = 'visible';
  Object.defineProperty(Document.prototype, 'visibilityState', { get() { return window.__vis; }, configurable: true });
  Object.defineProperty(Document.prototype, 'hidden', { get() { return window.__vis === 'hidden'; }, configurable: true });
};
const setVis = (page, v) => page.evaluate(v => { window.__vis = v; document.dispatchEvent(new Event('visibilitychange')); }, v);
const pillSnap = page => page.$$eval('.hero-live-pill', els => els.map(e => {
  const cs = getComputedStyle(e); const r = e.getBoundingClientRect(); const d = e.querySelector('.hero-live-dot');
  return { text: e.textContent.replace(/\s+/g, ' ').trim(), title: e.getAttribute('title'), hiddenAttr: e.hasAttribute('hidden'), style: e.getAttribute('style'), display: cs.display, visibility: cs.visibility, opacity: cs.opacity, wh: Math.round(r.width) + 'x' + Math.round(r.height), cls: e.className, dotAnim: d ? getComputedStyle(d).animationName : null };
}));
async function scenario(browser, name, opts, script) {
  if (ONLY && !ONLY.test(name)) return null;
  const ctx = await browser.newContext({ javaScriptEnabled: opts.js !== false });
  const page = await ctx.newPage();
  await page.addInitScript(VIS_INIT);
  const reqs = []; let t0 = Date.now(); const out = { name, phases: [], pills: {}, notes: [] };
  const st = { mode: opts.mode, delayed: opts.delayed, delayMs: opts.delayMs || 0, plan: opts.plan || null, n: { WTI: 0, BRENT: 0 }, onReq: null, modeAtRespond: true };
  page.on('request', r => { const m = r.url().match(/\/api\/hero\/(WTI|BRENT)/); if (m) { reqs.push({ sym: m[1], t: (Date.now() - t0) / 1000 }); if (st.onReq) st.onReq(m[1]); } });
  if (opts.doc) await page.route(u => new URL(u).pathname === '/', route => route.fulfill({ status: 200, contentType: 'text/html', body: fs.readFileSync(opts.doc, 'utf8') }));
  if (opts.stub) {
    await page.route(/\/api\/hero\/(WTI|BRENT)/, async route => { try {
      const sym = route.request().url().match(/\/api\/hero\/(WTI|BRENT)/)[1];
      const n = ++st.n[sym];
      const p = st.plan ? st.plan(n, sym) : null;
      const dly = (p && p.delay !== undefined) ? p.delay : st.delayMs;
      if (dly) await sleep(dly);
      const rec = reqs.filter(x => x.sym === sym).pop();
      if (p && p.status && p.status !== 200) {
        if (rec) rec.status = p.status;
        const headers = typeof p.headers === 'function' ? p.headers() : (p.headers || {});
        if (rec && headers['Retry-After']) rec.ra = headers['Retry-After'];
        return await route.fulfill({ status: p.status, headers, contentType: 'text/plain', body: 'stub ' + p.status });
      }
      const resp = await route.fetch(); const j = await resp.json();
      if (st.mode) { j.mode = st.mode; if (st.mode === 'live') { j.delayed = false; j.updatedAt = new Date().toISOString(); } }
      if (st.delayed !== undefined) j.delayed = st.delayed;
      if (st.sessionDate) j.sessionDate = st.sessionDate;
      if (rec) rec.status = 200;
      return await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(j) });
    } catch (e) {} });
  }
  t0 = Date.now();
  await page.goto(H + '/', { waitUntil: 'domcontentloaded' });
  out.pills.initial = await pillSnap(page);
  const mark = p => out.phases.push({ phase: p, t: (Date.now() - t0) / 1000 });
  await script({ page, st, mark, setVis: v => setVis(page, v), snap: async k => { out.pills[k] = await pillSnap(page); }, note: s => out.notes.push(s), now: () => (Date.now() - t0) / 1000 });
  mark('end'); out.reqs = reqs;
  const summ = {};
  for (let i = 0; i < out.phases.length - 1; i++) {
    const p = out.phases[i], q = out.phases[i + 1], dur = q.t - p.t; if (p.phase.startsWith('_')) continue;
    for (const sym of ['WTI', 'BRENT']) {
      const rs = reqs.filter(r => r.sym === sym && r.t >= p.t && r.t < q.t); const ts = rs.map(r => r.t);
      summ[`${p.phase}:${sym}`] = { dur: +dur.toFixed(1), count: ts.length, perMin: +(ts.length / dur * 60).toFixed(2), firstAt: ts.length ? +(ts[0] - p.t).toFixed(1) : null, gaps: ts.slice(1).map((x, j) => +(x - ts[j]).toFixed(1)), st: rs.map(r => r.status) };
    }
  }
  out.summary = summ; fs.writeFileSync(OUT.replace(/\.txt$/, '') + '-' + name.split(' ')[0] + '.json', JSON.stringify(reqs)); await page.unrouteAll({ behavior: 'ignoreErrors' }).catch(() => {}); await ctx.close();
  let s = '\n### ' + name + '\n'; for (const n of out.notes) s += '  note: ' + n + '\n';
  for (const [k, v] of Object.entries(out.pills)) s += '  pill ' + k + ' ' + JSON.stringify(v[0]) + (JSON.stringify(v[0]) === JSON.stringify(v[1]) ? ' (BRENT same)' : ' BRENT=' + JSON.stringify(v[1])) + '\n';
  for (const [k, v] of Object.entries(summ)) s += '  ' + k + ' ' + JSON.stringify(v) + '\n';
  fs.appendFileSync(OUT, s); return out;
}
const nextReq = st => new Promise(res => { st.onReq = sym => { if (sym === 'WTI') { st.onReq = null; res(); } }; });
const left = () => ({ WTI: 0, BRENT: 0 });
(async () => {
  const browser = await chromium.launch();
  const S = [];
  // ---- v4 new
  S.push(scenario(browser, 'F1 cold first load 500 x5 then OK (live stub)', { stub: true, mode: 'live', plan: n => n <= 5 ? { status: 500 } : null }, async ({ mark, snap }) => {
    mark('from-load'); await sleep(840000); await snap('end');
  }));
  S.push(scenario(browser, 'F3 cold first load 404 x3 then OK (live stub)', { stub: true, mode: 'live', plan: n => n <= 3 ? { status: 404 } : null }, async ({ mark }) => {
    mark('from-load'); await sleep(520000);
  }));
  S.push(scenario(browser, 'F2b first load 500 x3 (2s latency), hide/show x2 during first fetch, hands-off', { stub: true, mode: 'live', delayMs: 2000, plan: n => n <= 3 ? { status: 500 } : null }, async ({ mark, setVis, note, now }) => {
    mark('from-load'); await sleep(500); await setVis('hidden'); await sleep(100); await setVis('visible'); await sleep(100); await setVis('hidden'); await sleep(100); await setVis('visible'); note('toggles done @' + now());
    await sleep(500000);
  }));
  S.push(scenario(browser, 'F2c first load slow (4s) 500, resume fetch fast (0.5s) 500, then 500 x2, then OK', { stub: true, mode: 'live', plan: n => n === 1 ? { status: 500, delay: 4000 } : (n <= 4 ? { status: 500, delay: 500 } : { delay: 500 }) }, async ({ mark, setVis, note, now }) => {
    mark('from-load'); await sleep(500); await setVis('hidden'); await sleep(100); await setVis('visible'); note('toggle during slow first fetch @' + now());
    await sleep(500000);
  }));
  S.push(scenario(browser, 'R4 429 RA 10 arrives while hidden (hidden mid-fetch, stay hidden 20s), then show', { stub: true, mode: 'live', delayMs: 2000 }, async ({ st, mark, setVis, note, now }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 429, headers: { 'Retry-After': '10' } }) : null;
    await sleep(5000); mark('live'); await sleep(20000); l = { WTI: 1, BRENT: 1 };
    await nextReq(st); await sleep(300); await setVis('hidden'); note('hidden mid-fetch (429 incoming) @' + now()); mark('hidden'); await sleep(20000);
    await setVis('visible'); note('shown @' + now()); mark('shown(expect immediate then 12s)'); await sleep(90000);
  }));
  S.push(scenario(browser, 'R5 429 RA 30, hide+show before RA fires', { stub: true, mode: 'live' }, async ({ st, mark, setVis, note, now }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 429, headers: { 'Retry-After': '30' } }) : null;
    await sleep(3000); mark('live'); await sleep(25000); l = { WTI: 1, BRENT: 1 }; await nextReq(st); await sleep(5000);
    await setVis('hidden'); await sleep(2000); await setVis('visible'); note('hide/show 5s into RA wait @' + now()); mark('after-show'); await sleep(90000);
  }));
  S.push(scenario(browser, 'R6 429 RA 10 x6 then OK (escalation check)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 429, headers: { 'Retry-After': '10' } }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); l = { WTI: 6, BRENT: 6 }; mark('429x6-then-ok'); await sleep(150000);
  }));
  S.push(scenario(browser, 'R7 429 RA 10 then 500 x2 then OK (mixed)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let q = { WTI: [], BRENT: [] }; st.plan = (n, s) => q[s].shift() || null;
    await sleep(3000); mark('ok'); await sleep(25000); for (const s of ['WTI', 'BRENT']) q[s] = [{ status: 429, headers: { 'Retry-After': '10' } }, { status: 500 }, { status: 500 }]; mark('429-500-500-then-ok'); await sleep(200000);
  }));
  // ---- 7. regressions
  S.push(scenario(browser, 'Y Real Yahoo: visible/hidden/visible', {}, async ({ mark, setVis, snap }) => {
    await sleep(3000); await snap('afterLoad'); mark('visible1'); await sleep(125000); await snap('afterPolls');
    await setVis('hidden'); mark('hidden'); await sleep(60000); await setVis('visible'); mark('visible2'); await sleep(65000); await snap('end');
  }));
  S.push(scenario(browser, 'T Live stub 2s latency: rapid toggles mid-fetch', { stub: true, mode: 'live', delayMs: 2000 }, async ({ st, mark, setVis, note, now }) => {
    await sleep(5000); mark('visible-baseline'); await sleep(50000); mark('_toggling');
    for (let k = 0; k < 3; k++) { await nextReq(st); await sleep(300); for (let i = 0; i < 4; i++) { await setVis('hidden'); await sleep(80); await setVis('visible'); await sleep(80); } note(`toggle burst ${k} @${now()}`); }
    mark('visible-after-toggles'); await sleep(90000);
    await nextReq(st); await sleep(300); await setVis('hidden'); await sleep(100); await setVis('visible'); await sleep(100); await setVis('hidden');
    mark('hidden-after-midfetch-toggle'); await sleep(60000); await setVis('visible'); mark('visible-final'); await sleep(60000);
  }));
  // ---- 1. mode transitions
  S.push(scenario(browser, 'M1 live->today-paused (no latency)', { stub: true, mode: 'live' }, async ({ st, mark, note, now }) => {
    await sleep(3000); mark('live'); await sleep(30000); st.mode = 'today-paused'; note('flip @' + now()); mark('after-flip(expect 60s)'); await sleep(150000);
  }));
  S.push(scenario(browser, 'M2 today-paused->live mid-fetch (2s latency)', { stub: true, mode: 'today-paused', delayMs: 2000 }, async ({ st, mark, note, now }) => {
    await sleep(3000); mark('today-paused'); await nextReq(st); st.mode = 'live'; note('flipped mid-fetch @' + now()); mark('after-flip(expect ~12s)'); await sleep(90000);
  }));
  S.push(scenario(browser, 'M3 transition during backoff: live, 500x2, then OK as today-paused', { stub: true, mode: 'live' }, async ({ st, mark, note, now }) => {
    let l = left(); st.plan = (n, s) => { if (l[s] > 0) { l[s]--; if (l[s] === 0) st.mode = 'today-paused'; return { status: 500 }; } return null; };
    await sleep(3000); mark('live'); await sleep(25000); l = { WTI: 2, BRENT: 2 }; mark('500x2-then-today-paused(expect 20,40,then 60s)'); await sleep(200000);
  }));
  S.push(scenario(browser, 'M3b transition during backoff: today-paused, 500x1, then OK as live', { stub: true, mode: 'today-paused' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => { if (l[s] > 0) { l[s]--; if (l[s] === 0) st.mode = 'live'; return { status: 500 }; } return null; };
    await sleep(3000); l = { WTI: 1, BRENT: 1 }; mark('500x1-then-live(expect 60,120,then 10s)'); await sleep(260000);
  }));
  S.push(scenario(browser, 'M4 transition while hidden: live, hide, flip today-paused, show', { stub: true, mode: 'live' }, async ({ st, mark, setVis }) => {
    await sleep(3000); mark('live'); await sleep(25000); await setVis('hidden'); st.mode = 'today-paused'; mark('hidden'); await sleep(30000); await setVis('visible'); mark('shown(expect immediate then 60s)'); await sleep(140000);
  }));
  S.push(scenario(browser, 'M5 transition while hidden: today-paused, hide, flip live, show', { stub: true, mode: 'today-paused' }, async ({ st, mark, setVis }) => {
    await sleep(3000); mark('today-paused'); await sleep(10000); await setVis('hidden'); st.mode = 'live'; mark('hidden'); await sleep(30000); await setVis('visible'); mark('shown(expect immediate then 10s)'); await sleep(60000);
  }));
  // ---- 2. first-load failure
  // ---- 4. Retry-After
  S.push(scenario(browser, 'R1 429 Retry-After 30 x3 then OK (live)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 429, headers: { 'Retry-After': '30' } }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); l = { WTI: 3, BRENT: 3 }; mark('429x3-then-ok(expect ~30,30,30 then 10)'); await sleep(260000);
  }));
  S.push(scenario(browser, 'R2 429 Retry-After HTTP-date +25s x1 then OK (live)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 429, headers: () => ({ 'Retry-After': new Date(Date.now() + 25000).toUTCString() }) }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); l = { WTI: 1, BRENT: 1 }; mark('429date-then-ok(expect ~25 then 10)'); await sleep(120000);
  }));
  S.push(scenario(browser, 'R3 503 Retry-After 30 x1 then OK (live)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 503, headers: { 'Retry-After': '30' } }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); l = { WTI: 1, BRENT: 1 }; mark('503-then-ok'); await sleep(110000);
  }));
  // ---- 2/5. 500 and 404 backoff
  S.push(scenario(browser, 'E1 500 x3 then OK (live)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 500 }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); l = { WTI: 3, BRENT: 3 }; mark('500x3-then-ok'); await sleep(220000);
  }));
  S.push(scenario(browser, 'E2 404 x3 then OK (live)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let l = left(); st.plan = (n, s) => l[s] > 0 ? (l[s]--, { status: 404 }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); l = { WTI: 3, BRENT: 3 }; mark('404x3-then-ok'); await sleep(220000);
  }));
  // ---- 6. badges
  S.push(scenario(browser, 'B1 SSR real Yahoo (JS off)', { js: false }, async () => {}));
  S.push(scenario(browser, 'B2 SSR fixture: stale Pyth price (JS off)', { js: false, doc: '/tmp/ssr4-pyth-stale.html' }, async () => {}));
  S.push(scenario(browser, 'B3 SSR fixture: fresh Pyth price (JS off)', { js: false, doc: '/tmp/ssr4-pyth-fresh.html' }, async () => {}));
  S.push(scenario(browser, 'B4 stale-Pyth SSR + JS, hero today-paused delayed=false', { doc: '/tmp/ssr4-pyth-stale.html', stub: true, mode: 'today-paused', delayed: false }, async ({ snap }) => { await sleep(4000); await snap('afterJS'); }));
  S.push(scenario(browser, 'B5 Yahoo SSR + JS, stub prior-session', { stub: true, mode: 'prior-session' }, async ({ st, snap }) => { st.sessionDate = '2026-10-16'; await sleep(4000); await snap('afterJS'); }));
  S.push(scenario(browser, 'B6 Yahoo SSR + JS, stub live', { stub: true, mode: 'live' }, async ({ snap }) => { await sleep(4000); await snap('afterJS'); await sleep(12000); await snap('afterPoll'); }));
  S.push(scenario(browser, 'B7 Yahoo SSR + JS, stub warming-up (empty bars)', { stub: true, plan: () => null, mode: 'warming-up' }, async ({ snap }) => { await sleep(4000); await snap('afterJS'); }));
  await Promise.all(S);
  await browser.close();
  fs.appendFileSync(OUT, '\nDONE\n');
})();
