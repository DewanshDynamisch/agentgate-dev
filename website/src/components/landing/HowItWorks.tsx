import React from 'react';
import styles from './HowItWorks.module.css';

export default function HowItWorks() {
  return (
    <section id="how-it-works" className={styles.section}>
      <div className="container">
        <h2>How It Works</h2>
        <p className={styles.intro}>
          Changing what AI agents can do is a powerful operation. AgentGate ensures safety by walking you through four checkpoints before any rule goes live:
        </p>
        <div className={styles.steps}>
          <div className={styles.step}>
            <div className={styles.stepNum}>1</div>
            <h3>Write the Policy</h3>
            <p>Define precisely what the agent is allowed to do, mapping the intended behavior to the actual backend tool execution.</p>
          </div>
          <div className={styles.step}>
            <div className={styles.stepNum}>2</div>
            <h3>Validate Syntax</h3>
            <p>Before any testing begins, the system verifies that the policy is perfectly structured, catching mistakes immediately.</p>
          </div>
          <div className={styles.step}>
            <div className={styles.stepNum}>3</div>
            <h3>Test Safely</h3>
            <p>Run a "dry run" against realistic sample requests. You'll see exactly which decisions would change, without altering live behavior.</p>
          </div>
          <div className={styles.step}>
            <div className={styles.stepNum}>4</div>
            <h3>Deploy with Confidence</h3>
            <p>Only after reviewing the dry run results do you activate the policy. Every change requires a documented reason for the audit log.</p>
          </div>
        </div>
      </div>
    </section>
  );
}
