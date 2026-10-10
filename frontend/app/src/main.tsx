import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { ThemeProvider } from "./state/ThemeProvider";
import "./styles/tokens.css";
import "./styles/layout.css";
import "./styles/components.css";
import App from "./App.tsx";

try {
  const pref = localStorage.getItem('ag-theme-preference');
  if (pref === 'dark' || (pref !== 'light' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
    document.documentElement.setAttribute('data-theme', 'dark');
  }
} catch (e) {}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <ThemeProvider>
        <App />
      </ThemeProvider>
    </BrowserRouter>
  </StrictMode>
);
