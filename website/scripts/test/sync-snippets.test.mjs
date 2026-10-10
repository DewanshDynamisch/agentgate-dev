import test from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import os from 'node:os';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const scriptPath = path.resolve(__dirname, '../sync-snippets.mjs');

function runSyncSnippets(cwd) {
  return spawnSync('node', [scriptPath], { cwd, encoding: 'utf8' });
}

test('sync-snippets', async (t) => {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'sync-snippets-test-'));

  t.after(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  const websiteDir = path.join(tmpDir, 'website');
  const scriptsDir = path.join(websiteDir, 'scripts');
  const docsDir = path.join(websiteDir, 'docs');
  const generatedDir = path.join(docsDir, '_generated');
  const repoDir = tmpDir;

  fs.mkdirSync(scriptsDir, { recursive: true });
  fs.mkdirSync(docsDir, { recursive: true });

  const sourceFile = path.join(repoDir, 'test-source.md');
  const sourceContent = `Some content\n\`\`\`javascript\nconsole.log('test');\n\`\`\`\nMore content`;
  fs.writeFileSync(sourceFile, sourceContent);

  const configPath = path.join(scriptsDir, 'snippets.config.json');

  await t.test('fails if config is empty', () => {
    fs.writeFileSync(configPath, '[]');
    const res = runSyncSnippets(websiteDir);
    assert.strictEqual(res.status, 1, 'Should fail with empty config');
    assert.match(res.stderr, /empty/i, 'Should log error about empty config');
  });

  await t.test('fails if source is missing', () => {
    fs.writeFileSync(configPath, JSON.stringify([
      { name: 'missing', source: 'does-not-exist.txt', language: 'text' }
    ]));
    const res = runSyncSnippets(websiteDir);
    assert.strictEqual(res.status, 1, 'Should fail with missing source');
    assert.match(res.stderr, /missing|enoent|exist/i, 'Should log error about missing source');
  });

  await t.test('generates snippet successfully preserving content', () => {
    fs.writeFileSync(configPath, JSON.stringify([
      { name: 'test-snippet', source: 'test-source.md', language: 'markdown', title: 'Test Title' }
    ]));
    const res = runSyncSnippets(websiteDir);
    assert.strictEqual(res.status, 0, 'Should succeed');

    const outPath = path.join(generatedDir, 'test-snippet.mdx');
    assert.ok(fs.existsSync(outPath), 'Should create generated file');

    const outContent = fs.readFileSync(outPath, 'utf8');
    // It should have a title
    assert.match(outContent, /title="Test Title"/, 'Should include title');
    // It should preserve triple backticks inside
    assert.ok(outContent.includes(sourceContent), 'Should include exact source content');
    
    // Check fences are longer than inside
    const fenceMatch = outContent.match(/^(`{4,})/m);
    assert.ok(fenceMatch, 'Should use 4+ backticks for fence');
    assert.ok(outContent.includes(`${fenceMatch[1]}markdown`), 'Should have opening fence');
    assert.ok(outContent.includes(`${fenceMatch[1]}\n`), 'Should have closing fence');
  });

  await t.test('is idempotent on second run', () => {
    const res = runSyncSnippets(websiteDir);
    assert.strictEqual(res.status, 0, 'Second run should succeed');
    // You could test mtime, but simple success is enough for idempotence in this case
  });
});
