/**
 * fences.mjs — Parse `verify` and `expect` fenced code blocks from markdown.
 *
 * Exported interface:
 *   extractVerifyBlocks(markdown: string) → Array<{index, command, expect[]}>
 *
 * Rules:
 *   - A fence whose info string contains the word "verify" is a verify block.
 *   - A fence whose info string contains the word "expect" is an expect block.
 *   - Each expect block is attached to the immediately preceding verify block
 *     (non-verify, non-expect fences between them are fine as long as no
 *     verify block appears between them).
 *   - An expect block with no preceding verify block in the current document
 *     throws an Error.
 *   - Blank lines in expect content are skipped (they are not substrings).
 */

/**
 * @typedef {{ index: number, command: string, expect: string[] }} VerifyBlock
 */

/**
 * Extract ordered list of verify blocks from a markdown string.
 *
 * @param {string} markdown
 * @returns {VerifyBlock[]}
 * @throws {Error} if an `expect` fence has no preceding `verify` block.
 */
export function extractVerifyBlocks(markdown) {
  if (!markdown) return [];

  const blocks = [];
  let verifyIndex = 0;
  // Track whether the last seen block was a verify block (so the next expect
  // can be attached to it).  null = no verify block seen yet / last block was
  // not a verify block.
  let pendingVerifyBlock = null;

  // Match fenced code blocks: opening fence with optional info string,
  // content, closing fence of the same or longer sequence of backticks.
  // We use a global regex over the whole document.
  const FENCE_RE = /^(`{3,}|~{3,})([^\n]*)\n([\s\S]*?)\n?\1\s*$/gm;

  for (const match of markdown.matchAll(FENCE_RE)) {
    const infoString = match[2].trim(); // e.g. "bash verify" or "text expect"
    const content = match[3];           // raw fence content

    const isVerify = /\bverify\b/.test(infoString);
    const isExpect = /\bexpect\b/.test(infoString);

    if (isVerify) {
      const block = {
        index: verifyIndex++,
        command: content,
        expect: [],
      };
      blocks.push(block);
      pendingVerifyBlock = block;
    } else if (isExpect) {
      if (pendingVerifyBlock === null) {
        throw new Error(
          'Orphan expect fence: found an `expect` fence with no preceding `verify` block.'
        );
      }
      // Attach non-blank lines as expected substrings
      const lines = content
        .split('\n')
        .map((l) => l.trimEnd())
        .filter((l) => l.length > 0);
      pendingVerifyBlock.expect.push(...lines);
      // An expect fence consumes the pending verify block — a second expect
      // after the same verify is still valid (append), but after a non-verify
      // block the pending is cleared below.
    } else {
      // Non-verify, non-expect fence: clear the pending verify so that a later
      // expect fence cannot attach to it across an unrelated fence.
      pendingVerifyBlock = null;
    }
  }

  return blocks;
}
