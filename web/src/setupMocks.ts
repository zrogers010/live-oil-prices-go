const mockHTMLElement = () => {
  const el = {
    textContent: '',
    style: {
      display: '',
    },
  };
  return el;
};

document.getElementById = (id) => {
  if (id === 'errorContainer' || id === 'loader' || id === 'chartContainer' || id === 'chartOpen' || id === 'chartHigh' || id === 'chartLow' || id === 'chartClose' || id === 'chartVolume' || id === 'chartStats' || id === 'tickerTrack' || id === 'priceGrid' || id === 'marketTableBody' || id === 'newsGrid' || id === 'newsFeatured' || id === 'newsFilters' || id === 'chartSymbols' || id === 'chartTimeframes') {
    return mockHTMLElement();
  }
  return null;
};

// Mock closest method on HTMLElement prototype for buttons
HTMLElement.prototype.closest = function () { return this; };

// Mock querySelectorAll to return empty NodeList
HTMLElement.prototype.querySelectorAll = () => [];

// Mock addEventListener to noop
HTMLElement.prototype.addEventListener = () => {};

// Mock removeEventListener to noop
HTMLElement.prototype.removeEventListener = () => {};
