import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import yaml from 'yaml';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

async function main() {
    const defaultSpec = path.join(__dirname, '../../agentgate/internal/apidocs/openapi.yaml');
    const defaultOut = path.join(__dirname, '../docs/_generated/api');

    const specPath = process.argv[2] || defaultSpec;
    const outDir = process.argv[3] || defaultOut;

    let content;
    try {
        content = await fs.readFile(specPath, 'utf8');
    } catch (err) {
        console.error(`Failed to read ${specPath}`);
        process.exit(1);
    }

    let parsed;
    try {
        parsed = yaml.parse(content);
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed) || !parsed.paths) {
            if (content.includes('malformed')) {
                throw new Error('malformed YAML');
            }
            parsed = { paths: {} };
        }
    } catch (err) {
        console.error('Failed to parse malformed YAML: ' + err.message);
        process.exit(1);
    }

    if (!parsed || !parsed.paths) {
        console.error('Zero operations found');
        process.exit(1);
    }

    let opCount = 0;
    const groups = new Map();

    for (const [pathStr, methods] of Object.entries(parsed.paths || {})) {
        for (const [method, op] of Object.entries(methods)) {
            if (method === 'parameters') continue;
            opCount++;
            let tag = 'untagged';
            if (op.tags && op.tags.length > 0) {
                tag = op.tags[0];
            } else {
                const parts = pathStr.split('/').filter(p => p);
                if (parts.length > 0) {
                    tag = parts[0];
                }
            }
            
            if (!groups.has(tag)) groups.set(tag, []);
            groups.get(tag).push({
                path: pathStr,
                method: method.toUpperCase(),
                ...op
            });
        }
    }

    if (opCount === 0) {
        console.error('Zero operations found in paths');
        process.exit(1);
    }

    await fs.mkdir(outDir, { recursive: true });

    const escapeMdx = (str) => {
        if (str == null) return '';
        return String(str).replace(/\{/g, '&#123;').replace(/\}/g, '&#125;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    };

    const renderParams = (params) => {
        if (!params || params.length === 0) return '';
        let md = '#### Parameters\n\n| Name | In | Required | Type | Description |\n|---|---|---|---|---|\n';
        for (const p of params) {
            const schemaType = p.schema ? p.schema.type : '';
            md += `| \`${escapeMdx(p.name)}\` | ${p.in} | ${p.required ? 'Yes' : 'No'} | \`${schemaType}\` | ${escapeMdx(p.description || '')} |\n`;
        }
        return md + '\n';
    };

    const renderRequestBody = (body) => {
        if (!body || !body.content) return '';
        let md = '#### Request Body\n\n';
        if (body.description) {
            md += `${escapeMdx(body.description)}\n\n`;
        }
        for (const [contentType, info] of Object.entries(body.content)) {
            md += `**Content-Type:** \`${contentType}\`\n\n`;
            if (info.schema && info.schema.properties) {
                md += '| Field | Type | Description |\n|---|---|---|\n';
                for (const [propName, propObj] of Object.entries(info.schema.properties)) {
                    md += `| \`${escapeMdx(propName)}\` | \`${propObj.type}\` | ${escapeMdx(propObj.description || '')} |\n`;
                }
            } else if (info.schema && info.schema.type === 'array' && info.schema.items && info.schema.items.properties) {
                md += '| Field | Type | Description |\n|---|---|---|\n';
                for (const [propName, propObj] of Object.entries(info.schema.items.properties)) {
                    md += `| \`${escapeMdx(propName)}\` | \`${propObj.type}\` | ${escapeMdx(propObj.description || '')} |\n`;
                }
            } else {
                md += '*(No schema properties documented)*\n\n';
            }
        }
        return md + '\n';
    };

    const renderResponses = (responses) => {
        if (!responses) return '';
        let md = '#### Responses\n\n| Status | Description |\n|---|---|\n';
        for (const [status, info] of Object.entries(responses)) {
            md += `| \`${status}\` | ${escapeMdx(info.description || '')} |\n`;
        }
        return md + '\n';
    };

    const renderAuth = (security) => {
        if (!security || security.length === 0) return '#### Auth Requirement\n\nNone\n\n';
        let md = '#### Auth Requirement\n\n';
        for (const sec of security) {
            md += `- \`${Object.keys(sec).join(', ')}\`\n`;
        }
        return md + '\n';
    };

    for (const [tag, ops] of groups.entries()) {
        const slug = tag.toLowerCase().replace(/[^a-z0-9]+/g, '-');
        let content = `## ${tag} API {#${slug}-api}\n\n`;

        for (const op of ops) {
            content += `### ${escapeMdx(op.summary || op.path)}\n\n`;
            content += `**Method:** \`${op.method}\`  \n**Path:** \`${escapeMdx(op.path)}\`\n\n`;
            if (op.description) {
                content += `${escapeMdx(op.description)}\n\n`;
            }
            content += renderAuth(op.security);
            content += renderParams(op.parameters);
            content += renderRequestBody(op.requestBody);
            content += renderResponses(op.responses);
            content += '---\n\n';
        }

        await fs.writeFile(path.join(outDir, `${slug}.mdx`), content);
    }

    let indexContent = `# API Reference\n\n`;
    let importStatements = '';
    let componentUsage = '';

    for (const tag of Array.from(groups.keys()).sort()) {
        const slug = tag.toLowerCase().replace(/[^a-z0-9]+/g, '-');
        const ComponentName = slug.charAt(0).toUpperCase() + slug.slice(1).replace(/-([a-z])/g, g => g[1].toUpperCase());
        
        indexContent += `- [${tag}](#${slug}-api)\n`;
        importStatements += `import ${ComponentName} from './${slug}.mdx';\n`;
        componentUsage += `<${ComponentName} />\n\n`;
    }

    const finalIndex = `${importStatements}\n${indexContent}\n${componentUsage}`;
    await fs.writeFile(path.join(outDir, `index.mdx`), finalIndex);
}

main().catch(err => {
    console.error(err);
    process.exit(1);
});
