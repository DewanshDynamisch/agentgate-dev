import test from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';
import os from 'node:os';

import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const checkOfflineScript = path.resolve(__dirname, '../check-offline.mjs');

test('check-offline tests', async (t) => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'check-offline-test-'));
  
  const runCheck = (dir) => {
    try {
      execSync(`node ${checkOfflineScript} ${dir}`, { encoding: 'utf-8', stdio: 'pipe' });
      return { success: true };
    } catch (error) {
      return { success: false, output: error.stdout + error.stderr };
    }
  };

  await t.test('clean dir passes', () => {
    const dir = path.join(tempDir, 'clean');
    fs.mkdirSync(dir);
    fs.writeFileSync(path.join(dir, 'index.html'), '<html><body><a href="https://github.com/">Link</a></body></html>');
    fs.writeFileSync(path.join(dir, 'style.css'), 'body { background: url(/local.png); }');
    const result = runCheck(dir);
    assert.strictEqual(result.success, true);
  });

  await t.test('CSS @import of Google Fonts URL fails', () => {
    const dir = path.join(tempDir, 'import');
    fs.mkdirSync(dir);
    fs.writeFileSync(path.join(dir, 'style.css'), '@import url("https://fonts.googleapis.com/css?family=Open+Sans");');
    const result = runCheck(dir);
    assert.strictEqual(result.success, false);
    assert.match(result.output, /https:\/\/fonts\.googleapis\.com/);
  });

  await t.test('script src https fails', () => {
    const dir = path.join(tempDir, 'script');
    fs.mkdirSync(dir);
    fs.writeFileSync(path.join(dir, 'index.html'), '<script src="https://example.com/script.js"></script>');
    const result = runCheck(dir);
    assert.strictEqual(result.success, false);
    assert.match(result.output, /https:\/\/example\.com\/script\.js/);
  });

  await t.test('a href https passes', () => {
    const dir = path.join(tempDir, 'href');
    fs.mkdirSync(dir);
    fs.writeFileSync(path.join(dir, 'index.html'), '<a href="https://github.com/example">Link</a>');
    const result = runCheck(dir);
    assert.strictEqual(result.success, true);
  });

  await t.test('empty dir fails', () => {
    const dir = path.join(tempDir, 'empty');
    fs.mkdirSync(dir);
    const result = runCheck(dir);
    assert.strictEqual(result.success, false);
    assert.match(result.output, /zero files/i);
  });
});
