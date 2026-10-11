import { describe, it } from 'node:test';
import assert from 'node:assert';
import fs from 'node:fs';
import path from 'node:path';
import { tmpdir } from 'node:os';
import { buildManifest } from '../lib/manifest.mjs';

describe('Media Manifest Builder', () => {
    it('passes a compliant fixture', () => {
        const tempDir = fs.mkdtempSync(path.join(tmpdir(), 'ag-media-test-'));
        fs.writeFileSync(path.join(tempDir, 'dashboard-light.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'dashboard-dark.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'audit-light.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'audit-dark.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'tools-light.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'tools-dark.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'policies-light.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'policies-dark.png'), Buffer.alloc(100 * 1024));
        fs.writeFileSync(path.join(tempDir, 'walkthrough.webm'), Buffer.alloc(4 * 1024 * 1024));

        const expectedFiles = [
            'dashboard-light.png', 'dashboard-dark.png',
            'audit-light.png', 'audit-dark.png',
            'tools-light.png', 'tools-dark.png',
            'policies-light.png', 'policies-dark.png',
            'walkthrough.webm'
        ];

        const manifest = buildManifest(tempDir, expectedFiles);
        assert.equal(manifest.files.length, 9);
        assert.ok(manifest.totalBytes < 15 * 1024 * 1024);
    });

    it('rejects missing files', () => {
        const tempDir = fs.mkdtempSync(path.join(tmpdir(), 'ag-media-test-'));
        assert.throws(() => {
            buildManifest(tempDir, ['missing.png']);
        }, /Missing file/);
    });

    it('rejects zero-byte files', () => {
        const tempDir = fs.mkdtempSync(path.join(tmpdir(), 'ag-media-test-'));
        fs.writeFileSync(path.join(tempDir, 'empty.png'), '');
        assert.throws(() => {
            buildManifest(tempDir, ['empty.png']);
        }, /Zero-byte file/);
    });

    it('rejects total size > 15 MB', () => {
        const tempDir = fs.mkdtempSync(path.join(tmpdir(), 'ag-media-test-'));
        fs.writeFileSync(path.join(tempDir, 'huge.png'), Buffer.alloc(16 * 1024 * 1024));
        assert.throws(() => {
            buildManifest(tempDir, ['huge.png']);
        }, /Total size exceeds 15 MB/);
    });

    it('rejects video > 8 MB', () => {
        const tempDir = fs.mkdtempSync(path.join(tmpdir(), 'ag-media-test-'));
        fs.writeFileSync(path.join(tempDir, 'walkthrough.webm'), Buffer.alloc(9 * 1024 * 1024));
        assert.throws(() => {
            buildManifest(tempDir, ['walkthrough.webm']);
        }, /Video exceeds 8 MB/);
    });
});
