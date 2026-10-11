import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const websiteDir = process.cwd();
const repoRootDir = path.resolve(websiteDir, '..');

const configPath = path.join(websiteDir, 'scripts', 'snippets.config.json');

if (!fs.existsSync(configPath)) {
  console.error(`Config file not found at ${configPath}`);
  process.exit(1);
}

let config;
try {
  config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
} catch (e) {
  console.error(`Error parsing config: ${e.message}`);
  process.exit(1);
}

if (!Array.isArray(config) || config.length === 0) {
  console.error('Config is empty');
  process.exit(1);
}

const outDir = path.join(websiteDir, 'docs', '_generated');
if (!fs.existsSync(outDir)) {
  fs.mkdirSync(outDir, { recursive: true });
}

for (const entry of config) {
  const { name, source, language, title } = entry;
  if (!name || !source || !language) {
    console.error(`Invalid entry in config: ${JSON.stringify(entry)}`);
    process.exit(1);
  }

  const sourcePath = path.resolve(repoRootDir, source);
  if (!fs.existsSync(sourcePath)) {
    console.error(`Source missing: ${sourcePath}`);
    process.exit(1);
  }

  let content;
  try {
    content = fs.readFileSync(sourcePath, 'utf8');
  } catch (e) {
    console.error(`Error reading source ${sourcePath}: ${e.message}`);
    process.exit(1);
  }

  // Determine fence length
  const backtickRuns = content.match(/`+/g) || [];
  const maxBackticks = Math.max(0, ...backtickRuns.map((run) => run.length));
  const fenceLength = Math.max(3, maxBackticks + 1);
  const fence = process.env.TEST_FORCE_FENCE || '`'.repeat(fenceLength);

  let mdxContent = `${fence}${language}`;
  if (title) {
    mdxContent += ` title="${title}"`;
  }
  mdxContent += `\n${content}\n${fence}\n`;

  const startIndex = mdxContent.indexOf('\n') + 1;
  const endIndex = mdxContent.indexOf('\n' + fence + '\n', startIndex);
  const stripped = mdxContent.substring(startIndex, endIndex);
  if (stripped !== content) {
    console.error(`Integrity check failed for ${name}: stripped content differs from source`);
    process.exit(1);
  }

  const outPath = path.join(outDir, `${name}.mdx`);
  let existingContent = '';
  if (fs.existsSync(outPath)) {
    existingContent = fs.readFileSync(outPath, 'utf8');
  }

  if (existingContent !== mdxContent) {
    fs.writeFileSync(outPath, mdxContent);
    console.log(`Generated ${name}.mdx`);
  } else {
    console.log(`Unchanged ${name}.mdx`);
  }
}
