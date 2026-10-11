import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { renderHook, cleanup } from '@testing-library/react';
import { usePageTitle } from './usePageTitle';

describe('usePageTitle', () => {
  const originalTitle = document.title;

  beforeEach(() => {
    document.title = 'AgentGate';
  });

  afterEach(() => {
    cleanup();
    document.title = originalTitle;
  });

  it.each([
    ['Dashboard', 'Dashboard · AgentGate'],
    ['Audit', 'Audit · AgentGate'],
    ['Tools', 'Tools · AgentGate'],
    ['Policies', 'Policies · AgentGate'],
    ['Identities', 'Identities · AgentGate'],
    ['Settings', 'Settings · AgentGate'],
    ['Login', 'Login · AgentGate'],
    ['Not found', 'Not found · AgentGate'],
    ['Not authorized', 'Not authorized · AgentGate'],
  ])('sets document.title to %s for page %s', (title, expected) => {
    renderHook(() => usePageTitle(title));
    expect(document.title).toBe(expected);
  });

  it('reverts document.title to AgentGate on unmount', () => {
    const { unmount } = renderHook(() => usePageTitle('Dashboard'));
    expect(document.title).toBe('Dashboard · AgentGate');
    unmount();
    expect(document.title).toBe('AgentGate');
  });
});
