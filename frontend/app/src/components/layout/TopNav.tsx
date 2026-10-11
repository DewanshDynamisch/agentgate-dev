import { NavLink } from "react-router-dom";
import { ShieldCheck } from "lucide-react";
import { useAuth } from "../../state/AuthProvider";
import { useTheme } from "../../state/ThemeProvider";
import { QuickSearch } from "./QuickSearch";

interface NavItem {
  to: string;
  label: string;
  end?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  { to: "/", label: "Dashboard", end: true },
  { to: "/policies", label: "Policies" },
  { to: "/tools", label: "Tools & resources" },
  { to: "/identities", label: "Identities" },
  { to: "/audit", label: "Audit log" },
  { to: "/settings", label: "Settings" },
];

export function TopNav() {
  const { logout } = useAuth();
  const { preference, setPreference } = useTheme();

  return (
    <header className="ag-topbar">
      <div className="ag-brand">
        <ShieldCheck size={20} strokeWidth={2} />
        <div className="ag-brand-text">
          <span className="ag-brand-title">AgentGate</span>
          <span className="ag-brand-tagline">Secure AI actions</span>
        </div>
      </div>

      <nav className="ag-topnav">
        {NAV_ITEMS.map(({ to, label, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) => `ag-topnav-item${isActive ? " active" : ""}`}
          >
            {label}
          </NavLink>
        ))}
      </nav>

      <div className="ag-topbar-right">
        <select
          value={preference}
          onChange={(e) => setPreference(e.target.value as any)}
          style={{ marginRight: 8, padding: '2px 4px', borderRadius: 'var(--ag-radius-control)', border: '1px solid var(--ag-border)', background: 'var(--ag-bg-surface)', color: 'var(--ag-text-primary)' }}
        >
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
        <QuickSearch />
        <div className="ag-topbar-divider" />
        <button
          onClick={() => void logout()}
          title="Sign out"
          className="ag-avatar"
          style={{ border: "none", cursor: "pointer", fontFamily: "inherit" }}
        >
          SA
        </button>
      </div>
    </header>
  );
}
