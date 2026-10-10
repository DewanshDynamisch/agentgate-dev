import { test, describe, before, after } from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';
import os from 'node:os';

const CLI_PATH = path.resolve(process.cwd(), 'tools', 'check-brand-tokens.mjs');

describe('check-brand-tokens', () => {
  let tempDir;

  before(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'ag-tokens-test-'));
  });

  after(() => {
    fs.rmSync(tempDir, { recursive: true, force: true });
  });

  const runCli = (args) => {
    try {
      const output = execSync(`node "${CLI_PATH}" ${args.join(' ')}`, { encoding: 'utf8', stdio: 'pipe' });
      return { status: 0, output };
    } catch (err) {
      return { status: err.status, output: err.stdout + err.stderr };
    }
  };

  test('identical files pass', () => {
    const validCss = `
      :root {
        --ag-bg-canvas: #ffffff;
        --ag-bg-surface: #f8fafc;
        --ag-border: #e2e8f0;
        --ag-text-primary: #0f172a;
        --ag-text-secondary: #475569;
        --ag-text-inverse: #ffffff;
        --ag-accent: #4338ca;
        --ag-accent-hover: #3730a3;
        --ag-accent-soft: #e0e7ff;
        --ag-allow: #15803d;
        --ag-allow-soft: #dcfce7;
        --ag-deny: #b91c1c;
        --ag-deny-soft: #fee2e2;
        --ag-candidate: #a16207;
        --ag-candidate-soft: #fef9c3;
        --ag-font-sans: 'Geist', sans-serif;
        --ag-font-mono: 'Geist Mono', monospace;
      }
      [data-theme="dark"] {
        --ag-bg-canvas: #000000;
        --ag-bg-surface: #111111;
        --ag-border: #333333;
        --ag-text-primary: #ffffff;
        --ag-text-secondary: #cccccc;
        --ag-text-inverse: #000000;
        --ag-accent: #5555ff;
        --ag-accent-hover: #7777ff;
        --ag-accent-soft: #111144;
        --ag-allow: #22aa44;
        --ag-allow-soft: #003311;
        --ag-deny: #ff3333;
        --ag-deny-soft: #440000;
        --ag-candidate: #ffaa00;
        --ag-candidate-soft: #442200;
        --ag-font-sans: 'Geist', sans-serif;
        --ag-font-mono: 'Geist Mono', monospace;
      }
    `;
    fs.writeFileSync(path.join(tempDir, 'file1.css'), validCss);
    fs.writeFileSync(path.join(tempDir, 'file2.css'), validCss);
    
    const res = runCli(['--files', path.join(tempDir, 'file1.css'), path.join(tempDir, 'file2.css')]);
    assert.strictEqual(res.status, 0);
  });

  test('differing value fails naming the token and file', () => {
    const validCss = `:root { --ag-bg-canvas: #ffffff; } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    const invalidCss = `:root { --ag-bg-canvas: #eeeeee; } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    fs.writeFileSync(path.join(tempDir, 'diff1.css'), validCss);
    fs.writeFileSync(path.join(tempDir, 'diff2.css'), invalidCss);

    const res = runCli(['--files', path.join(tempDir, 'diff1.css'), path.join(tempDir, 'diff2.css')]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /--ag-bg-canvas/);
    assert.match(res.output, /diff2\.css/);
  });

  test('missing token fails', () => {
    const validCss = `:root { --ag-bg-canvas: #ffffff; } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    const invalidCss = `:root { } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    fs.writeFileSync(path.join(tempDir, 'miss1.css'), validCss);
    fs.writeFileSync(path.join(tempDir, 'miss2.css'), invalidCss);

    const res = runCli(['--files', path.join(tempDir, 'miss1.css'), path.join(tempDir, 'miss2.css')]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /missing/i);
    assert.match(res.output, /--ag-bg-canvas/);
  });

  test('missing dark block fails', () => {
    const validCss = `:root { --ag-bg-canvas: #ffffff; } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    const invalidCss = `:root { --ag-bg-canvas: #ffffff; }`;
    fs.writeFileSync(path.join(tempDir, 'dark1.css'), validCss);
    fs.writeFileSync(path.join(tempDir, 'dark2.css'), invalidCss);

    const res = runCli(['--files', path.join(tempDir, 'dark1.css'), path.join(tempDir, 'dark2.css')]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /dark/i);
  });

  test('missing dark block with --allow-missing-dark flag passes', () => {
    const validCss = `:root { --ag-bg-canvas: #ffffff; } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    const invalidCss = `:root { --ag-bg-canvas: #ffffff; }`;
    fs.writeFileSync(path.join(tempDir, 'dark_allow1.css'), validCss);
    fs.writeFileSync(path.join(tempDir, 'dark_allow2.css'), invalidCss);

    const res = runCli(['--files', path.join(tempDir, 'dark_allow1.css'), path.join(tempDir, 'dark_allow2.css'), '--allow-missing-dark', 'dark_allow2.css']);
    assert.strictEqual(res.status, 0);
  });

  test('missing optional file is skipped with a message', () => {
    const validCss = `:root { --ag-bg-canvas: #ffffff; } [data-theme="dark"] { --ag-bg-canvas: #000000; }`;
    fs.writeFileSync(path.join(tempDir, 'exist.css'), validCss);
    
    const missingFile = path.join(tempDir, 'does-not-exist.css');
    const res = runCli(['--files', path.join(tempDir, 'exist.css'), missingFile, '--optional', missingFile]);
    assert.strictEqual(res.status, 0);
    assert.match(res.output, /skipped: not present/i);
  });

  test('zero shared tokens found fails (no vacuous pass)', () => {
    const emptyCss = `:root { --other: #fff; } [data-theme="dark"] { --other: #000; }`;
    fs.writeFileSync(path.join(tempDir, 'empty1.css'), emptyCss);
    fs.writeFileSync(path.join(tempDir, 'empty2.css'), emptyCss);
    
    const res = runCli(['--files', path.join(tempDir, 'empty1.css'), path.join(tempDir, 'empty2.css')]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /zero shared tokens/i);
  });
});
