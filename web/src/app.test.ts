import '../setupMocks';
import { jest } from '@jest/globals';
import { loadPrices, loadChart, loadNews, setError, clearError, setLoading } from './app';
import * as api from './api';

describe('App error and loading state', () => {
  let errorContainer: HTMLElement;
  let loader: HTMLElement;

  beforeEach(() => {
    errorContainer = document.createElement('div');
    errorContainer.id = 'errorContainer';
    document.body.appendChild(errorContainer);

    loader = document.createElement('div');
    loader.id = 'loader';
    document.body.appendChild(loader);
  });

  afterEach(() => {
    errorContainer.remove();
    loader.remove();
    jest.restoreAllMocks();
  });

  test('setError sets error message and shows error container', () => {
    setError('Test error');
    expect(errorContainer.textContent).toBe('Test error');
    expect(errorContainer.style.display).toBe('block');

    clearError();
    expect(errorContainer.textContent).toBe('');
    expect(errorContainer.style.display).toBe('none');
  });

  test('setLoading shows and hides loader', () => {
    setLoading(true);
    expect(loader.style.display).toBe('block');
    setLoading(false);
    expect(loader.style.display).toBe('none');
  });

  test('loadPrices sets error on API failure', async () => {
    jest.spyOn(api, 'getPrices').mockRejectedValue(new Error('API failure'));
    await loadPrices();
    expect(errorContainer.textContent).toMatch(/failed to load prices/i);
  });

  test('loadChart sets error on API failure', async () => {
    jest.spyOn(api, 'getChartData').mockRejectedValue(new Error('API failure'));
    await loadChart('WTI', 90);
    expect(errorContainer.textContent).toMatch(/failed to load chart/i);
  });

  test('loadNews sets error on API failure', async () => {
    jest.spyOn(api, 'getNews').mockRejectedValue(new Error('API failure'));
    await loadNews();
    expect(errorContainer.textContent).toMatch(/failed to load news/i);
  });
});
