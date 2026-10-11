import fs from 'node:fs';
import path from 'node:path';

const args = process.argv.slice(2);
if (args.length === 0) {
  console.error('Usage: check-readme-links.mjs <file.md>');
  process.exit(1);
}

const mdFile = path.resolve(args[0]);
if (!fs.existsSync(mdFile)) {
  console.error('File not found: ' + mdFile);
  process.exit(1);
}

const content = fs.readFileSync(mdFile, 'utf8');

// Regex for [text](url) or ![text](url)
const linkRe = /\[[^\]]*\]\(([^)]+)\)/g;

let match;
let linksFound = 0;
let failed = false;

const mdDir = path.dirname(mdFile);

while ((match = linkRe.exec(content)) !== null) {
  let url = match[1];
  linksFound++;
  
  if (url.startsWith('http://') || url.startsWith('https://') || url.startsWith('mailto:')) {
    continue;
  }
  
  if (url.startsWith('#')) {
    continue; // internal anchor
  }
  
  // strip hash if present in relative link
  const hashIdx = url.indexOf('#');
  if (hashIdx !== -1) {
    url = url.slice(0, hashIdx);
  }
  
  const targetPath = path.resolve(mdDir, url);
  if (!fs.existsSync(targetPath)) {
    console.error('Link target not found: ' + url + ' (resolved to ' + targetPath + ')');
    failed = true;
  }
}

if (linksFound === 0) {
  console.error('Error: Zero links found in file.');
  process.exit(1);
}

if (failed) {
  process.exit(1);
} else {
  console.log('All links verified.');
  process.exit(0);
}

