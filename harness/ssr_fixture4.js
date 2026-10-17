const { chromium } = require('/workspace/lop-review/head4/node_modules/playwright');
const fs = require('fs');
(async () => {
  const b = await chromium.launch();
  for (const n of ['yahoo', 'pyth-stale', 'pyth-fresh']) {
    const ctx = await b.newContext({ javaScriptEnabled: false }); const page = await ctx.newPage();
    await page.route(u => new URL(u).pathname === '/', r => r.fulfill({ status: 200, contentType: 'text/html', body: fs.readFileSync(`/tmp/ssr4-${n}.html`, 'utf8') }));
    await page.goto('http://localhost:8145/', { waitUntil: 'load' });
    const s = await page.$$eval('.hero-live-pill', els => els.map(e => { const cs = getComputedStyle(e); const r = e.getBoundingClientRect(); return { text: e.textContent.replace(/\s+/g,' ').trim(), title: e.title, display: cs.display, visibility: cs.visibility, wh: Math.round(r.width)+'x'+Math.round(r.height), cls: e.className, color: cs.color }; }));
    await page.screenshot({ path: `/workspace/lop-review/harness/ssr4-${n}.png`, clip: await page.$eval('.hero-chart-card', e => { const r = e.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: 80 }; }) });
    console.log(n, JSON.stringify(s[0]));
    await ctx.close();
  }
  await b.close();
})();
