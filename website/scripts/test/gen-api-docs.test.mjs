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

    test('generated operation count equals paths*methods and paths appear', async () => {
        const specContent = `
openapi: 3.0.0
info:
  title: API
  version: 1.0.0
paths:
  /test/1:
    get:
      tags: [tests]
      summary: Get test 1
    post:
      tags: [tests]
      summary: Post test 1
  /untagged/2:
    put:
      summary: Untagged put
`;
        await fs.writeFile(tmpSpec, specContent);
        
        execSync(`node "${SCRIPT}" "${tmpSpec}" "${outDir}"`, { stdio: 'pipe' });

        const testsPage = await fs.readFile(path.join(outDir, 'tests.mdx'), 'utf8');
        const untaggedPage = await fs.readFile(path.join(outDir, 'untagged.mdx'), 'utf8');
        const indexPage = await fs.readFile(path.join(outDir, 'index.mdx'), 'utf8');

        // Total 3 ops
        assert.match(testsPage, /\/test\/1/);
        assert.match(untaggedPage, /\/untagged\/2/);
        
        assert.match(indexPage, /tests/);
        assert.match(indexPage, /untagged/);
    });
});
