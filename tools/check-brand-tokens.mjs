import fs from 'fs';
import path from 'path';

const args = process.argv.slice(2);
const files = [];
const optionalFiles = new Set();
const allowMissingDark = new Set();

let parseMode = 'files';
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--files') {
    parseMode = 'files';
  } else if (args[i] === '--optional') {
    parseMode = 'optional';
  } else if (args[i] === '--allow-missing-dark') {
    parseMode = 'dark';
  } else {
    if (parseMode === 'files') files.push(args[i]);
    else if (parseMode === 'optional') optionalFiles.add(args[i]);
    else if (parseMode === 'dark') allowMissingDark.add(args[i]);
  }
}

const REQUIRED_TOKENS = [
  '--ag-bg-canvas', '--ag-bg-surface', '--ag-border', 
  '--ag-text-primary', '--ag-text-secondary', '--ag-text-inverse', 
  '--ag-accent', '--ag-accent-hover', '--ag-accent-soft', 
  '--ag-allow', '--ag-allow-soft', '--ag-deny', '--ag-deny-soft', 
  '--ag-candidate', '--ag-candidate-soft', '--ag-font-sans', '--ag-font-mono'
];

function extractTokens(css, blockSelector) {
  const cssNoComments = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const blockRegex = new RegExp(`${blockSelector.replace(/\[/g, '\\[').replace(/\]/g, '\\]')}\\s*\\{([^}]+)\\}`, 'm');
  const match = cssNoComments.match(blockRegex);
  if (!match) return null;
  const block = match[1];
  const tokens = {};
  for (const token of REQUIRED_TOKENS) {
    const tokenRegex = new RegExp(`${token}\\s*:\\s*([^;]+);`, 'm');
    const tokenMatch = block.match(tokenRegex);
    if (tokenMatch) {
      tokens[token] = tokenMatch[1].trim();
    }
  }
  return tokens;
}

let allFilesTokens = [];

for (const file of files) {
  if (!fs.existsSync(file)) {
    if (optionalFiles.has(file) || optionalFiles.has(path.basename(file))) {
      console.log(`Skipped: not present: ${file}`);
      continue;
    } else {
      console.error(`File not found: ${file}`);
      process.exit(1);
    }
  }

  const css = fs.readFileSync(file, 'utf8');
  const light = extractTokens(css, ':root');
  if (!light) {
    console.error(`Missing :root block in ${file}`);
    process.exit(1);
  }

  let dark = extractTokens(css, '[data-theme="dark"]');
  const isAllowedMissingDark = Array.from(allowMissingDark).some(allowPath => file.replace(/\\/g, '/').includes(allowPath.replace(/\\/g, '/')));

  if (!dark) {
    if (!isAllowedMissingDark) {
      console.error(`Missing [data-theme="dark"] block in ${file}`);
      process.exit(1);
    }
    dark = {};
  }

  allFilesTokens.push({ file, light, dark, isAllowedMissingDark });
}

if (allFilesTokens.length < 1) {
  console.error('Zero shared tokens found');
  process.exit(1);
}

// Check tokens across all files
const reference = allFilesTokens[0];
let foundAny = false;

for (const token of REQUIRED_TOKENS) {
  const refLight = reference.light[token];
  if (refLight) foundAny = true;
  else continue;
}

if (!foundAny) {
  console.error('Zero shared tokens found');
  process.exit(1);
}

for (const token of REQUIRED_TOKENS) {
    let hasToken = false;
    let expectedLight = null;
    let expectedDark = null;
    
    // Find first defined value
    for (const f of allFilesTokens) {
        if (f.light[token]) {
            expectedLight = f.light[token];
            hasToken = true;
            break;
        }
    }
    if (!hasToken) continue;

    for (const f of allFilesTokens) {
        if (!f.isAllowedMissingDark && f.dark[token]) {
            expectedDark = f.dark[token];
            break;
        }
    }

    for (const current of allFilesTokens) {
        if (current.light[token] !== expectedLight) {
            console.error(`Mismatch or missing light token ${token} in ${current.file}. Expected ${expectedLight} but found ${current.light[token]}`);
            process.exit(1);
        }
        
        if (!current.isAllowedMissingDark) {
            if (current.dark[token] !== expectedDark) {
                console.error(`Mismatch or missing dark token ${token} in ${current.file}. Expected ${expectedDark} but found ${current.dark[token]}`);
                process.exit(1);
            }
        }
    }
}

console.log('All tokens match!');
process.exit(0);
