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
HTMLElement.prototype.addEventListener = function() {};

// Mock removeEventListener to noop
HTMLElement.prototype.removeEventListener = function() {};

// Mock setAttribute to noop
HTMLElement.prototype.setAttribute = function() {};

// Mock innerHTML property for elements
Object.defineProperty(HTMLElement.prototype, 'innerHTML', {
  get: function() { return this._innerHTML || ''; },
  set: function(value) { this._innerHTML = value; },
  configurable: true,
});

// Mock classList with add/remove/toggle/contains noops
Object.defineProperty(HTMLElement.prototype, 'classList', {
  get: function() {
    return {
      add: function() {},
      remove: function() {},
      toggle: function() {},
      contains: function() { return false; },
    };
  },
  configurable: true,
});
