import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

const stylesDir = path.resolve(__dirname, '../styles');

describe('CSS source checks', () => {
  it('components.css contains a prefers-reduced-motion block that disables transitions', () => {
    const css = fs.readFileSync(path.join(stylesDir, 'components.css'), 'utf8');
    expect(css).toMatch(/@media\s*\(\s*prefers-reduced-motion/);
    expect(css).toMatch(/transition\s*:\s*none/);
  });

  it('tokens.css or components.css contains a :focus-visible rule using the accent token', () => {
    const tokensCss = fs.readFileSync(path.join(stylesDir, 'tokens.css'), 'utf8');
    const componentsCss = fs.readFileSync(path.join(stylesDir, 'components.css'), 'utf8');
    const combined = tokensCss + componentsCss;
    expect(combined).toMatch(/:focus-visible/);
    expect(combined).toMatch(/var\(--ag-accent\)/);
  });

  it('tokens.css does NOT contain a fonts.googleapis.com reference', () => {
    const tokensCss = fs.readFileSync(path.join(stylesDir, 'tokens.css'), 'utf8');
    expect(tokensCss).not.toMatch(/fonts\.googleapis\.com/);
  });

  it('dist/ assets do NOT contain fonts.googleapis.com', () => {
    const distAssets = path.resolve(__dirname, '../../dist/assets');
    if (fs.existsSync(distAssets)) {
      const files = fs.readdirSync(distAssets).filter(f => f.endsWith('.css'));
      for (const f of files) {
        const content = fs.readFileSync(path.join(distAssets, f), 'utf8');
        expect(content).not.toMatch(/fonts\.googleapis\.com/);
      }
    }
  });
});

