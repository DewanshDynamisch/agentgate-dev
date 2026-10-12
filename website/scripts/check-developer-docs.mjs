import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// Verifies the merged contributor docs (docs/developer/) in a built site:
// every source page is built, carries the navbar entry, and the two sidebars
// stay separate (user docs never list developer pages and vice versa).

export const NAV_LABEL = 'Architecture & Contributor Guide';
const DEV_PREFIX = '/docs/developer/';

function listFiles(dir, ext) {
    if (!fs.existsSync(dir)) return [];
    return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
        const p = path.join(dir, e.name);
        if (e.isDirectory()) return listFiles(p, ext);
        return e.name.endsWith(ext) ? [p] : [];
    });
}

function sidebarLinks(html) {
    const m = html.match(/<nav[^>]*aria-label="Docs sidebar"[^>]*>([\s\S]*?)<\/nav>/);
    if (!m) return [];
    return [...m[1].matchAll(/href="([^"]+)"/g)].map((x) => x[1].replace(/\/$/, ''));
}

export function checkDeveloperDocs(buildDir, sourceDir) {
    const errors = [];
    const sources = listFiles(sourceDir, '.md').concat(listFiles(sourceDir, '.mdx'));
    if (sources.length === 0) {
        return [`No developer docs found in ${sourceDir}`];
    }

    const navLabelHtml = NAV_LABEL.replace('&', '&amp;');
    for (const src of sources) {
        const rel = path.relative(sourceDir, src).replace(/\\/g, '/').replace(/\.mdx?$/, '');
        const page = path.join(buildDir, 'docs', 'developer', rel, 'index.html');
        if (!fs.existsSync(page)) {
            errors.push(`Developer page not built: ${rel}`);
            continue;
        }
        const html = fs.readFileSync(page, 'utf8');
        if (!html.includes(navLabelHtml)) {
            errors.push(`Developer page ${rel} is missing the "${NAV_LABEL}" navbar link`);
        }
        const userLinks = sidebarLinks(html).filter((l) => l.startsWith('/docs/') && !l.startsWith(DEV_PREFIX));
        if (userLinks.length > 0) {
            errors.push(`Developer sidebar on ${rel} lists user docs: ${userLinks.join(', ')}`);
        }
    }

    const docsBuild = path.join(buildDir, 'docs');
    for (const page of listFiles(docsBuild, 'index.html')) {
        const route = path.relative(buildDir, path.dirname(page)).replace(/\\/g, '/');
        if (route.startsWith('docs/developer')) continue;
        const devLinks = sidebarLinks(fs.readFileSync(page, 'utf8')).filter((l) => l.startsWith(DEV_PREFIX));
        if (devLinks.length > 0) {
            errors.push(`User docs sidebar on ${route} lists developer pages: ${devLinks.join(', ')}`);
        }
    }

    return errors;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
    const errors = checkDeveloperDocs(process.argv[2] || 'build', process.argv[3] || 'docs/developer');
    if (errors.length > 0) {
        console.error('Developer docs checks failed:');
        for (const e of errors) console.error(`  - ${e}`);
        process.exit(1);
    }
    console.log('Developer docs checks passed.');
}
