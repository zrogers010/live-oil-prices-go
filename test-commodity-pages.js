const { chromium } = require('playwright');
const fs = require('fs');

(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
  
  try {
    console.log('=== TEST 1: Homepage Markets Section ===');
    await page.goto('http://localhost:8080', { waitUntil: 'networkidle', timeout: 30000 });
    await page.waitForTimeout(2000);
    
    // Scroll to markets section
    await page.evaluate(() => window.scrollTo(0, 900));
    await page.waitForTimeout(1000);
    await page.screenshot({ path: 'screenshots/commodity-01-homepage-markets.png' });
    console.log('✓ Homepage markets screenshot saved');
    
    console.log('\n=== TEST 2: Click WTI Crude Oil Card ===');
    const wtiCard = page.locator('text=WTI Crude Oil').first();
    const cardCount = await wtiCard.count();
    console.log('WTI cards found:', cardCount);
    
    if (cardCount > 0) {
      // Try clicking the parent container
      const cardParent = wtiCard.locator('..');
      await cardParent.click();
      console.log('Clicked WTI card');
      
      // Wait for navigation
      await page.waitForTimeout(2000);
      
      const currentUrl = page.url();
      console.log('Current URL:', currentUrl);
      
      console.log('\n=== TEST 3: WTI Commodity Detail Page ===');
      await page.screenshot({ path: 'screenshots/commodity-02-wti-page.png' });
      
      // Check page elements
      const pageInfo = await page.evaluate(() => {
        return {
          url: window.location.href,
          title: document.title,
          hasBreadcrumb: !!document.querySelector('nav[aria-label="breadcrumb"], .breadcrumb'),
          breadcrumbText: document.body.textContent.includes('Markets') && document.body.textContent.includes('WTI'),
          hasPrice: !!document.querySelector('[class*="price"]'),
          hasChart: !!document.querySelector('canvas'),
          bodyText: document.body.textContent.substring(0, 500)
        };
      });
      console.log('Page info:', JSON.stringify(pageInfo, null, 2));
      
      console.log('\n=== TEST 4: Click 1Y Timeframe Button ===');
      await page.waitForTimeout(1000);
      
      // Find and click 1Y button
      const oneYearButton = page.locator('button:has-text("1Y")').first();
      const buttonCount = await oneYearButton.count();
      console.log('1Y buttons found:', buttonCount);
      
      if (buttonCount > 0) {
        await oneYearButton.click();
        console.log('Clicked 1Y button');
        await page.waitForTimeout(1500);
        await page.screenshot({ path: 'screenshots/commodity-03-wti-1year.png' });
        console.log('✓ 1Y timeframe screenshot saved');
      }
      
      console.log('\n=== TEST 5: Navigate Back to Homepage ===');
      // Try clicking breadcrumb or logo
      const breadcrumbLink = page.locator('a:has-text("Markets")').first();
      const breadcrumbCount = await breadcrumbLink.count();
      
      if (breadcrumbCount > 0) {
        await breadcrumbLink.click();
        console.log('Clicked Markets breadcrumb');
      } else {
        // Try logo
        const logo = page.locator('a[href="/"]').first();
        if (await logo.count() > 0) {
          await logo.click();
          console.log('Clicked logo');
        } else {
          await page.goto('http://localhost:8080');
          console.log('Navigated to homepage directly');
        }
      }
      
      await page.waitForTimeout(2000);
      console.log('Back at URL:', page.url());
      
      console.log('\n=== TEST 6: Click Natural Gas from Table ===');
      await page.evaluate(() => window.scrollTo(0, 1400));
      await page.waitForTimeout(1000);
      
      // Find and click Natural Gas table row
      await page.evaluate(() => {
        const rows = Array.from(document.querySelectorAll('table tbody tr'));
        const gasRow = rows.find(row => row.textContent.includes('Natural Gas'));
        if (gasRow) {
          console.log('Found Natural Gas row');
          gasRow.click();
        }
      });
      
      await page.waitForTimeout(2000);
      console.log('After table click, URL:', page.url());
      
      console.log('\n=== TEST 7: Natural Gas Commodity Page ===');
      await page.screenshot({ path: 'screenshots/commodity-04-natgas-page.png' });
      
      const natgasInfo = await page.evaluate(() => {
        return {
          url: window.location.href,
          hasNaturalGasText: document.body.textContent.includes('Natural Gas'),
          hasChart: !!document.querySelector('canvas')
        };
      });
      console.log('Natural Gas page info:', JSON.stringify(natgasInfo, null, 2));
      
      console.log('\n=== TEST 8: Other Energy Markets Section ===');
      await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight - 1000));
      await page.waitForTimeout(1000);
      await page.screenshot({ path: 'screenshots/commodity-05-other-markets.png' });
      
      // Try to find and click another commodity card
      const otherCommodityClicked = await page.evaluate(() => {
        const cards = Array.from(document.querySelectorAll('[class*="card"], div[class*="commodity"]'));
        const heatingCard = cards.find(card => 
          card.textContent.includes('Heating Oil') || 
          card.textContent.includes('Brent') ||
          card.textContent.includes('RBOB')
        );
        
        if (heatingCard && heatingCard.tagName === 'A') {
          heatingCard.click();
          return { clicked: true, commodity: heatingCard.textContent.substring(0, 50) };
        } else if (heatingCard) {
          const link = heatingCard.querySelector('a');
          if (link) {
            link.click();
            return { clicked: true, commodity: link.textContent.substring(0, 50) };
          }
        }
        
        return { clicked: false };
      });
      
      console.log('Other commodity click:', JSON.stringify(otherCommodityClicked, null, 2));
      
      if (otherCommodityClicked.clicked) {
        await page.waitForTimeout(2000);
        console.log('Navigated to:', page.url());
        await page.screenshot({ path: 'screenshots/commodity-06-other-commodity.png' });
      }
    }
    
    console.log('\n✅ All commodity page tests completed');
    
  } catch (error) {
    console.error('Error during tests:', error.message);
    await page.screenshot({ path: 'screenshots/commodity-error.png' });
  } finally {
    await browser.close();
  }
})();
