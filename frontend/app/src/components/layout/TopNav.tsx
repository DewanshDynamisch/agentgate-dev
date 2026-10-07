import { NavLink } from "react-router-dom";
import { ShieldCheck } from "lucide-react";
import { useAuth } from "../../state/AuthProvider";
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
