#!/usr/bin/env node
/**
 * verify-quickstart.mjs - Run every `verify` code fence from a quickstart
 * markdown document in a single bash shell session and report pass/fail.
 */

import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import os from 'node:os';

import { extractVerifyBlocks } from './lib/fences.mjs';

const args = process.argv.slice(2);

function printUsage() {
  process.stderr.write('Usage: node verify-quickstart.mjs --file <path> [--with-stack]\n');
}

const fileIndex = args.indexOf('--file');
if (fileIndex === -1 || !args[fileIndex + 1]) {
  printUsage();
  process.exit(1);
}

const mdPath = path.resolve(args[fileIndex + 1]);
const withStack = args.includes('--with-stack');

if (!fs.existsSync(mdPath)) {
  process.stderr.write(`Error: file not found: ${mdPath}\n`);
  process.exit(1);
}

const JWT_RE = /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g;

function maskJwt(s) {
  return s.replace(JWT_RE, '[REDACTED]');
}

const REPO_ROOT = path.resolve(fileURLToPath(import.meta.url), '../../..');

const COL_INDEX = 8;
const COL_STATUS = 8;
const COL_COMMAND = 40;
const COL_DETAIL = 30;

function pad(s, n) {
  const str = String(s);
  return str.length >= n ? str.slice(0, n) : str + ' '.repeat(n - str.length);
}

function printTableHeader() {
  process.stdout.write(
    pad('Block', COL_INDEX) + pad('Status', COL_STATUS) + pad('Command', COL_COMMAND) + 'Detail\n'
  );
  process.stdout.write('-'.repeat(COL_INDEX + COL_STATUS + COL_COMMAND + COL_DETAIL) + '\n');
}

function printTableRow(index, status, command, detail) {
  const shortCmd = command.split('\n')[0].slice(0, COL_COMMAND - 1);
  process.stdout.write(
    pad(`#${index}`, COL_INDEX) + pad(status, COL_STATUS) + pad(shortCmd, COL_COMMAND) + String(detail || '').slice(0, COL_DETAIL) + '\n'
  );
}

const markdown = fs.readFileSync(mdPath, 'utf8');
let blocks;
try {
  blocks = extractVerifyBlocks(markdown);
} catch (err) {
  process.stderr.write(`Error parsing markdown: ${err.message}\n`);
  process.exit(1);
}

if (blocks.length === 0) {
  process.stderr.write(`Error: zero verify blocks found in ${mdPath}. Refusing to pass with no checks.\n`);
  process.exit(1);
}

process.stdout.write(`\nVerifying ${blocks.length} block(s) from: ${path.basename(mdPath)}\n\n`);
printTableHeader();

let overallPass = true;
let failedAt = -1;

const tmpDir = path.join(REPO_ROOT, '.ag-verify-tmp');
if (!fs.existsSync(tmpDir)) fs.mkdirSync(tmpDir, { recursive: true });

let scriptContent = '#!/bin/bash\nset -e\nexport GIT_TERMINAL_PROMPT=0\nexport GCM_INTERACTIVE=false\nexport GIT_ASKPASS=echo\nexport GCM_PROVIDER=empty\ngo() { go.exe "$@"; }\nexport -f go\nAG_VERIFY_DIR="$(pwd)/.ag-verify-tmp"\n';

for (const block of blocks) {
  const cmd = block.command.replace(/\r/g, '');
  scriptContent += `
echo ":::AG_BLOCK ${block.index}"
{
${cmd}
} > "$AG_VERIFY_DIR/out${block.index}" 2>&1
echo ":::AG_EXIT ${block.index} \$?"
`;
}

const runnerScript = path.join(tmpDir, 'runner.sh');
fs.writeFileSync(runnerScript, scriptContent);
const result = spawnSync('bash', ['.ag-verify-tmp/runner.sh'], {
  cwd: REPO_ROOT,
  encoding: 'utf8',
  stdio: ['ignore', 'pipe', 'pipe'],
  shell: false,
  timeout: 120_000,
});

const runnerStdout = result.stdout || '';
console.log("==> RUNNER STDOUT START\\n" + runnerStdout + "\\n==> RUNNER STDOUT END");

const startedBlocks = new Set();
const finishedBlocks = new Map();

for (const line of runnerStdout.split('\n')) {
  if (line.startsWith(':::AG_BLOCK ')) {
    startedBlocks.add(parseInt(line.split(' ')[1]));
  }
  if (line.startsWith(':::AG_EXIT ')) {
    const parts = line.split(' ');
    finishedBlocks.set(parseInt(parts[1]), parseInt(parts[2]));
  }
}

for (const block of blocks) {
  if (failedAt !== -1) {
    const isLastBlock = block.index === blocks.length - 1;
    if (withStack && isLastBlock) {
      // Need to run the teardown block separately because the script aborted
      const teardownCmd = block.command.replace(/\r\n/g, '\n');
      const tdRes = spawnSync('bash', ['-c', teardownCmd], {
        cwd: REPO_ROOT,
        encoding: 'utf8',
        stdio: 'pipe',
        shell: false
      });
      const combinedOutput = maskJwt((tdRes.stdout||'') + (tdRes.stderr||''));
      if (tdRes.status !== 0) {
        overallPass = false;
        printTableRow(block.index, 'FAIL', block.command, `exit ${tdRes.status}`);
      } else {
        printTableRow(block.index, 'PASS', block.command, '');
      }
      continue;
    } else {
      printTableRow(block.index, 'SKIP', block.command, 'skipped after failure');
      continue;
    }
  }

  const outPath = path.join(tmpDir, `out${block.index}`);
  let combinedOutput = '';
  if (fs.existsSync(outPath)) {
    combinedOutput = fs.readFileSync(outPath, 'utf8');
  }
  combinedOutput = maskJwt(combinedOutput);

  if (!startedBlocks.has(block.index)) {
    overallPass = false;
    failedAt = block.index;
    printTableRow(block.index, 'FAIL', block.command, `did not run`);
    continue;
  }

  let exitCode = 1;
  if (finishedBlocks.has(block.index)) {
    exitCode = finishedBlocks.get(block.index);
  }

  if (exitCode !== 0) {
    overallPass = false;
    failedAt = block.index;
    printTableRow(block.index, 'FAIL', block.command, `exit ${exitCode}`);
    process.stdout.write(`  Output: ${combinedOutput.slice(0, 200)}\n`);
    continue;
  }

  let expectFail = null;
  for (const substring of block.expect) {
    if (!combinedOutput.includes(substring)) {
      expectFail = substring;
      break;
    }
  }

  if (expectFail !== null) {
    overallPass = false;
    failedAt = block.index;
    printTableRow(block.index, 'FAIL', block.command, `missing: "${expectFail}"`);
    process.stdout.write(`  Output: ${combinedOutput.slice(0, 400)}\n`);
    continue;
  }

  printTableRow(block.index, 'PASS', block.command, '');
}

process.stdout.write('\n');

if (overallPass) {
  process.stdout.write(`All ${blocks.length} block(s) PASSED.\n`);
  process.exit(0);
} else {
  process.stdout.write(`FAILED: one or more blocks did not pass.\n`);
  process.exit(1);
}
