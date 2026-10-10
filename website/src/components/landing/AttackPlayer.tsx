import React, { useState, useEffect, useRef } from 'react';
import scenarios from '../../data/scenarios.json';
import styles from './AttackPlayer.module.css';

export default function AttackPlayer() {
  const [currentIndex, setCurrentIndex] = useState(0);
  const [stepIndex, setStepIndex] = useState(0);
  const [isPlaying, setIsPlaying] = useState(false);
  const playIntervalRef = useRef<number | null>(null);

  const scenario = scenarios[currentIndex];
  const totalSteps = scenario.steps.length + 1; // +1 for the result

  useEffect(() => {
    // Check reduced motion preference
    const mediaQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
    if (!mediaQuery.matches) {
       // Only start auto-play if we want it initially, but wait, instructions say "no auto-play when reduced motion is requested".
       // Does it imply auto-play by default?
       // Let's not auto-play by default unless they click play, or just auto-play but respect the pref.
    }
  }, []);

  useEffect(() => {
    if (isPlaying) {
      playIntervalRef.current = window.setInterval(() => {
        setStepIndex((prev) => {
          if (prev < totalSteps) {
            return prev + 1;
          } else {
            // go to next scenario
            setCurrentIndex((ci) => (ci + 1) % scenarios.length);
            return 0;
          }
        });
      }, 2000);
    } else if (playIntervalRef.current) {
      clearInterval(playIntervalRef.current);
    }
    return () => {
      if (playIntervalRef.current) clearInterval(playIntervalRef.current);
    };
  }, [isPlaying, totalSteps]);

  const handleNext = () => {
    if (stepIndex < totalSteps) {
      setStepIndex(stepIndex + 1);
    } else {
      setCurrentIndex((ci) => (ci + 1) % scenarios.length);
      setStepIndex(0);
    }
  };

  const handlePrev = () => {
    if (stepIndex > 0) {
      setStepIndex(stepIndex - 1);
    } else {
      const newCi = currentIndex === 0 ? scenarios.length - 1 : currentIndex - 1;
      setCurrentIndex(newCi);
      setStepIndex(scenarios[newCi].steps.length + 1);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowRight') {
      handleNext();
    } else if (e.key === 'ArrowLeft') {
      handlePrev();
    }
  };

  return (
    <section className={styles.container} aria-label="Attack Scenarios Player" onKeyDown={handleKeyDown} tabIndex={0}>
      <div className={styles.controls}>
        <button onClick={handlePrev} aria-label="Previous step">Prev</button>
        <button onClick={() => setIsPlaying(!isPlaying)} aria-label={isPlaying ? "Pause" : "Play"}>
          {isPlaying ? 'Pause' : 'Play'}
        </button>
        <button onClick={handleNext} aria-label="Next step">Next</button>
      </div>

      <div className={styles.playerArea}>
        <h3>{scenario.title}</h3>
        <p className={styles.attackDesc}>{scenario.attack}</p>
        
        <ul className={styles.stepList}>
          {scenario.steps.map((step, idx) => (
            <li key={idx} className={idx < stepIndex ? styles.stepVisible : styles.stepHidden}>
              {step}
            </li>
          ))}
        </ul>
        
        {stepIndex === totalSteps && (
          <div className={`${styles.result} ${scenario.outcome === 'blocked' ? styles.resultBlocked : styles.resultAllowed}`}>
            <strong>Verdict: {scenario.outcome.toUpperCase()}</strong> - {scenario.result}
            {scenario.http_status && <span> (Status: {scenario.http_status})</span>}
          </div>
        )}
      </div>
    </section>
  );
}
