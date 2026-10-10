import React from 'react';
import styles from './Problem.module.css';

export default function Problem() {
  return (
    <section id="problem" className={styles.problemSection}>
      <div className="container">
        <h2>The Problem</h2>
        <div className={styles.statements}>
          <div className={styles.statement}>
            <h3>AI Agents are Unpredictable</h3>
            <p>You cannot guarantee an LLM won't hallucinate a destructive tool call or be manipulated by a prompt injection attack.</p>
          </div>
          <div className={styles.statement}>
            <h3>Backend Credentials are Fragile</h3>
            <p>Giving a third-party hosted agent direct database credentials or API keys creates an massive and unmonitorable blast radius.</p>
          </div>
          <div className={styles.statement}>
            <h3>Blind Spots are Unacceptable</h3>
            <p>When an agent takes action, you need a tamper-evident audit trail of exactly what it did, not just its conversational logs.</p>
          </div>
        </div>
      </div>
    </section>
  );
}
