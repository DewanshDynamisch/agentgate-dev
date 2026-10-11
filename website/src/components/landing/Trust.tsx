import React from 'react';
import styles from './Trust.module.css';

export default function Trust() {
  return (
    <section id="trust" className={styles.section}>
      <div className="container">
        <h2>Designed for Zero Trust</h2>
        <div className="ag-trust-badge"></div>
        <div className={styles.grid}>
          <div className={styles.card}>
            <h3>Fails Closed</h3>
            <p>If anything goes wrong—a network timeout, a malformed request, or an engine error—AgentGate denies the action. <a href="/docs/security-and-limitations">Read the security model</a>.</p>
          </div>
          <div className={styles.card}>
            <h3>Tamper-Evident Audit</h3>
            <p>Every allowed action is written to a verifiable hash chain before it ever executes. <a href="/docs/audit">Learn about the audit trail</a>.</p>
          </div>
          <div className={styles.card}>
            <h3>No Shared Credentials</h3>
            <p>Agents authenticate with short-lived tokens. They never see your backend database passwords or API keys. <a href="/docs/credentials">See how credentials work</a>.</p>
          </div>
        </div>
      </div>
    </section>
  );
}
