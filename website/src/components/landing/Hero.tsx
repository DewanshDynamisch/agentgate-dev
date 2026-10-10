import React from 'react';
import styles from './Hero.module.css';

export default function Hero() {
  return (
    <header id="hero" className={styles.heroSection}>
      <div className="container">
        <h1 id="hero-title" className={styles.title}>AgentGate</h1>
        <p className={styles.tagline}>
          A checkpoint in the only path between an AI agent and its tools.
        </p>
        <p className={styles.promise}>
          Secure your infrastructure by enforcing policies on every agent action before it happens.
        </p>
        <div className={styles.buttons}>
          <a className="button button--primary button--lg" href="/docs/quickstart">
            Quickstart
          </a>
          <a className="button button--secondary button--lg" href="#how-it-works">
            How it works
          </a>
        </div>
        
        {/* Slot for flow animation (Task 5) */}
        <div className={styles.animationSlot}>
          {/* FlowAnimation goes here */}
        </div>
      </div>
    </header>
  );
}
