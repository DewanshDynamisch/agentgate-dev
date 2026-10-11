import { spawn } from 'node:child_process';
import lighthouse from 'lighthouse';
import * as chromeLauncher from 'chrome-launcher';

async function runAudit() {
  console.log('Starting docusaurus serve on port 3000...');
  const server = spawn('npx', ['docusaurus', 'serve', '--port', '3000'], {
    shell: true,
    stdio: 'ignore'
  });

  // Wait for server to be responsive
  let ready = false;
  for (let i = 0; i < 30; i++) {
    try {
      const res = await fetch('http://localhost:3000/');
      if (res.ok) {
        ready = true;
        break;
      }
    } catch (_) {}
    await new Promise(r => setTimeout(r, 1000));
  }

  if (!ready) {
    server.kill();
    throw new Error('Docusaurus serve failed to start on port 3000');
  }

  console.log('Server is ready! Launching Chrome for Lighthouse...');
  const chrome = await chromeLauncher.launch({
    chromeFlags: ['--headless', '--no-sandbox', '--disable-gpu']
  });

  const options = {
    logLevel: 'error',
    output: 'json',
    onlyCategories: ['performance', 'accessibility'],
    port: chrome.port,
    formFactor: 'desktop',
    screenEmulation: {
      mobile: false,
      width: 1350,
      height: 940,
      deviceScaleFactor: 1,
      disabled: false
    },
    throttlingMethod: 'provided'
  };

  const results = [];

  // Audit /
  console.log('Running Lighthouse on / ...');
  const resHome = await lighthouse('http://localhost:3000/', options);
  const homeReport = JSON.parse(resHome.report);
  const homePerf = Math.round((homeReport.categories.performance?.score || 0) * 100);
  const homeA11y = Math.round((homeReport.categories.accessibility?.score || 0) * 100);
  console.log('--- Failing Accessibility Audits on / ---');
  for (const [id, a] of Object.entries(homeReport.audits)) {
    if (a.score !== null && a.score < 1 && homeReport.categories.accessibility.auditRefs.some(r => r.id === id)) {
      console.log(`- ${id}: ${a.title} (${a.explanation || ''})`);
      if (a.details && a.details.items) {
        console.log(JSON.stringify(a.details.items.map(item => item.node ? { selector: item.node.selector, snippet: item.node.snippet } : item), null, 2));
      }
    }
  }
  results.push({ page: '/', performance: homePerf, accessibility: homeA11y });

  // Audit /docs/quickstart
  console.log('Running Lighthouse on /docs/quickstart ...');
  const resQs = await lighthouse('http://localhost:3000/docs/quickstart', options);
  const qsReport = JSON.parse(resQs.report);
  const qsPerf = Math.round((qsReport.categories.performance?.score || 0) * 100);
  const qsA11y = Math.round((qsReport.categories.accessibility?.score || 0) * 100);
  console.log('--- Failing Accessibility Audits on /docs/quickstart ---');
  for (const [id, a] of Object.entries(qsReport.audits)) {
    if (a.score !== null && a.score < 1 && qsReport.categories.accessibility.auditRefs.some(r => r.id === id)) {
      console.log(`- ${id}: ${a.title} (${a.explanation || ''})`);
      if (a.details && a.details.items) {
        console.log(JSON.stringify(a.details.items.map(item => item.node ? { selector: item.node.selector, snippet: item.node.snippet } : item), null, 2));
      }
    }
  }
  results.push({ page: '/docs/quickstart', performance: qsPerf, accessibility: qsA11y });

  await chrome.kill();
  server.kill();

  console.log('\n========================================');
  console.log('       LIGHTHOUSE AUDIT SCORES          ');
  console.log('========================================');
  console.table(results);

  let pass = true;
  if (homeA11y < 95) {
    console.error(`FAIL: / accessibility score ${homeA11y} < 95`);
    pass = false;
  }
  if (homePerf < 90) {
    console.error(`FAIL: / performance score ${homePerf} < 90`);
    pass = false;
  }
  if (qsA11y < 95) {
    console.error(`FAIL: /docs/quickstart accessibility score ${qsA11y} < 95`);
    pass = false;
  }

  if (!pass) {
    process.exit(1);
  }
  console.log('All Lighthouse targets MET!');
}

runAudit().catch(err => {
  console.error('Audit failed:', err);
  process.exit(1);
});
