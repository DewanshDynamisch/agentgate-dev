import React from 'react';
import Layout from '@theme/Layout';

export default function Home(): JSX.Element {
  return (
    <Layout
      title="Welcome"
      description="AgentGate documentation">
      <main>
        <div style={{padding: '2rem', textAlign: 'center'}}>
          <h1>Welcome to AgentGate</h1>
          <p>
            <a href="/docs/intro">Go to Docs</a>
          </p>
        </div>
      </main>
    </Layout>
  );
}
