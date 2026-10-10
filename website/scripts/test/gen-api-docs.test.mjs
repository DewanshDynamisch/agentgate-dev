import { test, describe, before, after } from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execSync } from 'node:child_process';
import yaml from 'yaml';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '../../../');
const SCRIPT = path.join(ROOT, 'website/scripts/gen-api-docs.mjs');

describe('gen-api-docs', () => {
    const tmpSpec = path.join(ROOT, 'website/scripts/test/fixtures/openapi.yaml');
    const outDir = path.join(ROOT, 'website/docs/_generated/api');
    const realSpec = path.join(ROOT, 'agentgate/internal/apidocs/openapi.yaml');

    before(async () => {
        await fs.mkdir(path.dirname(tmpSpec), { recursive: true });
        await fs.mkdir(outDir, { recursive: true });
    });

    after(async () => {
        await fs.rm(path.dirname(tmpSpec), { recursive: true, force: true });
    });

    test('fails on malformed YAML', async () => {
        await fs.writeFile(tmpSpec, 'malformed: { yaml: \n');
        assert.throws(() => {
            execSync(`node "${SCRIPT}" "${tmpSpec}" "${outDir}"`, { stdio: 'pipe' });
        });
    });

    test('fails on zero operations', async () => {
        await fs.writeFile(tmpSpec, `
openapi: 3.0.0
info:
  title: API
  version: 1.0.0
paths: {}
`);
        assert.throws(() => {
            execSync(`node "${SCRIPT}" "${tmpSpec}" "${outDir}"`, { stdio: 'pipe' });
        }, /zero operations/i);
    });

    test('generated operation count equals paths*methods and paths appear in generated files', async () => {
        // Clear outDir
        await fs.rm(outDir, { recursive: true, force: true });
        await fs.mkdir(outDir, { recursive: true });

        // Run against the real spec file instead of a hardcoded mock
        execSync(`node "${SCRIPT}" "${realSpec}" "${outDir}"`, { stdio: 'pipe' });

        const specContent = await fs.readFile(realSpec, 'utf8');
        const parsedYaml = yaml.parse(specContent);
        
        let expectedCount = 0;
        const expectedPaths = new Set();
        
        for (const [pathStr, methods] of Object.entries(parsedYaml.paths)) {
            for (const method of Object.keys(methods)) {
                if (method !== 'parameters') {
                    expectedCount++;
                    expectedPaths.add(pathStr);
                }
            }
        }

        const files = await fs.readdir(outDir);
        let actualCount = 0;
        let allOutput = '';

        for (const file of files) {
            if (file.endsWith('.mdx')) {
                const content = await fs.readFile(path.join(outDir, file), 'utf8');
                actualCount += (content.match(/\*\*Method:\*\*/g) || []).length;
                allOutput += content + '\n';
            }
        }
        
        assert.strictEqual(actualCount, expectedCount, 'Generated operation count must equal the number of (path, method) pairs in the YAML');

        for (const pathStr of expectedPaths) {
            const escapedPath = pathStr.replace(/\{/g, '&#123;').replace(/\}/g, '&#125;');
            assert.ok(allOutput.includes(escapedPath), `Path ${pathStr} should appear in the generated pages`);
        }
    });
});
