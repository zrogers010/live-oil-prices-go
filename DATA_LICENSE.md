# Data Licensing & Attribution

This document explains the data sources used by Live Oil Prices and their licensing terms.

## Data Sources

### 1. Pyth Network (Real-Time Streaming Data)

**Usage:** Real-time WTI and Brent crude oil spot prices (1-minute candle data)

**License:** Paid subscription required via Pyth Pro

**Important:** Pyth Network data is **NOT** open source or CC BY 4.0 licensed for production use. Display rights require a paid Pyth Pro subscription:

- **Pyth Pro Starter:** $500/month minimum, crypto-only assets
- **Pyth Pro (Commodities):** Starting from $2,500/month for commodities like WTI and Brent
- No redistribution rights under Douro Labs Subscription Terms
- API key required for production use

**Current Status:**
The production instance is receiving `hermes status 401: unauthorized` errors, indicating no valid PYTH_API_KEY is configured.

**Setup Requirements:**
```bash
# Required environment variables
export PYTH_API_KEY="your_api_key_here"
export PYTH_HERMES_BASE_URL="https://hermes.pyth.network"
```

**To Subscribe:**
1. Visit https://www.pyth.network/price-feeds
2. Choose Pyth Pro plan (commodities require Pro tier, $2,500/month+)
3. Obtain API key and configure Hermes endpoint
4. Set environment variables in `/etc/liveoilprices/env`

**Attribution:**
While using Pyth data, attribution is required. The site footer includes: "Real-time WTI price data via Pyth Network"

**Redistribution Notes:**
- Display on your own site with paid subscription
- No bulk redistribution or resale permitted
- Commercial use requires Pro subscription
- Terms: https://pyth.network/terms-of-use

**Pyth Network Resources:**
- Website: https://www.pyth.network/
- Pricing: https://www.pyth.network/price-feeds
- Developer Docs: https://docs.pyth.network/

### 2. Yahoo Finance (Historical & Daily Data)

**Usage:** Daily OHLCV (Open, High, Low, Close, Volume) data for multiple commodities

**License:** Unlicensed / Personal Non-Commercial Use Only

**Important:** Yahoo Finance's unofficial API endpoints are:
- **NOT licensed for commercial use**
- Limited to personal, non-commercial use only
- Explicitly prohibit automated access in Yahoo's Terms of Service
- Not cleared for ad-supported websites

**Redistribution Notes:**
- Displaying quotes on ad-supported sites violates Yahoo's ToS
- No bulk data export or API reselling
- Automated scraping is against terms
- **Not suitable for production with AdSense enabled**

**Recommendation:**
Replace Yahoo Finance with a licensed commercial data provider **before** enabling AdSense:

**Recommended Alternatives:**
- **IEX Cloud** (https://iexcloud.io) - Clear commercial licensing, API-based
- **Alpha Vantage** (https://www.alphavantage.co) - Free tier + commercial plans
- **Twelve Data** (https://twelvedata.com) - Real-time and historical financial data
- **Polygon.io** (https://polygon.io) - Financial market data APIs with commercial rights
- **Quandl/Nasdaq Data Link** (https://data.nasdaq.com) - Historical and alternative data

**Current Usage:**
The site footer states: "Other commodities via Yahoo Finance (15-min delayed)"

**Yahoo Finance Resources:**
- Terms of Service: https://legal.yahoo.com/us/en/yahoo/terms/otos/index.html
- No official public API documentation

### 3. EIA (U.S. Energy Information Administration)

**Usage:** Consensus forecasts (Short-Term Energy Outlook) when `EIA_API_KEY` is set

**License:** U.S. Government Public Domain

**Terms:**
- All EIA data is in the public domain
- Free for any use, including commercial
- Citation requested (but not required)
- No restrictions on redistribution

**Attribution:**
Forecasts include source links to EIA STEO releases and state "Source: EIA STEO"

**Redistribution Notes:**
- All uses permitted, including commercial redistribution
- No permission needed
- Public domain, no licensing concerns

**EIA Resources:**
- API Key Registration: https://www.eia.gov/opendata/register.php (free)
- Terms of Service: https://www.eia.gov/about/copyrights_reuse.php
- STEO Data: https://www.eia.gov/outlooks/steo/

## Running Ads: Licensing Summary

| Data Source | Commercial Use | Ads Permitted | Status | Notes |
|-------------|----------------|---------------|--------|-------|
| **Pyth Network** | $2,500+/month | With subscription | 401 Unauthorized | Requires paid Pyth Pro subscription + API key |
| **Yahoo Finance** | No | No | Not licensed | Violates ToS for ad-supported sites; replace before enabling ads |
| **EIA** | Yes | Yes | Public domain | No restrictions |
| **Generated/Estimated** | Yes | Yes | Original work | Model-generated forecasts and synthetic fallback data |

### Critical Issues for Monetization:

1. **Pyth Network:** Currently not working (`401 unauthorized`). Requires:
   - Paid subscription ($2,500/month for commodities)
   - Valid PYTH_API_KEY configuration
   - Budget approval for ongoing monthly costs

2. **Yahoo Finance:** **NOT CLEARED FOR AD-SUPPORTED USE**
   - Must replace with licensed provider before enabling AdSense
   - Continuing to use Yahoo Finance with ads risks legal action
   - Recommended: Switch to IEX Cloud, Alpha Vantage, or Polygon.io

3. **EIA:** No concerns, public domain

### Recommendations Before Enabling AdSense:

1. **Replace Yahoo Finance immediately** with a licensed commercial data provider
2. **Subscribe to Pyth Pro** and configure PYTH_API_KEY, or remove live price features
3. **Alternative:** Use only EIA public domain data + your own analysis/forecasts until commercial data sources are secured
4. **Budget:** Plan for $2,500-5,000/month in data costs for Pyth + commercial provider

## Legal Disclaimer

**This document provides general guidance and is not legal advice.** The owner/operator of liveoilprices.com should:

1. Review each data provider's current Terms of Service
2. Consult with legal counsel before monetizing the site
3. Secure proper commercial data licenses before enabling advertising
4. Monitor for any updates to data licensing terms
5. Budget for commercial data subscriptions ($3,000-6,000/month estimated)

**Do not enable AdSense until Yahoo Finance is replaced with a licensed provider.**

Last updated: October 17, 2026
