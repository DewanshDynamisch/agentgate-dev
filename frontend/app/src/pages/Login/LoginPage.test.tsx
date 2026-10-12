import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LoginPage } from './LoginPage';

// Mock useAuth; individual tests may flip ssoConfigured.
const authState = vi.hoisted(() => ({ ssoConfigured: true }));
vi.mock('../../state/AuthProvider', () => ({
  useAuth: () => ({
    user: null,
    loading: false,
    ssoConfigured: authState.ssoConfigured,
    login: vi.fn().mockResolvedValue(undefined),
  }),
}));

describe('LoginPage', () => {
  afterEach(() => {
    cleanup();
    authState.ssoConfigured = true;
  });

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the AgentGate logo with correct alt text', () => {
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    );
    const logo = screen.getByRole('img', { name: /agentgate/i });
    expect(logo).toBeInTheDocument();
  });

  it('renders the tagline', () => {
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    );
    expect(screen.getByText(/checkpoint/i)).toBeInTheDocument();
  });

  it('renders the Sign in with SSO button when SSO is configured', () => {
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    );
    const btn = screen.getByRole('button', { name: /sign in with sso/i });
    expect(btn).toBeInTheDocument();
  });

  it('renders SSO not configured message when ssoConfigured is false', () => {
    authState.ssoConfigured = false;
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    );
    expect(screen.getByText(/sso not configured/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /sign in with sso/i })).not.toBeInTheDocument();
  });

  it('wraps the SSO not configured message in a single flex child so it wraps inside the alert', () => {
    authState.ssoConfigured = false;
    const { container } = render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>
    );
    const alert = container.querySelector('.ag-alert');
    expect(alert).not.toBeNull();
    // .ag-alert is display:flex; loose text and inline elements would each become a flex item.
    const nonEmptyTextNodes = Array.from(alert!.childNodes).filter(
      (n) => n.nodeType === Node.TEXT_NODE && n.textContent!.trim() !== '',
    );
    expect(nonEmptyTextNodes).toHaveLength(0);
    expect(alert!.children).toHaveLength(1);
  });
});
