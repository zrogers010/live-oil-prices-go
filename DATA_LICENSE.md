# Data Licensing & Attribution

This document explains the data sources used by Live Oil Prices and their licensing terms.

## Data Sources

### 1. Pyth Network (Real-Time Streaming Data)

**Usage:** Real-time WTI crude oil spot prices (1-minute candle data)

**License:** Creative Commons Attribution 4.0 International (CC BY 4.0)

**Terms:**
- Free to use with attribution
- Commercial use permitted
- Modifications allowed
- Must give appropriate credit

**Attribution:**
The site footer includes: "Real-time WTI price data via Pyth Network, licensed under CC BY 4.0"

**License Details:** https://creativecommons.org/licenses/by/4.0/

**Redistribution Notes:**
- ✅ **Permitted:** Displaying prices, creating charts, building applications
- ✅ **Permitted:** Redistributing data with proper attribution
- ⚠️ **Required:** Must include attribution to Pyth Network
- ⚠️ **Required:** Must indicate if modifications were made

**Pyth Network Resources:**
- Website: https://www.pyth.network/
- Data License: https://www.pyth.network/terms-of-use
- Privacy Policy: https://www.pyth.network/privacy-policy

### 2. Yahoo Finance (Historical & Daily Data)

**Usage:** Daily OHLCV (Open, High, Low, Close, Volume) data for multiple commodities

**License:** Yahoo's Terms of Service (personal/non-commercial use)

**Terms:**
- Free for personal and non-commercial use
- No redistribution of bulk data
- Cannot create competing financial data products
- Reasonable display of quotes is permitted

**Attribution:**
The site footer includes: "Other commodities via Yahoo Finance (15-min delayed)"

**Redistribution Notes for Ad-Supported Sites:**
- ✅ **Permitted:** Displaying individual price quotes on ad-supported websites
- ✅ **Permitted:** Creating charts from Yahoo Finance data for site users
- ❌ **Not Permitted:** Bulk data export or API reselling
- ❌ **Not Permitted:** Creating competing financial data feeds
- ⚠️ **Advisory:** Ad-supported websites displaying Yahoo Finance data should review Yahoo's terms

**Important for Running Ads:**
Yahoo Finance's terms generally allow display of quotes on websites, including ad-supported sites, as long as you're not primarily monetizing the data itself. Since Live Oil Prices is an **editorial site** providing market analysis, news, and charts (not a pure data reseller), displaying Yahoo Finance data alongside advertising is typically within acceptable use.

**Recommendation:** If running ads and using Yahoo Finance data at scale, consider:
1. Reviewing Yahoo Finance's current Terms of Service
2. Contacting Yahoo Finance for clarification if needed
3. Consider switching to a commercial data provider (e.g., IEX Cloud, Alpha Vantage, Twelve Data) for long-term commercial operations

**Yahoo Finance Resources:**
- Terms of Service: https://legal.yahoo.com/us/en/yahoo/terms/otos/index.html
- API Documentation: (No official public API; site scraping is against ToS)

### 3. EIA (U.S. Energy Information Administration)

**Usage:** Consensus forecasts (Short-Term Energy Outlook) when `EIA_API_KEY` is set

**License:** U.S. Government Public Domain

**Terms:**
- All EIA data is in the public domain
- No attribution required (but provided as best practice)
- Free for any use, including commercial

**Attribution:**
Forecasts include source links to EIA STEO releases

**Redistribution Notes:**
- ✅ **Permitted:** All uses, including commercial redistribution
- ✅ **No restrictions:** Public domain, no permission needed

**EIA Resources:**
- API Key Registration: https://www.eia.gov/opendata/register.php
- Terms of Service: https://www.eia.gov/about/copyrights_reuse.php
- STEO Data: https://www.eia.gov/outlooks/steo/

## Running Ads: Licensing Summary

| Data Source | Commercial Use | Ads Permitted | Notes |
|-------------|----------------|---------------|-------|
| **Pyth Network** | ✅ Yes | ✅ Yes | Requires CC BY 4.0 attribution |
| **Yahoo Finance** | ⚠️ Review ToS | ⚠️ Likely OK | Editorial use typically fine; bulk resale is not |
| **EIA** | ✅ Yes | ✅ Yes | Public domain, no restrictions |
| **Generated/Estimated** | ✅ Yes | ✅ Yes | Model-generated forecasts and synthetic data |

### Key Considerations for Ads:

1. **Pyth Network:** Fully licensed for commercial use. Attribution is included.

2. **Yahoo Finance:** Generally permits displaying quotes on websites (including ad-supported ones) as long as the site provides editorial value and isn't solely a data redistribution service. Live Oil Prices includes analysis, news, forecasts, and charts, making it an editorial site rather than a pure data feed.

3. **EIA:** Public domain, no concerns.

4. **Generated Data:** Forecast models, synthetic fallback data, and analysis are original work.

### Best Practices:

- ✅ **Attribution is present:** Pyth Network is credited in the footer
- ✅ **Editorial content:** The site provides analysis, not just raw data
- ✅ **No bulk export:** Users cannot download raw data files
- ✅ **Privacy policy:** Discloses data sources and practices
- ✅ **Public domain prioritized:** EIA data is used where possible

## Future Considerations

If the site scales significantly or Yahoo Finance objects to the usage, consider switching to:

- **IEX Cloud** (https://iexcloud.io) - Commercial data API with clear pricing
- **Alpha Vantage** (https://www.alphavantage.co) - Free tier + commercial plans
- **Twelve Data** (https://twelvedata.com) - Real-time and historical financial data
- **Polygon.io** (https://polygon.io) - Financial market data APIs

These providers explicitly support commercial use and advertising-supported websites.

## Legal Disclaimer

**This document provides general guidance and is not legal advice.** The owner/operator of liveoilprices.com should:

1. Review each data provider's current Terms of Service
2. Consult with legal counsel if operating at significant scale
3. Monitor for any updates to data licensing terms
4. Consider commercial data providers for long-term commercial operations

Last updated: October 17, 2026
