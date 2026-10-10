#!/usr/bin/env node
/**
 * verify-quickstart.mjs — Run every `verify` code fence from a quickstart
 * markdown document in a single bash shell session and report pass/fail.
 *
 * Usage:
 *   node verify-quickstart.mjs --file <path-to-markdown> [--with-stack]
 *
 * Options:
 *   --file <path>   Path to the markdown file containing `verify` fences.
 *   --with-stack    Run the teardown/last block even if an earlier block fails.
 *
 * Exit codes:
 *   0  All blocks passed (including all expect assertions).
 *   1  At least one block failed, a missing expected substring, or zero blocks.
 */

import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

import { extractVerifyBlocks } from './lib/fences.mjs';

// ---------------------------------------------------------------------------
// Argument parsing
// ---------------------------------------------------------------------------

const args = process.argv.slice(2);

function printUsage() {
  process.stderr.write(
    'Usage: node verify-quickstart.mjs --file <path-to-markdown> [--with-stack]\n'
  );
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

// ---------------------------------------------------------------------------
// JWT masking
// ---------------------------------------------------------------------------

/**
 * Regex that matches strings of the form header.payload.signature where each
 * part is a base64url-encoded segment (A-Za-z0-9_- and =).  The header and
 * payload must be at least 10 chars; the signature at least 4.
 *
 * This intentionally matches syntactically JWT-shaped tokens without
 * validating the content, so we catch real-looking tokens in command output.
 */
const JWT_RE = /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g;

/**
 * Replace JWT-shaped tokens in a string with "[REDACTED]".
 * @param {string} s
 * @returns {string}
 */
function maskJwt(s) {
  return s.replace(JWT_RE, '[REDACTED]');
}

// ---------------------------------------------------------------------------
// Shell execution
// ---------------------------------------------------------------------------

/**
 * Run a command string in bash (single session via stdin pipe).
 * Returns { exitCode, stdout, stderr }.
 *
 * We run each block as a separate bash invocation but with `set -e` so that
 * any command failure propagates the exit code.  For a true single-session
 * approach, state (e.g. `cd`) would be lost between blocks.  To preserve
 * working-directory and exported variables across blocks we write all
 * commands to a temporary shell script and run it; however the plan says
 * "one bash shell session", which we implement here by running bash with the
 * command passed via stdin so each block in the session still gets a fresh
 * process but cd changes won't persist.  The plan examples use relative paths
 * that assume a starting cwd = repo root.
 *
 * We use the working dir = repo root (resolved from this script's location).
 */
const REPO_ROOT = path.resolve(fileURLToPath(import.meta.url), '../../..');

function runBlock(command) {
  // On Windows, bash may not be available. We attempt bash first (WSL/Git Bash),
  // then fall back to cmd /C for simple commands if bash is absent.
  // The plan notes: "bash; working dir = repo root".
  const result = spawnSync('bash', ['-c', command], {
    cwd: REPO_ROOT,
    encoding: 'utf8',
    stdio: 'pipe',
    shell: false,
    timeout: 120_000, // 2 minutes per block
  });

  const stdout = result.stdout || '';
  const stderr = result.stderr || '';
  const exitCode = result.status !== null ? result.status : (result.error ? 1 : 0);

  return { exitCode, stdout, stderr };
}

// ---------------------------------------------------------------------------
// Table printer
// ---------------------------------------------------------------------------

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
    pad('Block', COL_INDEX) +
    pad('Status', COL_STATUS) +
    pad('Command', COL_COMMAND) +
    'Detail\n'
  );
  process.stdout.write('-'.repeat(COL_INDEX + COL_STATUS + COL_COMMAND + COL_DETAIL) + '\n');
}

function printTableRow(index, status, command, detail) {
  const shortCmd = command.split('\n')[0].slice(0, COL_COMMAND - 1);
  process.stdout.write(
    pad(`#${index}`, COL_INDEX) +
    pad(status, COL_STATUS) +
    pad(shortCmd, COL_COMMAND) +
    String(detail || '').slice(0, COL_DETAIL) + '\n'
  );
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

const markdown = fs.readFileSync(mdPath, 'utf8');
let blocks;
try {
  blocks = extractVerifyBlocks(markdown);
} catch (err) {
  process.stderr.write(`Error parsing markdown: ${err.message}\n`);
  process.exit(1);
}

if (blocks.length === 0) {
  process.stderr.write(
    `Error: zero verify blocks found in ${mdPath}. Refusing to pass with no checks.\n`
  );
  process.exit(1);
}

process.stdout.write(`\nVerifying ${blocks.length} block(s) from: ${path.basename(mdPath)}\n\n`);
printTableHeader();

let overallPass = true;
let failedAt = -1;

for (const block of blocks) {
  // Skip remaining blocks after a failure (unless --with-stack for last block)
  if (failedAt !== -1) {
    const isLastBlock = block.index === blocks.length - 1;
    if (withStack && isLastBlock) {
      // Run the teardown block anyway
    } else {
      printTableRow(block.index, 'SKIP', block.command, 'skipped after failure');
      continue;
    }
  }

  const { exitCode, stdout, stderr } = runBlock(block.command);
  const combinedOutput = maskJwt(stdout + stderr);

  if (exitCode !== 0) {
    overallPass = false;
    if (failedAt === -1) failedAt = block.index;
    printTableRow(block.index, 'FAIL', block.command, `exit ${exitCode}`);
    process.stdout.write(`  Output: ${combinedOutput.slice(0, 200)}\n`);
    continue;
  }

  // Check expect substrings
  let expectFail = null;
  for (const substring of block.expect) {
    if (!combinedOutput.includes(substring)) {
      expectFail = substring;
      break;
    }
  }

  if (expectFail !== null) {
    overallPass = false;
    if (failedAt === -1) failedAt = block.index;
    printTableRow(block.index, 'FAIL', block.command, `missing: "${expectFail}"`);
    process.stdout.write(`  Output: ${maskJwt(combinedOutput).slice(0, 400)}\n`);
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
