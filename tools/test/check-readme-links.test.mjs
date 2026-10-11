import test from 'node:test';
import assert from 'node:assert';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const checkerScript = path.resolve(__dirname, '../check-readme-links.mjs');

function runChecker(markdownContent) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'crl-test-'));
  const mdFile = path.join(tmp, 'TEST.md');
  fs.writeFileSync(mdFile, markdownContent, 'utf8');
  
  // also create a dummy file to link to
  fs.writeFileSync(path.join(tmp, 'existing.md'), 'hi', 'utf8');
  fs.mkdirSync(path.join(tmp, 'img'));
  fs.writeFileSync(path.join(tmp, 'img/pic.png'), 'png', 'utf8');

  const result = spawnSync(
    process.execPath,
    [checkerScript, mdFile],
    { encoding: 'utf8', cwd: tmp, stdio: 'pipe' }
  );
  fs.rmSync(tmp, { recursive: true, force: true });
  return {
    code: result.status,
    output: (result.stdout || '') + (result.stderr || ''),
  };
}

test('check-readme-links', async (t) => {
  await t.test('fails if zero links', () => {
    const r = runChecker('# Hello\nNo links here.');
    assert.notStrictEqual(r.code, 0);
    assert.match(r.output, /zero links|0 links/i);
  });

  await t.test('passes with valid relative link and image', () => {
    const r = runChecker('Link to [page](./existing.md) and ![pic](img/pic.png) and [external](https://example.com).');
    assert.strictEqual(r.code, 0, r.output);
  });

  await t.test('fails with missing relative link', () => {
    const r = runChecker('Link to [missing](./missing.md)');
    assert.notStrictEqual(r.code, 0);
    assert.match(r.output, /not found/i);
  });
  
  await t.test('passes with anchor links', () => {
    const r = runChecker('Link to [anchor](#my-heading) and [file](existing.md#heading)');
    assert.strictEqual(r.code, 0, r.output);
  });
});

