import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, cleanup } from '@testing-library/react';
import { ThemeProvider, useTheme } from './ThemeProvider';

function TestComponent() {
  const { theme, preference, setPreference } = useTheme();
  return (
    <div>
      <span data-testid="theme">{theme}</span>
      <span data-testid="preference">{preference}</span>
      <button onClick={() => setPreference('dark')}>Set Dark</button>
      <button onClick={() => setPreference('light')}>Set Light</button>
      <button onClick={() => setPreference('system')}>Set System</button>
    </div>
  );
}

describe('ThemeProvider', () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    
    // Mock matchMedia
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(query => ({
        matches: query === '(prefers-color-scheme: dark)',
        media: query,
        onchange: null,
        addListener: vi.fn(), // Deprecated
        removeListener: vi.fn(), // Deprecated
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    });
  });

  it('default follows a mocked matchMedia (system dark)', () => {
    render(
      <ThemeProvider>
        <TestComponent />
      </ThemeProvider>
    );

    expect(screen.getByTestId('theme').textContent).toBe('dark');
    expect(screen.getByTestId('preference').textContent).toBe('system');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('explicit preference overrides and persists', () => {
    render(
      <ThemeProvider>
        <TestComponent />
      </ThemeProvider>
    );

    act(() => {
      screen.getByText('Set Light').click();
    });

    expect(screen.getByTestId('theme').textContent).toBe('light');
    expect(screen.getByTestId('preference').textContent).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(localStorage.getItem('ag-theme-preference')).toBe('light');
  });

  it('storage throwing still works and does not crash', () => {
    const originalSetItem = localStorage.setItem;
    localStorage.setItem = () => { throw new Error('Quota exceeded'); };
    
    render(
      <ThemeProvider>
        <TestComponent />
      </ThemeProvider>
    );

    act(() => {
      screen.getByText('Set Light').click();
    });

    expect(screen.getByTestId('theme').textContent).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');

    localStorage.setItem = originalSetItem;
  });

  it('attribute updates on toggle', () => {
    render(
      <ThemeProvider>
        <TestComponent />
      </ThemeProvider>
    );

    act(() => {
      screen.getByText('Set Light').click();
    });
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');

    act(() => {
      screen.getByText('Set Dark').click();
    });
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });
});
