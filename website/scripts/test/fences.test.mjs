import test from 'node:test';
import assert from 'node:assert';
import { extractVerifyBlocks } from '../lib/fences.mjs';

test('fences — extractVerifyBlocks', async (t) => {

  await t.test('returns empty array for empty input', () => {
    const result = extractVerifyBlocks('');
    assert.deepStrictEqual(result, []);
  });

  await t.test('returns empty array when no verify fences present', () => {
    const md = `
# Heading

\`\`\`bash
echo "plain block"
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.deepStrictEqual(result, []);
  });

  await t.test('parses a single verify block with no expect', () => {
    const md = `
\`\`\`bash verify
echo hello
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.strictEqual(result.length, 1);
    assert.strictEqual(result[0].index, 0);
    assert.strictEqual(result[0].command.trim(), 'echo hello');
    assert.deepStrictEqual(result[0].expect, []);
  });

  await t.test('parses a verify block followed by an expect fence', () => {
    const md = `
\`\`\`bash verify
echo "Tool Call Succeeded:"
\`\`\`
\`\`\`text expect
Tool Call Succeeded:
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.strictEqual(result.length, 1);
    assert.strictEqual(result[0].index, 0);
    assert.ok(result[0].command.includes('echo'));
    assert.deepStrictEqual(result[0].expect, ['Tool Call Succeeded:']);
  });

  await t.test('attaches multiple expect lines to the preceding block', () => {
    const md = `
\`\`\`bash verify
docker compose exec postgres psql -c "SELECT decision FROM audit"
\`\`\`
\`\`\`text expect
deny|policy_deny
allow|
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.strictEqual(result.length, 1);
    assert.deepStrictEqual(result[0].expect, ['deny|policy_deny', 'allow|']);
  });

  await t.test('parses multiple verify blocks and assigns correct indices', () => {
    const md = `
\`\`\`bash verify
echo first
\`\`\`
\`\`\`text expect
first
\`\`\`

\`\`\`bash verify
echo second
\`\`\`
\`\`\`text expect
second
\`\`\`

\`\`\`bash verify
echo third
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.strictEqual(result.length, 3);
    assert.strictEqual(result[0].index, 0);
    assert.deepStrictEqual(result[0].expect, ['first']);
    assert.strictEqual(result[1].index, 1);
    assert.deepStrictEqual(result[1].expect, ['second']);
    assert.strictEqual(result[2].index, 2);
    assert.deepStrictEqual(result[2].expect, []);
  });

  await t.test('throws on orphan expect fence with no preceding verify block', () => {
    const md = `
\`\`\`text expect
something
\`\`\`
`;
    assert.throws(
      () => extractVerifyBlocks(md),
      /orphan|no preceding|expect.*without/i
    );
  });

  await t.test('throws on orphan expect fence after a non-verify block', () => {
    const md = `
\`\`\`bash
echo "not a verify block"
\`\`\`
\`\`\`text expect
something
\`\`\`
`;
    assert.throws(
      () => extractVerifyBlocks(md),
      /orphan|no preceding|expect.*without/i
    );
  });

  await t.test('skips blank lines in expect content', () => {
    const md = `
\`\`\`bash verify
echo hi
\`\`\`
\`\`\`text expect

line-one

line-two

\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.deepStrictEqual(result[0].expect, ['line-one', 'line-two']);
  });

  await t.test('handles verify with extra words in info string', () => {
    const md = `
\`\`\`bash verify session=main
echo hi
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.strictEqual(result.length, 1);
    assert.ok(result[0].command.includes('echo hi'));
  });

  await t.test('preserves multi-line commands verbatim', () => {
    const md = `
\`\`\`bash verify
mkdir -p deploy/demo/secrets
docker run --rm alpine/openssl sh
\`\`\`
`;
    const result = extractVerifyBlocks(md);
    assert.strictEqual(result.length, 1);
    assert.ok(result[0].command.includes('mkdir'));
    assert.ok(result[0].command.includes('docker run'));
  });
});
