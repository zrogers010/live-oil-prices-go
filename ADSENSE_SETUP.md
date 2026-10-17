# AdSense Setup Guide

This document explains how to enable Google AdSense advertising on Live Oil Prices.

## Prerequisites

1. **Apply for Google AdSense**
   - Visit [https://www.google.com/adsense/start/](https://www.google.com/adsense/start/)
   - Sign in with a Google account
   - Submit your application with liveoilprices.com
   - Complete the site verification process
   - Wait for approval (typically 1-2 weeks)

2. **Obtain Your Publisher ID**
   - Once approved, go to the AdSense dashboard
   - Navigate to **Account** → **Account information**
   - Copy your **Publisher ID** (format: `ca-pub-XXXXXXXXXXXXXXXX`)

## Configuration

### Required Environment Variable

Set the `ADSENSE_CLIENT` environment variable to your AdSense publisher ID:

```bash
export ADSENSE_CLIENT="ca-pub-XXXXXXXXXXXXXXXX"
```

When this variable is set:
- The AdSense script tag is injected into all pages
- Ad slots are rendered on the home page
- The `/ads.txt` file is served automatically

When this variable is **not set** or empty:
- No ads are displayed
- No AdSense scripts are loaded
- `/ads.txt` returns 404

### Optional: Custom Ad Slot IDs

For better control and reporting, you can specify custom ad unit slot IDs:

```bash
export ADSENSE_SLOT_BELOW_PRICE_TABLE="1234567890"
export ADSENSE_SLOT_NEWS_FLOW="0987654321"
export ADSENSE_SLOT_SIDEBAR="1122334455"
export ADSENSE_SLOT_ARTICLE="5544332211"
```

If these are not set, AdSense will use automatic ad units.

### Deployment Example (systemd)

Add the environment variables to your systemd service file:

```ini
[Service]
Environment="ADSENSE_CLIENT=ca-pub-XXXXXXXXXXXXXXXX"
Environment="ADSENSE_SLOT_BELOW_PRICE_TABLE=1234567890"
Environment="ADSENSE_SLOT_NEWS_FLOW=0987654321"
```

Then reload and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl restart liveoilprices
```

### Deployment Example (Docker)

Pass environment variables when running the container:

```bash
docker run -e ADSENSE_CLIENT="ca-pub-XXXXXXXXXXXXXXXX" \
           -e ADSENSE_SLOT_BELOW_PRICE_TABLE="1234567890" \
           -p 8080:8080 \
           liveoilprices:latest
```

## Ad Placements

The following ad slots are currently implemented:

1. **Below Price Table** (`ADSENSE_SLOT_BELOW_PRICE_TABLE`)
   - Location: Home page, between the price table and the charts section
   - Format: Horizontal banner (typically 728×90 or responsive)
   - Space reserved: 970px max-width

2. **News Flow** (`ADSENSE_SLOT_NEWS_FLOW`)
   - Location: Home page, within the news section
   - Format: Auto-sized responsive ad
   - Space reserved: 728px max-width

These placements were chosen to:
- Avoid appearing above the main price data
- Minimize layout shift (reserved space)
- Use lazy loading where supported by AdSense

## ads.txt

The `/ads.txt` route is automatically generated when `ADSENSE_CLIENT` is set. It includes:

```
google.com, pub-XXXXXXXXXXXXXXXX, DIRECT, f08c47fec0942fa0
```

Google will verify this file during the onboarding process.

## Privacy Policy

A privacy policy is **required** by Google AdSense. The site now includes a privacy policy at `/privacy` that discloses:
- Use of cookies
- Google Analytics tracking
- Google AdSense advertising
- User rights and opt-out options

## Verification

After deploying with the environment variable set:

1. **Test the site:**
   ```bash
   curl http://localhost:8080/ | grep adsbygoogle
   ```
   You should see AdSense script tags.

2. **Check ads.txt:**
   ```bash
   curl http://localhost:8080/ads.txt
   ```
   Should return:
   ```
   google.com, pub-XXXXXXXXXXXXXXXX, DIRECT, f08c47fec0942fa0
   ```

3. **Verify in AdSense Dashboard:**
   - Go to **Sites** → **Add a site**
   - Add `liveoilprices.com`
   - AdSense will check for the ads.txt file
   - Ads should start serving within 24-48 hours

## Removing Ads

To disable all advertising, simply unset or remove the `ADSENSE_CLIENT` environment variable and restart the server:

```bash
unset ADSENSE_CLIENT
sudo systemctl restart liveoilprices
```

All ad slots and scripts will be removed from the site immediately.

## Important Notes

- **No ads above the fold:** Ads are deliberately placed below the main price data to maintain a clean user experience.
- **Layout shift prevention:** Each ad slot reserves space to prevent content jumping when ads load.
- **Performance:** Ads are lazy-loaded where possible to minimize impact on page speed.
- **Compliance:** The privacy policy meets Google AdSense requirements for disclosure.

## Support

For AdSense-specific issues:
- [AdSense Help Center](https://support.google.com/adsense)
- [AdSense Community](https://support.google.com/adsense/community)

For site-specific technical issues, see the main README.md.
