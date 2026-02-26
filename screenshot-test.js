const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

(async () => {
  const browser = await chromium.launch();
  const screenshotsDir = path.join(__dirname, 'screenshots');
  
  if (!fs.existsSync(screenshotsDir)) {
    fs.mkdirSync(screenshotsDir);
  }

  console.log('Testing desktop view (1920x1080)...');
  const page = await browser.newPage({
    viewport: { width: 1920, height: 1080 }
  });

  await page.goto('http://localhost:8080', { waitUntil: 'networkidle' });
  
  // Wait a bit for any animations or dynamic content
  await page.waitForTimeout(2000);

  // 1. Full page screenshot
  console.log('Taking full page screenshot...');
  await page.screenshot({ 
    path: path.join(screenshotsDir, '01-full-page.png'),
    fullPage: true 
  });

  // 2. Navigation bar and price ticker at the top
  console.log('Capturing navigation bar and price ticker...');
  await page.screenshot({ 
    path: path.join(screenshotsDir, '02-nav-and-ticker.png')
  });

  // 3. Hero section
  console.log('Capturing hero section...');
  const heroSection = await page.locator('section').first();
  if (await heroSection.count() > 0) {
    await heroSection.screenshot({ 
      path: path.join(screenshotsDir, '03-hero-section.png')
    });
  }

  // 4. Live price cards section
  console.log('Scrolling to live price cards...');
  await page.evaluate(() => window.scrollTo(0, 600));
  await page.waitForTimeout(500);
  await page.screenshot({ 
    path: path.join(screenshotsDir, '04-price-cards.png')
  });

  // 5. Interactive chart section
  console.log('Scrolling to chart section...');
  await page.evaluate(() => window.scrollTo(0, 1200));
  await page.waitForTimeout(1000);
  await page.screenshot({ 
    path: path.join(screenshotsDir, '05-chart-section.png')
  });

  // 6. AI analysis section
  console.log('Scrolling to AI analysis...');
  await page.evaluate(() => window.scrollTo(0, 2000));
  await page.waitForTimeout(500);
  await page.screenshot({ 
    path: path.join(screenshotsDir, '06-ai-analysis.png')
  });

  // 7. News section
  console.log('Scrolling to news section...');
  await page.evaluate(() => window.scrollTo(0, 2800));
  await page.waitForTimeout(500);
  await page.screenshot({ 
    path: path.join(screenshotsDir, '07-news-section.png')
  });

  // 8. Pricing section
  console.log('Scrolling to pricing section...');
  await page.evaluate(() => window.scrollTo(0, 3600));
  await page.waitForTimeout(500);
  await page.screenshot({ 
    path: path.join(screenshotsDir, '08-pricing-section.png')
  });

  // 9. Footer
  console.log('Scrolling to footer...');
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await page.waitForTimeout(500);
  await page.screenshot({ 
    path: path.join(screenshotsDir, '09-footer.png')
  });

  await page.close();

  // Mobile view test (375px width - iPhone size)
  console.log('\nTesting mobile view (375x667)...');
  const mobilePage = await browser.newPage({
    viewport: { width: 375, height: 667 }
  });

  await mobilePage.goto('http://localhost:8080', { waitUntil: 'networkidle' });
  await mobilePage.waitForTimeout(2000);

  console.log('Taking mobile full page screenshot...');
  await mobilePage.screenshot({ 
    path: path.join(screenshotsDir, '10-mobile-full-page.png'),
    fullPage: true 
  });

  console.log('Taking mobile viewport screenshot...');
  await mobilePage.screenshot({ 
    path: path.join(screenshotsDir, '11-mobile-viewport.png')
  });

  await mobilePage.close();
  await browser.close();

  console.log('\n✅ All screenshots saved to:', screenshotsDir);
  console.log('\nScreenshots taken:');
  const files = fs.readdirSync(screenshotsDir).sort();
  files.forEach(file => console.log(`  - ${file}`));
})();
