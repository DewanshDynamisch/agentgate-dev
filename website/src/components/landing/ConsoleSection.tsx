import React, { useState, useEffect } from 'react';
import styles from './ConsoleSection.module.css';

interface ManifestFile {
  name: string;
  bytes: number;
}

interface Manifest {
  files: ManifestFile[];
  totalBytes: number;
}

export default function ConsoleSection(): React.JSX.Element {
  const [tab, setTab] = useState<'video' | 'dashboard' | 'audit' | 'tools' | 'policies'>('video');
  const [theme, setTheme] = useState<'light' | 'dark'>('dark');
  const [manifest, setManifest] = useState<Manifest | null>(null);
  const [hasLoaded, setHasLoaded] = useState(false);

  useEffect(() => {
    fetch('/media/manifest.json')
      .then((res) => {
        if (!res.ok) throw new Error('Not found');
        return res.json();
      })
      .then((data: Manifest) => {
        setManifest(data);
      })
      .catch(() => {
        // In local development, manifest might not be present before capture runs
      })
      .finally(() => {
        setHasLoaded(true);
      });
  }, []);

  const tabs = [
    { id: 'video', label: 'Walkthrough Video' },
    { id: 'dashboard', label: 'Dashboard' },
    { id: 'audit', label: 'Audit Log' },
    { id: 'tools', label: 'Tools Catalog' },
    { id: 'policies', label: 'Policies' },
  ] as const;

  const currentScreenshot = `/media/${tab}-${theme}.png`;

  return (
    <section id="console" className={styles.section} aria-label="Admin Console Preview">
      <div className={styles.container}>
        <div className={styles.header}>
          <div className={styles.tag}>Console Experience</div>
          <h2 className={styles.title}>Production Admin Console</h2>
          <p className={styles.subtitle}>
            Zero-trust visibility into every model tool invocation. Inspect cryptographic audit hash chains,
            enforce real-time Cedar policies, and manage tool risk classifications live.
          </p>
        </div>

        <div className={styles.controlsBar}>
          <div className={styles.tabList} role="tablist" aria-label="Console views">
            {tabs.map((t) => (
              <button
                key={t.id}
                role="tab"
                aria-selected={tab === t.id}
                className={`${styles.tabBtn} ${tab === t.id ? styles.tabBtnActive : ''}`}
                onClick={() => setTab(t.id)}
              >
                {t.label}
              </button>
            ))}
          </div>

          {tab !== 'video' && (
            <div className={styles.themeToggle}>
              <button
                type="button"
                className={`${styles.toggleBtn} ${theme === 'light' ? styles.toggleBtnActive : ''}`}
                onClick={() => setTheme('light')}
              >
                Light
              </button>
              <button
                type="button"
                className={`${styles.toggleBtn} ${theme === 'dark' ? styles.toggleBtnActive : ''}`}
                onClick={() => setTheme('dark')}
              >
                Dark
              </button>
            </div>
          )}
        </div>

        <div className={styles.previewContainer}>
          {tab === 'video' ? (
            <div className={styles.videoWrapper}>
              <video
                className={styles.video}
                src="/media/walkthrough.webm"
                poster="/media/dashboard-light.png"
                muted
                loop
                playsInline
                autoPlay
                controls
                aria-label="AgentGate full console walkthrough video"
              />
            </div>
          ) : (
            <div className={styles.imageWrapper}>
              <img
                src={currentScreenshot}
                alt={`AgentGate Admin Console ${tab} view (${theme} theme)`}
                className={styles.screenshot}
                loading="lazy"
              />
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
