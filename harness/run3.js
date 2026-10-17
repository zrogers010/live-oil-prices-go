// PR42 v2 harness. Only /api/hero/* is stubbed (page.route) where a scenario sets `stub`.
const { chromium } = require('/workspace/lop-review/head2/node_modules/playwright');
const fs = require('fs');
const H = process.env.TARGET || 'http://localhost:8143';
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
  return { text: e.textContent.replace(/\s+/g, ' ').trim(), hiddenAttr: e.hasAttribute('hidden'), style: e.getAttribute('style'), display: cs.display, visibility: cs.visibility, opacity: cs.opacity, wh: Math.round(r.width) + 'x' + Math.round(r.height), cls: e.className, dotAnim: d ? getComputedStyle(d).animationName : null };
}));
async function scenario(browser, name, opts, script) {
  if (ONLY && !ONLY.test(name)) return null;
  const ctx = await browser.newContext({ javaScriptEnabled: opts.js !== false });
  const page = await ctx.newPage();
  await page.addInitScript(VIS_INIT);
  const reqs = []; let t0 = Date.now(); const out = { name, phases: [], pills: {}, notes: [] };
  const st = { mode: opts.mode, delayMs: opts.delayMs || 0, plan: opts.plan || null, n: { WTI: 0, BRENT: 0 }, onReq: null, modeAtRespond: false };
  page.on('request', r => { const m = r.url().match(/\/api\/hero\/(WTI|BRENT)/); if (m) { reqs.push({ sym: m[1], t: (Date.now() - t0) / 1000 }); if (st.onReq) st.onReq(m[1]); } });
  if (opts.stub) {
    await page.route(/\/api\/hero\/(WTI|BRENT)/, async route => { try {
      const sym = route.request().url().match(/\/api\/hero\/(WTI|BRENT)/)[1];
      const n = ++st.n[sym]; const modeAtReq = st.mode;
      if (st.delayMs) await sleep(st.delayMs);
      const p = st.plan ? st.plan(n, sym) : null;
      const rec = reqs.filter(x => x.sym === sym).pop();
      if (p && p.status !== 200) { if (rec) rec.status = p.status; return route.fulfill({ status: p.status, headers: p.headers || {}, contentType: 'text/plain', body: 'stub ' + p.status }); }
      const resp = await route.fetch(); const j = await resp.json();
      const m2 = st.modeAtRespond ? st.mode : modeAtReq;
      if (m2) { j.mode = m2; if (m2 === 'live') { j.delayed = false; j.updatedAt = new Date().toISOString(); } }
      if (rec) rec.status = 200;
      return await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(j) });
    } catch (e) { /* page closed */ } });
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
  out.summary = summ; await page.unrouteAll({ behavior: 'ignoreErrors' }).catch(()=>{}); await ctx.close();
  fs.appendFileSync('/workspace/lop-review/harness/out3.txt', fmt(out)); return out;
}
function fmt(r) {
  let s = '\n### ' + r.name + '\n'; for (const n of r.notes) s += '  note: ' + n + '\n';
  for (const [k, v] of Object.entries(r.pills)) s += '  pill ' + k + ' ' + JSON.stringify(v) + '\n';
  for (const [k, v] of Object.entries(r.summary)) s += '  ' + k + ' ' + JSON.stringify(v) + '\n';
  return s;
}
const nextReq = st => new Promise(res => { st.onReq = sym => { if (sym === 'WTI') { st.onReq = null; res(); } }; });
(async () => {
  const browser = await chromium.launch();
  const S = [];
  S.push(scenario(browser, 'R1 Live stub: 500 x2 per symbol then OK (recovery)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let left = { WTI: 0, BRENT: 0 }; st.plan = (n, sym) => left[sym] > 0 ? (left[sym]--, { status: 500 }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); left = { WTI: 2, BRENT: 2 }; mark('500x2-then-ok'); await sleep(120000);
  }));
  S.push(scenario(browser, 'R2 Live stub: 404 x2 per symbol then OK (recovery)', { stub: true, mode: 'live' }, async ({ st, mark }) => {
    let left = { WTI: 0, BRENT: 0 }; st.plan = (n, sym) => left[sym] > 0 ? (left[sym]--, { status: 404 }) : null;
    await sleep(3000); mark('ok'); await sleep(25000); left = { WTI: 2, BRENT: 2 }; mark('404x2-then-ok'); await sleep(120000);
  }));
  S.push(scenario(browser, 'R3 Mode change, no latency: live->today-paused', { stub: true, mode: 'live' }, async ({ st, mark, note, now }) => {
    await sleep(3000); mark('live'); await sleep(30000); st.mode = 'today-paused'; note('flip @' + now()); mark('after-flip(expect 60s)'); await sleep(120000);
  }));
  S.push(scenario(browser, 'R4 Delayed-mode 500s (60s base) backoff', { stub: true, mode: 'today-paused' }, async ({ st, mark }) => {
    let fail = false; st.plan = () => fail ? { status: 500 } : null;
    await sleep(3000); fail = true; mark('errors500'); await sleep(150000);
  }));
  const results = (await Promise.all(S)).filter(Boolean);
  await browser.close();
  fs.writeFileSync('/workspace/lop-review/harness/results3.json', JSON.stringify(results, null, 1));
  fs.appendFileSync('/workspace/lop-review/harness/out3.txt', '\nDONE\n');
})();
