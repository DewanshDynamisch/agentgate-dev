import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { checkDeveloperDocs, NAV_LABEL } from '../check-developer-docs.mjs';

const NAV = `<nav class="navbar"><a href="/docs/intro">Docs</a><a href="/docs/developer/getting-started/overview">${NAV_LABEL.replace('&', '&amp;')}</a></nav>`;
const sidebar = (links) => `<nav aria-label="Docs sidebar" class="menu"><ul>${links.map(l => `<li><a href="${l}">x</a></li>`).join('')}</ul></nav>`;

function fixture({ sources = ['getting-started/overview.md', 'architecture/frontend.md'], pages = {} } = {}) {
    const root = mkdtempSync(join(tmpdir(), 'ag-devdocs-'));
    const src = join(root, 'docs', 'developer');
    const build = join(root, 'build');
    for (const s of sources) {
        mkdirSync(dirname(join(src, s)), { recursive: true });
        writeFileSync(join(src, s), '# x\n');
    }
    for (const [route, html] of Object.entries(pages)) {
        const file = join(build, route, 'index.html');
        mkdirSync(dirname(file), { recursive: true });
        writeFileSync(file, html);
    }
    mkdirSync(build, { recursive: true });
    return { root, src, build };
}

const goodPages = {
    'docs/intro': NAV + sidebar(['/docs/intro', '/docs/quickstart']),
    'docs/developer/getting-started/overview': NAV + sidebar(['/docs/developer/getting-started/overview', '/docs/developer/architecture/frontend']),
    'docs/developer/architecture/frontend': NAV + sidebar(['/docs/developer/getting-started/overview']),
};

test('check-developer-docs', async (t) => {
    await t.test('passes a correctly merged build', () => {
        const f = fixture({ pages: goodPages });
        assert.deepEqual(checkDeveloperDocs(f.build, f.src), []);
        rmSync(f.root, { recursive: true, force: true });
    });

    await t.test('fails when there are no developer source docs (no vacuous pass)', () => {
        const f = fixture({ sources: [], pages: goodPages });
        assert.ok(checkDeveloperDocs(f.build, f.src).some(e => /no developer docs/i.test(e)));
        rmSync(f.root, { recursive: true, force: true });
    });

    await t.test('fails when a source page was not built', () => {
        const pages = { ...goodPages };
        delete pages['docs/developer/architecture/frontend'];
        const f = fixture({ pages });
        assert.ok(checkDeveloperDocs(f.build, f.src).some(e => e.includes('architecture/frontend')));
        rmSync(f.root, { recursive: true, force: true });
    });

    await t.test('fails when a developer page lacks the navbar link', () => {
        const pages = { ...goodPages, 'docs/developer/architecture/frontend': sidebar(['/docs/developer/getting-started/overview']) };
        const f = fixture({ pages });
        assert.ok(checkDeveloperDocs(f.build, f.src).some(e => /navbar/i.test(e)));
        rmSync(f.root, { recursive: true, force: true });
    });

    await t.test('fails when the user Docs sidebar lists developer pages', () => {
        const pages = { ...goodPages, 'docs/intro': NAV + sidebar(['/docs/intro', '/docs/developer/architecture/frontend']) };
        const f = fixture({ pages });
        assert.ok(checkDeveloperDocs(f.build, f.src).some(e => e.includes('docs/intro') && /developer/.test(e)));
        rmSync(f.root, { recursive: true, force: true });
    });

    await t.test('fails when the developer sidebar lists user docs', () => {
        const pages = { ...goodPages, 'docs/developer/architecture/frontend': NAV + sidebar(['/docs/quickstart']) };
        const f = fixture({ pages });
        assert.ok(checkDeveloperDocs(f.build, f.src).some(e => e.includes('architecture/frontend') && /user doc/i.test(e)));
        rmSync(f.root, { recursive: true, force: true });
    });
});
