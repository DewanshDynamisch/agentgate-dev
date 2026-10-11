import test from 'node:test';
import assert from 'node:assert';
import { execSync, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const verifierScript = path.resolve(__dirname, '../verify-quickstart.mjs');

/**
 * Runs the verifier CLI against a temporary markdown file.
 * Returns { code, stdout, stderr }.
 */
function runVerifier(markdownContent, extraArgs = []) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'vqs-test-'));
  const mdFile = path.join(tmp, 'quickstart.md');
  fs.writeFileSync(mdFile, markdownContent, 'utf8');
  const result = spawnSync(
    process.execPath,
    [verifierScript, '--file', mdFile, ...extraArgs],
    { encoding: 'utf8', stdio: 'pipe' }
  );
  fs.rmSync(tmp, { recursive: true, force: true });
  return {
    code: result.status,
    stdout: result.stdout || '',
    stderr: result.stderr || '',
    output: (result.stdout || '') + (result.stderr || ''),
  };
}

test('verify-quickstart — CLI behaviour', async (t) => {

  await t.test('zero verify blocks → exits non-zero', () => {
    const md = `
# Quickstart

Some text with no verify fences.

\`\`\`bash
echo "plain command not verify"
\`\`\`
`;
    const r = runVerifier(md);
    assert.notStrictEqual(r.code, 0, 'should fail when zero verify blocks found');
    assert.match(r.output, /zero|no verify|0 verify/i);
  });

  await t.test('failing command → exits non-zero and shows FAIL', () => {
    const md = `
\`\`\`bash verify
exit 42
\`\`\`
`;
    const r = runVerifier(md);
    assert.notStrictEqual(r.code, 0, 'should fail when a command exits non-zero');
    assert.match(r.output, /FAIL/i);
  });

  await t.test('passing command → exits zero and shows PASS', () => {
    const md = `
\`\`\`bash verify
echo "hello world"
\`\`\`
`;
    const r = runVerifier(md);
    assert.strictEqual(r.code, 0, `expected success but got code ${r.code}\n${r.output}`);
    assert.match(r.output, /PASS/i);
  });

  await t.test('missing expected substring → exits non-zero', () => {
    const md = `
\`\`\`bash verify
echo "actual output"
\`\`\`
\`\`\`text expect
this string is NOT in the output
\`\`\`
`;
    const r = runVerifier(md);
    assert.notStrictEqual(r.code, 0, 'should fail when expected substring is missing');
    assert.match(r.output, /FAIL/i);
  });

  await t.test('matching expected substring → exits zero', () => {
    const md = `
\`\`\`bash verify
echo "Tool Call Succeeded:"
\`\`\`
\`\`\`text expect
Tool Call Succeeded:
\`\`\`
`;
    const r = runVerifier(md);
    assert.strictEqual(r.code, 0, `expected success\n${r.output}`);
    assert.match(r.output, /PASS/i);
  });

  await t.test('multiple blocks — first failing block stops run, shows FAIL table', () => {
    const md = `
\`\`\`bash verify
echo "block one ok"
\`\`\`

\`\`\`bash verify
exit 1
\`\`\`

\`\`\`bash verify
echo "block three"
\`\`\`
`;
    const r = runVerifier(md);
    assert.notStrictEqual(r.code, 0);
    assert.match(r.output, /FAIL/i);
  });

  await t.test('JWT-shaped token is masked in output', () => {
    // A syntactically valid 3-part base64url JWT-shaped string
    // (header.payload.signature) — does not need to be a real signed JWT
    const fakeJwt = 'eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ0ZXN0IiwiaWF0IjoxNjAwMDAwMDAwfQ.AAAA_signature_BBBB';
    const md = `
\`\`\`bash verify
echo "Token: ${fakeJwt}"
\`\`\`
`;
    const r = runVerifier(md);
    // The JWT should be masked — the raw token should NOT appear in output
    assert.ok(
      !r.output.includes(fakeJwt),
      `JWT token should be masked but found in output:\n${r.output}`
    );
  });

  await t.test('prints per-block PASS/FAIL table', () => {
    const md = `
\`\`\`bash verify
echo "block one"
\`\`\`

\`\`\`bash verify
echo "block two"
\`\`\`
`;
    const r = runVerifier(md);
    assert.strictEqual(r.code, 0, `expected success\n${r.output}`);
    // Table should show block numbers
    assert.match(r.output, /block\s*[#:]?\s*[01]|#0|#1|\[0\]|\[1\]/i);
    assert.match(r.output, /PASS/i);
  });

  await t.test('--file flag is required; missing flag causes error', () => {
    const result = spawnSync(process.execPath, [verifierScript], {
      encoding: 'utf8',
      stdio: 'pipe',
    });
    assert.notStrictEqual(result.status, 0);
    assert.match(
      (result.stdout || '') + (result.stderr || ''),
      /--file|usage/i
    );
  });
});
