import fs from 'node:fs';
import path from 'node:path';

const dir = process.argv[2] || 'build';

if (!fs.existsSync(dir)) {
  console.error('Directory does not exist:', dir);
  console.error('zero files found');
  process.exit(1);
}

const walkSync = (d, filelist = []) => {
  const files = fs.readdirSync(d);
  for (const file of files) {
    const dirFile = path.join(d, file);
    const dirent = fs.statSync(dirFile);
    if (dirent.isDirectory()) {
      filelist = walkSync(dirFile, filelist);
    } else {
      filelist.push(dirFile);
    }
  }
  return filelist;
};

const files = walkSync(dir).filter(f => /\.(html|css|js)$/.test(f));

if (files.length === 0) {
  console.error('zero files found in', dir);
  process.exit(1);
}

let hasError = false;

for (const file of files) {
  const content = fs.readFileSync(file, 'utf8');
  let match;

  const regexes = [
    /src=["']?(https?:)?\/\/[^"'>]+/gi,
    /<link[^>]+href=["']?(https?:)?\/\/[^"'>]+[^>]+rel=["']?(stylesheet|preload|icon)["']?/gi,
    /@import\s+url\(["']?(https?:)?\/\/[^"')]+/gi,
    /url\(["']?(https?:)?\/\/[^"')]+/gi,
    /fetch\(["']?(https?:)?\/\/[^"')]+/gi,
    /XMLHttpRequest\(["']?(https?:)?\/\/[^"')]+/gi
  ];

  for (const regex of regexes) {
    while ((match = regex.exec(content)) !== null) {
      console.error(`Found external resource in ${file}: ${match[0]}`);
      hasError = true;
    }
  }
}

if (hasError) {
  process.exit(1);
} else {
  console.log('check-offline passed.');
  process.exit(0);
}
