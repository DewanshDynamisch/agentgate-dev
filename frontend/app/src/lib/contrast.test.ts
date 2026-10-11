import { describe, it, expect } from 'vitest';
import { contrastRatio, parseTokens } from './contrast';
import fs from 'fs';
import path from 'path';

describe('contrast utilities', () => {
  it('computes known contrast ratios', () => {
    // black on white
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 1);
    // white on black
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 1);
    // same colour
    expect(contrastRatio('#123456', '#123456')).toBeCloseTo(1, 1);
  });

  it('parses real tokens.css and checks WCAG contrast', () => {
    const cssPath = path.resolve(__dirname, '../styles/tokens.css');
    const cssContent = fs.readFileSync(cssPath, 'utf8');
    const themes = parseTokens(cssContent);

    // We check both light and dark themes
    for (const [themeName, tokens] of Object.entries(themes)) {
      if (Object.keys(tokens).length === 0) continue; // skip if empty (e.g. no dark theme yet)

      const canvasBg = tokens['--ag-bg-canvas'];
      const surfaceBg = tokens['--ag-bg-surface'];
      
      const textPrimary = tokens['--ag-text-primary'];
      const textSecondary = tokens['--ag-text-secondary'];

      const textInverse = tokens['--ag-text-inverse'];
      const accent = tokens['--ag-accent'];

      const allowText = tokens['--ag-allow'];
      const allowBg = tokens['--ag-allow-soft'];
      
      const denyText = tokens['--ag-deny'];
      const denyBg = tokens['--ag-deny-soft'];

      const candidateText = tokens['--ag-candidate'];
      const candidateBg = tokens['--ag-candidate-soft'];

      const cases = [
        { fg: textPrimary, bg: canvasBg, name: 'primary text on canvas' },
        { fg: textPrimary, bg: surfaceBg, name: 'primary text on surface' },
        { fg: textSecondary, bg: canvasBg, name: 'secondary text on canvas' },
        { fg: textSecondary, bg: surfaceBg, name: 'secondary text on surface' },
        { fg: textInverse, bg: accent, name: 'inverse text on accent' },
        { fg: allowText, bg: allowBg, name: 'allow text on soft background' },
        { fg: denyText, bg: denyBg, name: 'deny text on soft background' },
        { fg: candidateText, bg: candidateBg, name: 'candidate text on soft background' },
      ];

      for (const { fg, bg, name } of cases) {
        if (!fg || !bg) {
          console.warn(`Missing token for ${name} in theme ${themeName}`);
          continue;
        }
        const ratio = contrastRatio(fg, bg);
        expect(ratio, `Contrast ratio for ${name} (${fg} on ${bg}) in theme ${themeName} is ${ratio}`).toBeGreaterThanOrEqual(4.5);
      }
    }
  });
});
