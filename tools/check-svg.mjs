#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import { parseArgs } from 'node:util';

function getFiles(targetPath) {
  if (!fs.existsSync(targetPath)) return [];
  const stat = fs.statSync(targetPath);
  if (stat.isFile() && targetPath.endsWith('.svg')) return [targetPath];
  if (stat.isDirectory()) {
    let results = [];
    const entries = fs.readdirSync(targetPath, { withFileTypes: true });
    for (const entry of entries) {
      const res = path.resolve(targetPath, entry.name);
      if (entry.isDirectory()) {
        results = results.concat(getFiles(res));
      } else if (entry.isFile() && res.endsWith('.svg')) {
        results.push(res);
      }
    }
    return results;
  }
  return [];
}

const args = process.argv.slice(2);
if (args.length === 0) {
  console.error("Usage: check-svg.mjs <file|dir> ...");
  process.exit(1);
}

let files = [];
for (const arg of args) {
  files = files.concat(getFiles(path.resolve(process.cwd(), arg)));
}

if (files.length === 0) {
  console.error("Error: empty file list or no SVG files found");
  process.exit(1);
}

let hasError = false;

function validateXml(xml) {
  const noQuotes = xml.replace(/"[^"]*"/g, '""').replace(/'[^']*'/g, "''");
  const selfClosing = /<[^>]*\/>/g;
  const tags = /<\/?([a-zA-Z0-9:-]+)[^>]*>/g;
  const xmlNoSelfClosing = noQuotes.replace(selfClosing, '');
  const stack = [];
  let match;
  while ((match = tags.exec(xmlNoSelfClosing)) !== null) {
    const isClosing = match[0].startsWith('</');
    const tagName = match[1];
    if (isClosing) {
      if (stack.length === 0 || stack[stack.length - 1] !== tagName) {
        return false;
      }
      stack.pop();
    } else {
      stack.push(tagName);
    }
  }
  return stack.length === 0;
}

for (const file of files) {
  const content = fs.readFileSync(file, 'utf8');
  if (content.trim() === '') {
    console.error(`[${file}] Error: file is empty`);
    hasError = true;
    continue;
  }

  if (!validateXml(content)) {
    console.error(`[${file}] Error: invalid XML`);
    hasError = true;
    continue;
  }

  if (!/<svg[^>]*\bviewBox\s*=/i.test(content)) {
    console.error(`[${file}] Error: missing viewBox attribute`);
    hasError = true;
  }
  
  if (!content.includes('<title>')) {
    console.error(`[${file}] Error: missing <title> element`);
    hasError = true;
  }

  if (/<script\b/i.test(content)) {
    console.error(`[${file}] Error: contains <script> element`);
    hasError = true;
  }

  if (/\bon[a-z]+\s*=/i.test(content)) {
    console.error(`[${file}] Error: contains on* event handler attribute`);
    hasError = true;
  }

  if (/(?:href|xlink:href)\s*=\s*["'](?:https?:\/\/|data:text\/html)/i.test(content) || /url\(\s*["']?(?:https?:\/\/|data:text\/html)/i.test(content)) {
    console.error(`[${file}] Error: contains external http/https or data:text/html reference`);
    hasError = true;
  }
}

if (hasError) {
  process.exit(1);
}
