import { test, describe, before, after } from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs';
import path from 'node:path';
import { execSync } from 'node:child_process';
import os from 'node:os';

const CLI_PATH = path.resolve(process.cwd(), 'tools', 'check-svg.mjs');

describe('check-svg', () => {
  let tempDir;

  before(() => {
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'ag-svg-test-'));
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

  test('valid SVG passes', () => {
    const file = path.join(tempDir, 'valid.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>Valid</title><path d="M0 0"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 0, 'Expected valid SVG to pass');
  });

  test('missing viewBox fails', () => {
    const file = path.join(tempDir, 'missing-viewbox.svg');
    fs.writeFileSync(file, '<svg><title>Missing viewBox</title><path d="M0 0"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /viewBox/i);
  });

  test('missing title fails', () => {
    const file = path.join(tempDir, 'missing-title.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><path d="M0 0"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /title/i);
  });

  test('script tag fails', () => {
    const file = path.join(tempDir, 'script.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>Script</title><script>alert(1)</script></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /script/i);
  });

  test('on* attribute fails', () => {
    const file = path.join(tempDir, 'onattr.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>On Attr</title><path d="M0 0" onclick="alert(1)"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /on\*/i);
  });

  test('external href fails', () => {
    const file = path.join(tempDir, 'href.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>Href</title><image href="https://example.com/img.png"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /http/i);
  });

  test('external xlink:href fails', () => {
    const file = path.join(tempDir, 'xlink.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>Xlink</title><image xlink:href="http://example.com/img.png"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /http/i);
  });

  test('data:text/html url fails', () => {
    const file = path.join(tempDir, 'data.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>Data URL</title><image href="data:text/html,<html></html>"/></svg>');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /data:text\/html/i);
  });

  test('invalid XML fails', () => {
    const file = path.join(tempDir, 'invalid.svg');
    fs.writeFileSync(file, '<svg viewBox="0 0 100 100"><title>Invalid</title><path d="M0 0">');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /xml/i);
  });

  test('empty file fails', () => {
    const file = path.join(tempDir, 'empty.svg');
    fs.writeFileSync(file, '');
    const res = runCli([file]);
    assert.strictEqual(res.status, 1);
    assert.match(res.output, /empty/i);
  });
});
