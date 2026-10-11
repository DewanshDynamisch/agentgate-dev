export function contrastRatio(fgHex: string, bgHex: string): number {
  const fg = hexToRgb(fgHex);
  const bg = hexToRgb(bgHex);
  const l1 = luminance(fg);
  const l2 = luminance(bg);
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
}

function hexToRgb(hex: string): [number, number, number] {
  let c = hex.replace(/^#/, '');
  if (c.length === 3) c = c.split('').map(x => x + x).join('');
  const num = parseInt(c, 16);
  return [(num >> 16) & 255, (num >> 8) & 255, num & 255];
}

function luminance(rgb: [number, number, number]): number {
  const [r, g, b] = rgb.map(v => {
    v /= 255;
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function parseTokens(css: string): Record<string, Record<string, string>> {
  const themes: Record<string, Record<string, string>> = { light: {}, dark: {} };
  
  // A naive parser that finds :root and [data-theme="dark"] blocks
  const rootMatch = css.match(/:root\s*{([^}]+)}/);
  const darkMatch = css.match(/\[data-theme="dark"\]\s*{([^}]+)}/);

  if (rootMatch) {
    extractVars(rootMatch[1], themes.light);
  }
  if (darkMatch) {
    extractVars(darkMatch[1], themes.dark);
  }

  return themes;
}

function extractVars(block: string, obj: Record<string, string>) {
  const regex = /(--ag-[\w-]+):\s*(#[0-9a-fA-F]{3,6});/g;
  let m;
  while ((m = regex.exec(block)) !== null) {
    obj[m[1]] = m[2];
  }
}
