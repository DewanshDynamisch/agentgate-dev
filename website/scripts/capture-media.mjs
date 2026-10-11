import { chromium } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildManifest } from './lib/manifest.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..');
const outDir = path.resolve(__dirname, '..', 'static', 'media');

if (!fs.existsSync(outDir)) {
    fs.mkdirSync(outDir, { recursive: true });
}

// Read admin password
const secretsPassPath = path.join(repoRoot, 'deploy', 'prod', 'secrets', 'admin_initial_password');
const adminPassword = fs.existsSync(secretsPassPath) ? fs.readFileSync(secretsPassPath, 'utf8').trim() : 'admin';

async function setupContextRouting(context) {
    await context.route('**/*', route => {
        const url = route.request().url();
        if (url.startsWith('https://keycloak:8443')) {
            const redirected = url.replace('https://keycloak:8443', 'https://localhost:8443');
            route.continue({ url: redirected });
            return;
        }
        route.continue();
    });
}

async function performLogin(page) {
    console.log('Navigating to Admin UI https://localhost:8444...');
    await page.goto('https://localhost:8444', { waitUntil: 'networkidle' }).catch(() => {});
    await page.waitForTimeout(1000);

    const ssoBtn = page.locator('button:has-text("Sign in with SSO")');
    if (await ssoBtn.isVisible()) {
        console.log('Clicking Sign in with SSO...');
        await ssoBtn.click();
    }

    const userField = page.locator('input[name="username"], input#username');
    try {
        await userField.waitFor({ timeout: 10000 });
    } catch (_) {}

    if (await userField.isVisible()) {
        console.log('Filling Keycloak credentials...');
        await userField.fill('admin');
        const passField = page.locator('input[name="password"], input#password');
        await passField.fill(adminPassword);
        const submitBtn = page.locator('input[type="submit"], input#kc-login, button[type="submit"]');
        await submitBtn.click();
        console.log('Submitted Keycloak form. Waiting for dashboard navigation...');
        await page.waitForURL('https://localhost:8444/**', { timeout: 15000 }).catch(() => {});
        await page.locator('.ag-page, h1').first().waitFor({ timeout: 15000 }).catch(() => {});
        await page.waitForTimeout(2000);
    }
}

async function capture() {
    console.log('Launching browser for screenshots...');
    const browser = await chromium.launch({ headless: true });
    
    // First context: images (1440x900)
    const context = await browser.newContext({
        viewport: { width: 1440, height: 900 },
        ignoreHTTPSErrors: true
    });
    await setupContextRouting(context);
    
    const page = await context.newPage();
    await performLogin(page);

    const pages = [
        { name: 'dashboard', path: '/' },
        { name: 'audit', path: '/audit' },
        { name: 'tools', path: '/tools' },
        { name: 'policies', path: '/policies' }
    ];

    for (const p of pages) {
        console.log(`Navigating to ${p.path}...`);
        await page.goto(`https://localhost:8444${p.path}`, { waitUntil: 'networkidle' }).catch(() => {});
        await page.waitForTimeout(2000);

        // Light mode
        console.log(`Capturing ${p.name} (light)...`);
        await page.evaluate(() => {
            try {
                localStorage.setItem('theme', 'light');
                document.documentElement.setAttribute('data-theme', 'light');
            } catch (_) {}
        });
        await page.waitForTimeout(500);
        await page.screenshot({ path: path.join(outDir, `${p.name}-light.png`) });

        // Dark mode
        console.log(`Capturing ${p.name} (dark)...`);
        await page.evaluate(() => {
            try {
                localStorage.setItem('theme', 'dark');
                document.documentElement.setAttribute('data-theme', 'dark');
            } catch (_) {}
        });
        await page.waitForTimeout(500);
        await page.screenshot({ path: path.join(outDir, `${p.name}-dark.png`) });
    }
    await context.close();

    // Second context: Video (1280x720)
    console.log('Starting walkthrough video recording (1280x720)...');
    const videoContext = await browser.newContext({
        viewport: { width: 1280, height: 720 },
        ignoreHTTPSErrors: true,
        recordVideo: {
            dir: outDir,
            size: { width: 1280, height: 720 }
        }
    });
    await setupContextRouting(videoContext);

    const vPage = await videoContext.newPage();
    await performLogin(vPage);

    // Set light theme
    await vPage.evaluate(() => {
        try {
            localStorage.setItem('theme', 'light');
            document.documentElement.setAttribute('data-theme', 'light');
        } catch (_) {}
    });

    // 1. Dashboard with live traffic
    console.log('Video step 1: Dashboard with live traffic...');
    await vPage.goto('https://localhost:8444/', { waitUntil: 'networkidle' }).catch(() => {});
    await vPage.waitForTimeout(10000);

    // 2. Audit page filtered to DENY
    console.log('Video step 2: Audit filtered to DENY...');
    await vPage.goto('https://localhost:8444/audit', { waitUntil: 'networkidle' }).catch(() => {});
    await vPage.waitForTimeout(2000);
    const decisionSelect = vPage.locator('label:has-text("Decision") select, select.ag-input').first();
    if (await decisionSelect.isVisible()) {
        await decisionSelect.selectOption('DENY');
        await vPage.waitForTimeout(8000);
    }

    // 3. Reclassify a tool in the Tools dialog
    console.log('Video step 3: Tools page reclassification...');
    await vPage.goto('https://localhost:8444/tools', { waitUntil: 'networkidle' }).catch(() => {});
    await vPage.waitForTimeout(2000);
    const editBtn = vPage.locator('button:has-text("Edit")').first();
    if (await editBtn.isVisible()) {
        await editBtn.click();
        await vPage.waitForTimeout(2000);
        const riskSelect = vPage.locator('label:has-text("Risk") select, select.ag-input').first();
        if (await riskSelect.isVisible()) {
            await riskSelect.selectOption('write');
            await vPage.waitForTimeout(1000);
        }
        const saveBtn = vPage.locator('button:has-text("Save")').first();
        if (await saveBtn.isVisible()) {
            await saveBtn.click();
            await vPage.waitForTimeout(3000);
        }
    }

    // 4. Back to audit page showing effect
    console.log('Video step 4: Back to audit showing effect...');
    await vPage.goto('https://localhost:8444/audit', { waitUntil: 'networkidle' }).catch(() => {});
    await vPage.waitForTimeout(10000);

    console.log('Closing video recording...');
    await videoContext.close();

    // Rename generated video to walkthrough.webm
    const files = fs.readdirSync(outDir);
    const videoFile = files.find(f => f.endsWith('.webm') && f !== 'walkthrough.webm');
    if (videoFile) {
        const src = path.join(outDir, videoFile);
        const dst = path.join(outDir, 'walkthrough.webm');
        if (fs.existsSync(dst)) {
            fs.unlinkSync(dst);
        }
        fs.renameSync(src, dst);
        console.log(`Renamed video ${videoFile} -> walkthrough.webm`);
    }

    await browser.close();

    // 5. Build and write manifest.json
    console.log('Generating media manifest.json...');
    const expectedFiles = [
        'dashboard-light.png', 'dashboard-dark.png',
        'audit-light.png', 'audit-dark.png',
        'tools-light.png', 'tools-dark.png',
        'policies-light.png', 'policies-dark.png',
        'walkthrough.webm'
    ];
    const manifest = buildManifest(outDir, expectedFiles);
    fs.writeFileSync(path.join(outDir, 'manifest.json'), JSON.stringify(manifest, null, 2));
    console.log('Capture media complete! Manifest successfully generated:');
    console.log(JSON.stringify(manifest, null, 2));
}

capture().catch(err => {
    console.error('Capture media failed:', err);
    process.exit(1);
});
