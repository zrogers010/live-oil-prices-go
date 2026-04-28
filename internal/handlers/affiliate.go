package handlers

// TradingViewAffiliateURL is the single partner link used by:
//
//   - the "Powered by TradingView" attribution in the footer (every page),
//   - the contextual partner card on /charts and /commodity/{symbol},
//   - the /disclosure page.
//
// To activate revenue: replace YOUR_AFFILIATE_ID with the value from your
// TradingView affiliate dashboard (https://www.tradingview.com/affiliate-program/).
// Until then, the link points at the public TradingView homepage and earns
// no commission, but everything is wired up correctly so a single edit here
// is all that's needed to go live.
//
// All anchors that target this URL must use rel="noopener sponsored" so
// Google treats the link as a paid placement (per Google's Search Central
// guidance on qualifying outbound links). The "sponsored" attribute is
// applied directly in the templates.
const TradingViewAffiliateURL = "https://www.tradingview.com/?aff_id=165762"
