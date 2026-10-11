import fs from 'node:fs';
import path from 'node:path';

export function buildManifest(dir, expectedFiles) {
    const files = [];
    let totalBytes = 0;

    for (const filename of expectedFiles) {
        const filepath = path.join(dir, filename);
        if (!fs.existsSync(filepath)) {
            throw new Error(`Missing file: ${filename}`);
        }
        
        const stats = fs.statSync(filepath);
        if (stats.size === 0) {
            throw new Error(`Zero-byte file: ${filename}`);
        }

        if (filename.endsWith('.webm') && stats.size > 8 * 1024 * 1024) {
            throw new Error(`Video exceeds 8 MB: ${filename} (${stats.size} bytes)`);
        }

        files.push({
            name: filename,
            bytes: stats.size
        });
        totalBytes += stats.size;
    }

    if (totalBytes > 15 * 1024 * 1024) {
        throw new Error(`Total size exceeds 15 MB: ${totalBytes} bytes`);
    }

    return {
        files,
        totalBytes
    };
}
