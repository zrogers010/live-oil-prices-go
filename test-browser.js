const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch();
  const context = await browser.newContext();
  
  console.log('=== ONLINE TESTS ===\n');
  
  // Test home page online
  const homePage = await context.newPage();
  await homePage.goto('http://localhost:8080/');
  await homePage.waitForTimeout(3000);
  
  const heroCanvases = await homePage.locator('.hero-chart-card canvas').count();
  const homeText = await homePage.content();
  const homeHasZero = homeText.includes('$0.00');
  const tagline = await homePage.locator('#heroTaglineAccent').textContent();
  
  console.log('Home page (online):');
  console.log(`  Hero canvas count: ${heroCanvases}`);
  console.log(`  Tagline: ${tagline}`);
  console.log(`  Contains $0.00: ${homeHasZero}`);
  console.log('');
  
  // Test WTI page online
  const wtiPage = await context.newPage();
  await wtiPage.goto('http://localhost:8080/commodity/WTI');
  await wtiPage.waitForTimeout(3000);
  
  const wtiPrice = await wtiPage.locator('#detailPrice').textContent();
  const wtiText = await wtiPage.content();
  const wtiHasZero = wtiText.includes('$0.00');
  
  console.log('WTI page (online):');
  console.log(`  Header price: ${wtiPrice}`);
  console.log(`  Contains $0.00: ${wtiHasZero}`);
  console.log('');
  
  // Test OPEC page online (no Yahoo data)
  const opecPage = await context.newPage();
  await opecPage.goto('http://localhost:8080/commodity/OPEC');
  await opecPage.waitForTimeout(3000);
  
  const opecPrice = await opecPage.locator('#detailPrice').textContent();
  const opecText = await opecPage.content();
  const opecHasZero = opecText.includes('$0.00');
  
  console.log('OPEC page (online, no Yahoo data):');
  console.log(`  Header price: ${opecPrice}`);
  console.log(`  Contains $0.00: ${opecHasZero}`);
  console.log('');
  
  await context.close();
  
  // OFFLINE TESTS
  console.log('\n=== OFFLINE TESTS ===\n');
  
  const offlineContext = await browser.newContext({
    proxy: {
      server: 'http://127.0.0.1:9'
    }
  });
  
  // Test home page offline
  const offlineHome = await offlineContext.newPage();
  try {
    await offlineHome.goto('http://localhost:8080/', { timeout: 5000 });
    await offlineHome.waitForTimeout(3000);
    
    const offlineHeroCanvases = await offlineHome.locator('.hero-chart-card canvas').count();
    const offlineHomeText = await offlineHome.content();
    const offlineHomeHasZero = offlineHomeText.includes('$0.00');
    const offlineTagline = await offlineHome.locator('#heroTaglineAccent').textContent();
    
    console.log('Home page (offline):');
    console.log(`  Hero canvas count: ${offlineHeroCanvases}`);
    console.log(`  Tagline: ${offlineTagline}`);
    console.log(`  Contains $0.00: ${offlineHomeHasZero}`);
    console.log('');
  } catch (e) {
    console.log('Home page (offline): Navigation failed (expected with bad proxy)');
  }
  
  // Test WTI page offline
  const offlineWTI = await offlineContext.newPage();
  try {
    await offlineWTI.goto('http://localhost:8080/commodity/WTI', { timeout: 5000 });
    await offlineWTI.waitForTimeout(3000);
    
    const offlineWTIPrice = await offlineWTI.locator('#detailPrice').textContent();
    const offlineWTIText = await offlineWTI.content();
    const offlineWTIHasZero = offlineWTIText.includes('$0.00');
    
    console.log('WTI page (offline):');
    console.log(`  Header price: ${offlineWTIPrice}`);
    console.log(`  Contains $0.00: ${offlineWTIHasZero}`);
    console.log('');
  } catch (e) {
    console.log('WTI page (offline): Navigation failed (expected with bad proxy)');
  }
  
  await offlineContext.close();
  await browser.close();
  
  console.log('✓ Playwright tests complete');
})();
