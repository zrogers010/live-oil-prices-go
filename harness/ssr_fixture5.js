const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');
(async () => {
  const b = await chromium.launch();
  for (const n of ['yahoo', 'pyth-stale', 'pyth-fresh']) {
    // First, generate the HTML file by fetching the live page
    const ctxFetch = await b.newContext();
    const pageFetch = await ctxFetch.newPage();
    await pageFetch.goto('http://localhost:8080/', { waitUntil: 'load' });
    const html = await pageFetch.content();
    fs.writeFileSync(path.join(__dirname, `ssr5-${n}.html`), html, 'utf8');
    await ctxFetch.close();
    
    // Now test rendering with JS off
    const ctx = await b.newContext({ javaScriptEnabled: false }); 
    const page = await ctx.newPage();
    await page.route(u => new URL(u).pathname === '/', r => r.fulfill({ status: 200, contentType: 'text/html', body: html }));
    await page.goto('http://localhost:8080/', { waitUntil: 'load' });
    const s = await page.$$eval('.hero-live-pill', els => els.map(e => { const cs = getComputedStyle(e); const r = e.getBoundingClientRect(); return { text: e.textContent.replace(/\s+/g,' ').trim(), title: e.title, display: cs.display, visibility: cs.visibility, wh: Math.round(r.width)+'x'+Math.round(r.height), cls: e.className, color: cs.color }; }));
    await page.screenshot({ path: path.join(__dirname, `ssr5-${n}.png`), clip: await page.$eval('.hero-chart-card', e => { const r = e.getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: 80 }; }) });
    console.log(n, JSON.stringify(s[0]));
    await ctx.close();
  }
  await b.close();
})();
