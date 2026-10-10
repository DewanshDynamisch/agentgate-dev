import React from 'react';
import styles from './QuickstartTeaser.module.css';

export default function QuickstartTeaser() {
  return (
    <section id="quickstart" className={styles.section}>
      <div className="container">
        <h2>Ready to secure your agents?</h2>
        <div className={styles.terminal}>
          <pre><code>git clone https://github.com/agentgate/agentgate.git
cd agentgate/deploy/demo
docker run --rm -v "$(pwd)/secrets:/out" --entrypoint sh alpine/openssl -c "openssl genrsa -out /out/cred_key.pem 2048 && chmod 644 /out/cred_key.pem"
docker compose up -d --build</code></pre>
        </div>
        <div className={styles.action}>
          <a className="button button--primary button--lg" href="/docs/quickstart">
            Read the full Quickstart
          </a>
        </div>
      </div>
    </section>
  );
}
