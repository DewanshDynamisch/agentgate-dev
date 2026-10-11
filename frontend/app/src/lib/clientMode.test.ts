import { describe, expect, it } from 'vitest';
import { selectClientMode } from './clientMode';

describe('selectClientMode', () => {
  it('uses the mock in the test environment', () => {
    expect(selectClientMode({ MODE: 'test', DEV: true, PROD: false })).toBe('mock');
  });
  it('uses the mock in dev when VITE_USE_MOCK is "true"', () => {
    expect(selectClientMode({ MODE: 'development', DEV: true, PROD: false, VITE_USE_MOCK: 'true' })).toBe('mock');
  });
  it('uses http in dev when VITE_USE_MOCK is not "true"', () => {
    expect(selectClientMode({ MODE: 'development', DEV: true, PROD: false })).toBe('http');
    expect(selectClientMode({ MODE: 'development', DEV: true, PROD: false, VITE_USE_MOCK: 'false' })).toBe('http');
  });
  it('ignores VITE_USE_MOCK in a production build', () => {
    expect(selectClientMode({ MODE: 'production', DEV: false, PROD: true, VITE_USE_MOCK: 'true' })).toBe('http');
  });
  it('never selects the mock in a production build, even with MODE=test', () => {
    expect(selectClientMode({ MODE: 'test', DEV: false, PROD: true, VITE_USE_MOCK: 'true' })).toBe('http');
  });
});
