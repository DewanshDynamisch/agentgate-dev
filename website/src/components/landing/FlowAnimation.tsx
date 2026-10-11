import React, { useState } from 'react';
import styles from './FlowAnimation.module.css';

export default function FlowAnimation() {
  const [paused, setPaused] = useState(false);

  return (
    <div className={`${styles.container} ${paused ? styles.paused : ''}`}>
      <div className={styles.controls}>
        <button 
          className={styles.pauseBtn}
          onClick={() => setPaused(!paused)}
          aria-label={paused ? "Play animation" : "Pause animation"}
        >
          {paused ? "Play" : "Pause"}
        </button>
      </div>

      <svg 
        className={styles.svg} 
        viewBox="0 0 800 400" 
        xmlns="http://www.w3.org/2000/svg"
        role="img"
        aria-label="Request flow animation showing allowed and denied paths"
      >
        <defs>
          <marker id="flow-arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
            <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--ag-text-secondary, #71717a)" />
          </marker>
        </defs>

        {/* Nodes */}
        <rect x="50" y="150" width="100" height="60" rx="8" className={styles.box} />
        <text x="100" y="185" className={styles.text}>Agent</text>

        <line x1="150" y1="180" x2="250" y2="180" className={styles.line} />

        <rect x="250" y="150" width="120" height="60" rx="8" className={styles.box} />
        <text x="310" y="185" className={styles.text}>agentgateway</text>

        <line x1="370" y1="180" x2="450" y2="180" className={styles.line} />

        <rect x="450" y="150" width="120" height="60" rx="8" className={styles.accentBox} />
        <text x="510" y="185" className={styles.text}>AgentGate</text>

        <line x1="570" y1="180" x2="650" y2="180" className={styles.line} />

        <rect x="650" y="150" width="100" height="60" rx="8" className={styles.box} />
        <text x="700" y="185" className={styles.text}>Tool (Backend)</text>

        {/* Keycloak */}
        <rect x="250" y="50" width="120" height="50" rx="8" className={styles.box} />
        <text x="310" y="80" className={styles.text}>Keycloak</text>
        <line x1="310" y1="150" x2="310" y2="100" className={styles.line} strokeDasharray="4" />

        {/* Postgres */}
        <rect x="450" y="270" width="120" height="50" rx="8" className={styles.box} />
        <text x="510" y="300" className={styles.text}>Postgres (Audit)</text>
        <line x1="510" y1="210" x2="510" y2="270" className={styles.line} />

        {/* Animated token */}
        <circle cx="0" cy="0" r="6" className={styles.token} />

        {/* Static representations for prefers-reduced-motion */}
        <g className={styles.staticToken}>
          {/* Allowed example */}
          <circle cx="600" cy="180" r="6" fill="var(--ag-allow, #15803d)" />
          <text x="600" y="165" className={styles.text} fontSize="10" fill="var(--ag-allow, #15803d)">Allowed</text>

          {/* Denied example */}
          <circle cx="400" cy="180" r="6" fill="var(--ag-deny, #b91c1c)" />
          <text x="400" y="165" className={styles.text} fontSize="10" fill="var(--ag-deny, #b91c1c)">Denied</text>
        </g>
      </svg>
    </div>
  );
}
