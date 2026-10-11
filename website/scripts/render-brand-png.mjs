import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..');
const staticImgDir = path.resolve(__dirname, '..', 'static', 'img');

if (!fs.existsSync(staticImgDir)) {
    fs.mkdirSync(staticImgDir, { recursive: true });
}

async function renderBrandPng() {
    const browser = await chromium.launch();
    const page = await browser.newPage();

    // 1. Render favicon-32.png (32x32)
    const faviconSvgPath = path.join(repoRoot, 'brand', 'favicon.svg');
    const faviconSvg = fs.readFileSync(faviconSvgPath, 'utf8');

    await page.setViewportSize({ width: 32, height: 32 });
    await page.setContent(`
        <!DOCTYPE html>
        <html>
        <head>
            <style>
                body { margin: 0; padding: 0; display: flex; width: 32px; height: 32px; background: transparent; }
                svg { width: 32px; height: 32px; }
            </style>
        </head>
        <body>${faviconSvg}</body>
        </html>
    `);
    const faviconDest = path.join(staticImgDir, 'favicon-32.png');
    await page.screenshot({ path: faviconDest, omitBackground: true });
    console.log(`Rendered ${faviconDest}`);

    // 2. Render og.png (1200x630)
    await page.setViewportSize({ width: 1200, height: 630 });
    const ogHtml = `
        <!DOCTYPE html>
        <html>
        <head>
            <meta charset="utf-8">
            <style>
                @import url('https://fonts.googleapis.com/css2?family=Geist:wght@400;600;700&family=Geist+Mono&display=swap');
                * { box-sizing: border-box; margin: 0; padding: 0; }
                body {
                    width: 1200px;
                    height: 630px;
                    background-color: #09090b;
                    color: #f4f4f5;
                    font-family: 'Geist', system-ui, -apple-system, sans-serif;
                    display: flex;
                    flex-direction: column;
                    justify-content: space-between;
                    padding: 80px 96px;
                    position: relative;
                    overflow: hidden;
                }
                .glow {
                    position: absolute;
                    width: 600px;
                    height: 600px;
                    background: radial-gradient(circle, rgba(99, 102, 241, 0.15) 0%, rgba(9, 9, 11, 0) 70%);
                    top: -150px;
                    right: -100px;
                    pointer-events: none;
                }
                .grid-bg {
                    position: absolute;
                    inset: 0;
                    background-image: linear-gradient(rgba(255, 255, 255, 0.03) 1px, transparent 1px),
                                      linear-gradient(90deg, rgba(255, 255, 255, 0.03) 1px, transparent 1px);
                    background-size: 40px 40px;
                    pointer-events: none;
                }
                .header {
                    display: flex;
                    align-items: center;
                    gap: 20px;
                    z-index: 1;
                }
                .mark {
                    width: 56px;
                    height: 56px;
                }
                .brand-title {
                    font-size: 36px;
                    font-weight: 700;
                    letter-spacing: -0.02em;
                    color: #ffffff;
                }
                .badge {
                    font-size: 14px;
                    font-weight: 600;
                    padding: 6px 14px;
                    border-radius: 9999px;
                    background: rgba(99, 102, 241, 0.2);
                    color: #818cf8;
                    border: 1px solid rgba(99, 102, 241, 0.4);
                    margin-left: 12px;
                    text-transform: uppercase;
                    letter-spacing: 0.05em;
                }
                .content {
                    z-index: 1;
                    max-width: 900px;
                }
                .headline {
                    font-size: 56px;
                    font-weight: 700;
                    line-height: 1.15;
                    letter-spacing: -0.03em;
                    color: #ffffff;
                    margin-bottom: 24px;
                }
                .headline span {
                    color: #818cf8;
                }
                .tagline {
                    font-size: 26px;
                    line-height: 1.45;
                    color: #a1a1aa;
                    font-weight: 400;
                }
                .footer {
                    display: flex;
                    align-items: center;
                    gap: 32px;
                    z-index: 1;
                    border-top: 1px solid #27272a;
                    padding-top: 28px;
                }
                .pill {
                    display: flex;
                    align-items: center;
                    gap: 10px;
                    font-size: 16px;
                    color: #d4d4d8;
                    font-weight: 500;
                }
                .dot {
                    width: 8px;
                    height: 8px;
                    border-radius: 50%;
                    background: #4ade80;
                }
            </style>
        </head>
        <body>
            <div class="grid-bg"></div>
            <div class="glow"></div>
            
            <div class="header">
                <svg class="mark" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
                    <rect x="12" y="12" width="40" height="40" rx="8" fill="none" stroke="#818cf8" stroke-width="4"/>
                    <path d="M12 32h16M36 32h16" stroke="#818cf8" stroke-width="4" stroke-linecap="round"/>
                    <circle cx="32" cy="32" r="8" fill="#818cf8"/>
                </svg>
                <div class="brand-title">AgentGate</div>
                <div class="badge">Control Plane</div>
            </div>

            <div class="content">
                <h1 class="headline">A checkpoint in the <span>only path</span> between an AI agent and its tools.</h1>
                <p class="tagline">Deterministic identity, schema drift protection, real-time Cedar authorization, and cryptographic audit logging.</p>
            </div>

            <div class="footer">
                <div class="pill"><div class="dot"></div>Deterministic Security Gate</div>
                <div class="pill"><div class="dot"></div>Tamper-Evident Audit Trail</div>
                <div class="pill"><div class="dot"></div>Zero-Trust Agent Auth</div>
            </div>
        </body>
        </html>
    `;

    await page.setContent(ogHtml);
    // Wait for fonts or render
    await page.waitForTimeout(500);
    const ogDest = path.join(staticImgDir, 'og.png');
    await page.screenshot({ path: ogDest });
    console.log(`Rendered ${ogDest}`);

    await browser.close();
}

renderBrandPng().catch(err => {
    console.error('Failed to render brand PNGs:', err);
    process.exit(1);
});
