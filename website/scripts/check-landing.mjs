import fs from 'fs';
import path from 'path';

export function checkLanding(html) {
    const errors = [];

    const h1Count = (html.match(/<h1\b[^>]*>/g) || []).length;
    if (h1Count !== 1) {
        errors.push(`Expected exactly one <h1>, found ${h1Count}`);
    } else {
        if (!/<h1[^>]*id=["']hero-title["'][^>]*>/.test(html)) {
            errors.push(`<h1> is missing id="hero-title"`);
        }
    }

    if (!/class(Name)?=["'][^"']*ag-trust-badge[^"']*["']/.test(html)) {
        errors.push(`Missing ag-trust-badge`);
    }

    const requiredSections = ['hero', 'problem', 'how-it-works', 'see-it-block', 'console', 'trust', 'quickstart'];
    for (const section of requiredSections) {
        // match id="section" or id='section'
        const regex = new RegExp(`id=["']${section}["']`);
        if (!regex.test(html)) {
            errors.push(`Missing section: ${section}`);
        }
    }

    if (!/<title[^>]*>[\s\S]*?<\/title>/.test(html)) {
        errors.push('Missing <title>');
    }

    if (!/<meta[^>]+name=["']description["'][^>]*>/.test(html) && !/<meta[^>]+content=["'][^"']*["'][^>]*name=["']description["']/.test(html)) {
        errors.push('Missing meta description');
    }

    // Need to check for og:image, maybe others too if we assume Open Graph tags broadly, let's just check og:image at least
    if (!/<meta[^>]+property=["']og:image["'][^>]*>/.test(html) && !/<meta[^>]+content=["'][^"']*["'][^>]*property=["']og:image["']/.test(html)) {
        errors.push('Missing Open Graph tags');
    }

    // Check for a tags with empty or # only href
    const aRegex = /<a\b[^>]*>/gi;
    let match;
    while ((match = aRegex.exec(html)) !== null) {
        const aTag = match[0];
        const hrefMatch = aTag.match(/href=["'](.*?)["']/);
        if (!hrefMatch || hrefMatch[1].trim() === '' || hrefMatch[1].trim() === '#') {
            errors.push(`<a> with empty or #-only href: ${aTag}`);
        }
    }

    // Check for img without alt
    const imgRegex = /<img\b[^>]*>/gi;
    while ((match = imgRegex.exec(html)) !== null) {
        const imgTag = match[0];
        if (!/alt=["']/.test(imgTag)) {
            errors.push(`<img> without alt: ${imgTag}`);
        }
    }

    return errors;
}

import { fileURLToPath } from 'url';

// at top of file add:
const isMain = process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1]);

if (isMain) {
    const htmlPath = path.resolve('build', 'index.html');
    if (!fs.existsSync(htmlPath)) {
        console.error(`File not found: ${htmlPath}`);
        process.exit(1);
    }
    const html = fs.readFileSync(htmlPath, 'utf8');
    const errors = checkLanding(html);
    if (errors.length > 0) {
        console.error('Landing page checks failed:');
        errors.forEach(e => console.error(`- ${e}`));
        process.exit(1);
    }
    console.log('Landing page checks passed.');
}
