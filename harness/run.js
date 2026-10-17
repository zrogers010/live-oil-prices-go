// Test harness for PR42 review. Stubs ONLY /api/hero/* responses via Playwright routing where noted.
const { chromium } = require('/workspace/lop-review/head/node_modules/playwright');
const sleep = ms => new Promise(r => setTimeout(r, ms));
const VIS_INIT = () => {
  window.__vis = 'visible';
  Object.defineProperty(Document.prototype, 'visibilityState', { get() { return window.__vis; }, configurable: true });
  Object.defineProperty(Document.prototype, 'hidden', { get() { return window.__vis === 'hidden'; }, configurable: true });
};
async function setVis(page, v) {
  await page.evaluate(v => { window.__vis = v; document.dispatchEvent(new Event('visibilitychange')); }, v);
}
async function pill(page) {
  return page.$$eval('[data-role="live-pill"]', els => els.filter(e => e.classList.contains('hero-live-pill')).map(e => ({
    text: e.textContent.replace(/\s+/g, ' ').trim(), visible: getComputedStyle(e).display !== 'none' })));
}
async function scenario(browser, name, base, opts, script) {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.addInitScript(VIS_INIT);
  const reqs = []; let t0 = 0; let phase = 'load';
  page.on('request', r => { const m = r.url().match(/\/api\/hero\/(WTI|BRENT)/); if (m) reqs.push({ sym: m[1], t: (Date.now() - t0) / 1000, phase }); });
  const state = { fail: false, mode: opts.mode, delayMs: opts.delayMs || 0, n: 0 };
  if (opts.stub) {
    await page.route(/\/api\/hero\/(WTI|BRENT)/, async route => {
      state.n++;
      if (state.delayMs) await sleep(state.delayMs);
      if (state.fail) return route.fulfill({ status: 500, contentType: 'text/plain', body: 'stub error' });
      const resp = await route.fetch();
      const j = await resp.json();
      if (state.mode) { j.mode = state.mode; if (state.mode === 'live') { j.delayed = false; j.updatedAt = new Date().toISOString(); } }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(j) });
    });
  }
  t0 = Date.now();
  await page.goto(base + '/', { waitUntil: 'domcontentloaded' });
  const ssr = await page.evaluate(() => [...document.querySelectorAll('.hero-live-pill')].map(e => e.getAttribute('style')));
  const out = { name, ssrPillStyle: ssr, pills: {}, phases: [] };
  const mark = async (p) => { phase = p; out.phases.push({ phase: p, t: (Date.now() - t0) / 1000 }); };
  await script({ page, setVis: v => setVis(page, v), mark, state, sleep, pillSnap: async k => { out.pills[k] = await pill(page); } });
  await mark('end');
  out.reqs = reqs;
  // summarize per phase
  const summ = {};
  for (let i = 0; i < out.phases.length - 1; i++) {
    const p = out.phases[i], q = out.phases[i + 1], dur = q.t - p.t;
    for (const sym of ['WTI', 'BRENT']) {
      const ts = reqs.filter(r => r.sym === sym && r.t >= p.t && r.t < q.t).map(r => r.t);
      const gaps = ts.slice(1).map((x, j) => +(x - ts[j]).toFixed(1));
      summ[`${p.phase}:${sym}`] = { durS: +dur.toFixed(1), count: ts.length, perMin: +(ts.length / dur * 60).toFixed(1), gaps };
    }
  }
  out.summary = summ;
  await ctx.close();
  return out;
}
(async () => {
  const browser = await chromium.launch();
  const H = 'http://localhost:8142', B = 'http://localhost:8141';
  const results = await Promise.all([
    scenario(browser, 'HEAD real backend (Yahoo delayed, today-paused)', H, {}, async ({ mark, sleep, setVis, pillSnap }) => {
      await sleep(3000); await pillSnap('afterLoad');
      await mark('visible'); await sleep(180000);
    }),
    scenario(browser, 'HEAD stubbed mode=live: visible/hidden/visible', H, { stub: true, mode: 'live' }, async ({ mark, sleep, setVis, pillSnap }) => {
      await sleep(3000); await pillSnap('afterLoad');
      await mark('visible1'); await sleep(60000);
      await setVis('hidden'); await mark('hidden'); await sleep(60000);
      await setVis('visible'); await mark('visible2'); await sleep(60000);
    }),
    scenario(browser, 'HEAD stubbed live then 500s', H, { stub: true, mode: 'live' }, async ({ mark, sleep, state }) => {
      await sleep(3000);
      await mark('ok'); await sleep(30000);
      state.fail = true; await mark('errors500'); await sleep(150000);
    }),
    scenario(browser, 'HEAD stubbed delayed(today-paused) then 500s', H, { stub: true }, async ({ mark, sleep, state }) => {
      await sleep(3000); state.fail = true; await mark('errors500'); await sleep(180000);
    }),
    scenario(browser, 'HEAD resume race: live stub, 1.5s latency, rapid toggles', H, { stub: true, mode: 'live', delayMs: 1500 }, async ({ mark, sleep, setVis }) => {
      await sleep(5000); await mark('visible-before'); await sleep(60000);
      // hidden->visible->hidden->visible within 300ms, while resume fetches are in flight
      await setVis('hidden'); await sleep(100); await setVis('visible'); await sleep(100); await setVis('hidden'); await sleep(100); await setVis('visible');
      await mark('visible-after-rapid-toggles'); await sleep(60000);
      // hidden while a resume fetch is in flight -> should not poll while hidden
      await setVis('hidden'); await sleep(100); await setVis('visible'); await sleep(200); await setVis('hidden');
      await mark('hidden-after-toggle'); await sleep(60000);
    }),
    scenario(browser, 'BASE real backend: visible/hidden', B, {}, async ({ mark, sleep, setVis, pillSnap }) => {
      await sleep(3000); await pillSnap('afterLoad');
      await mark('visible'); await sleep(90000);
      await setVis('hidden'); await mark('hidden'); await sleep(90000);
    }),
    scenario(browser, 'BASE stubbed live then 500s', B, { stub: true, mode: 'live' }, async ({ mark, sleep, state, pillSnap }) => {
      await sleep(3000); await pillSnap('afterLoad');
      await mark('ok'); await sleep(30000); state.fail = true; await mark('errors500'); await sleep(60000);
    }),
  ]);
  await browser.close();
  require('fs').writeFileSync('/workspace/lop-review/harness/results.json', JSON.stringify(results, null, 1));
  for (const r of results) { console.log('\n### ' + r.name); console.log('SSR pill style:', JSON.stringify(r.ssrPillStyle)); console.log('pills:', JSON.stringify(r.pills)); for (const [k, v] of Object.entries(r.summary)) console.log(k, JSON.stringify({ dur: v.durS, count: v.count, perMin: v.perMin, gaps: v.gaps.length > 20 ? v.gaps.slice(0, 20).concat(['...']) : v.gaps })); }
})();
