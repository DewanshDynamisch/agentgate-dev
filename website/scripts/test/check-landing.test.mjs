import test from 'node:test';
import assert from 'node:assert/strict';
import { checkLanding } from '../check-landing.mjs';

test('check-landing parses correctly and fails appropriately', async (t) => {
    await t.test('passes valid html', () => {
        const html = `
        <!DOCTYPE html>
        <html>
        <head>
            <title>Valid Title</title>
            <meta name="description" content="Desc">
            <meta property="og:title" content="Valid Title">
            <meta property="og:description" content="Desc">
            <meta property="og:image" content="static/img/og.png">
        </head>
        <body>
            <h1>One H1</h1>
            <div id="hero"></div>
            <div id="problem"></div>
            <div id="how-it-works"></div>
            <div id="see-it-block"></div>
            <div id="console"></div>
            <div id="trust"></div>
            <div id="quickstart"></div>
            <a href="/docs">Docs</a>
            <img src="foo.png" alt="bar" />
        </body>
        </html>
        `;
        const errors = checkLanding(html);
        assert.deepEqual(errors, []);
    });

    await t.test('fails if not exactly one h1', () => {
        const html0 = `<title>T</title><meta name="description" content="D"><meta property="og:image" content="img"><div id="hero"></div><div id="problem"></div><div id="how-it-works"></div><div id="see-it-block"></div><div id="console"></div><div id="trust"></div><div id="quickstart"></div>`;
        assert.ok(checkLanding(html0).some(e => e.includes('exactly one <h1>')));

        const html2 = `<h1>A</h1><h1>B</h1>` + html0;
        assert.ok(checkLanding(html2).some(e => e.includes('exactly one <h1>')));
    });

    await t.test('fails if missing sections', () => {
        const html = `<title>T</title><meta name="description" content="D"><meta property="og:image" content="img"><h1>H1</h1><div id="hero"></div><div id="trust"></div>`;
        const errors = checkLanding(html);
        assert.ok(errors.some(e => e.includes('Missing section: problem')));
    });

    await t.test('fails if missing meta tags', () => {
        const html = `<h1>H1</h1><div id="hero"></div><div id="problem"></div><div id="how-it-works"></div><div id="see-it-block"></div><div id="console"></div><div id="trust"></div><div id="quickstart"></div>`;
        const errors = checkLanding(html);
        assert.ok(errors.some(e => e.includes('Missing <title>')));
        assert.ok(errors.some(e => e.includes('Missing meta description')));
        assert.ok(errors.some(e => e.includes('Missing Open Graph tags')));
    });

    await t.test('fails if <a> has empty or #-only href', () => {
        const html = `<title>T</title><meta name="description" content="D"><meta property="og:image" content="img"><h1>H1</h1><div id="hero"></div><div id="problem"></div><div id="how-it-works"></div><div id="see-it-block"></div><div id="console"></div><div id="trust"></div><div id="quickstart"></div>
        <a href="#">Bad</a><a href="">Worse</a>`;
        const errors = checkLanding(html);
        assert.ok(errors.some(e => e.includes('empty or #-only href')));
    });

    await t.test('fails if <img> without alt', () => {
        const html = `<title>T</title><meta name="description" content="D"><meta property="og:image" content="img"><h1>H1</h1><div id="hero"></div><div id="problem"></div><div id="how-it-works"></div><div id="see-it-block"></div><div id="console"></div><div id="trust"></div><div id="quickstart"></div>
        <img src="foo.png" />`;
        const errors = checkLanding(html);
        assert.ok(errors.some(e => e.includes('<img> without alt')));
    });
});
