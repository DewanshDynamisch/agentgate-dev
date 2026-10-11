import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';

const args = process.argv.slice(2);
const runbookPath = args[0] || '../docs/OPERATIONS/RUNBOOK.md';
const outPath = args[1] || 'docs/_generated/limitations.mdx';

try {
    const content = readFileSync(runbookPath, 'utf8');
    const sectionMatch = content.match(/## \d+\. Known v1 Limitations\n([\s\S]*?)(\n## \d+\. |\n*$)/);

    if (!sectionMatch) {
        console.error('Error: "Known v1 Limitations" section not found in RUNBOOK.md');
        process.exit(1);
    }

    const sectionContent = sectionMatch[1];
    const items = sectionContent.match(/^\d+\. .*(\n\s+.*)*/gm);

    if (!items || items.length === 0) {
        console.error('Error: Zero limitation items found in "Known v1 Limitations" section');
        process.exit(1);
    }

    const outContent = `<!-- GENERATED FILE: DO NOT EDIT -->
${items.join('\n')}
`;

    mkdirSync(dirname(outPath), { recursive: true });
    writeFileSync(outPath, outContent);
} catch (e) {
    console.error(e);
    process.exit(1);
}
