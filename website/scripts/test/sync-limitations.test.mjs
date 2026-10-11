import { test } from 'node:test';
import assert from 'node:assert';
import { readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { randomBytes } from 'node:crypto';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const SCRIPT_PATH = fileURLToPath(new URL('../sync-limitations.mjs', import.meta.url));

test('sync-limitations script', async (t) => {
    await t.test('fails if runbook section is missing', () => {
        const tmpDir = join(tmpdir(), `ag-sync-lim-test-${randomBytes(4).toString('hex')}`);
        mkdirSync(tmpDir, { recursive: true });
        const runbookPath = join(tmpDir, 'RUNBOOK.md');
        const outPath = join(tmpDir, 'limitations.mdx');
        
        writeFileSync(runbookPath, '# Some other content\n\nNo limitations here.');
        
        assert.throws(() => {
            execSync(`node "${SCRIPT_PATH}" "${runbookPath}" "${outPath}"`, { stdio: 'pipe' });
        });
        
        rmSync(tmpDir, { recursive: true, force: true });
    });

    await t.test('fails if zero items in section', () => {
        const tmpDir = join(tmpdir(), `ag-sync-lim-test-${randomBytes(4).toString('hex')}`);
        mkdirSync(tmpDir, { recursive: true });
        const runbookPath = join(tmpDir, 'RUNBOOK.md');
        const outPath = join(tmpDir, 'limitations.mdx');
        
        writeFileSync(runbookPath, '## 12. Known v1 Limitations\n\nThere are no limitations.');
        
        assert.throws(() => {
            execSync(`node "${SCRIPT_PATH}" "${runbookPath}" "${outPath}"`, { stdio: 'pipe' });
        });
        
        rmSync(tmpDir, { recursive: true, force: true });
    });

    await t.test('renders items if section exists and has items', () => {
        const tmpDir = join(tmpdir(), `ag-sync-lim-test-${randomBytes(4).toString('hex')}`);
        mkdirSync(tmpDir, { recursive: true });
        const runbookPath = join(tmpDir, 'RUNBOOK.md');
        const outPath = join(tmpDir, 'limitations.mdx');
        
        writeFileSync(runbookPath, `## 12. Known v1 Limitations

1. **First item**: details
2. **Second item**: details
3. **Third item**: details
`);
        
        execSync(`node "${SCRIPT_PATH}" "${runbookPath}" "${outPath}"`, { stdio: 'pipe' });
        
        const outContent = readFileSync(outPath, 'utf8');
        assert.match(outContent, /1\. \*\*First item\*\*: details/);
        assert.match(outContent, /2\. \*\*Second item\*\*: details/);
        assert.match(outContent, /3\. \*\*Third item\*\*: details/);
        
        rmSync(tmpDir, { recursive: true, force: true });
    });
});
