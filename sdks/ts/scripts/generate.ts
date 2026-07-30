/**
 * SDK code generation script.
 *
 * Reads the canonical OpenAPI spec at `../../openapi/openapi.yaml` and
 * writes generated TypeScript types into `src/types/api.gen.ts`. The
 * generated file is committed to the repo so consumers (and IDEs) work
 * without running a build step.
 *
 * Usage:
 *   node --experimental-strip-types scripts/generate.ts
 *   # or
 *   pnpm generate
 *
 * Exit codes:
 *   0 – success
 *   1 – generation failed (logged with stack)
 *   2 – spec or output path missing
 */

import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import openapiTS, { astToString } from 'openapi-typescript';

const __dirname = dirname(fileURLToPath(import.meta.url));

const REPO_ROOT = resolve(__dirname, '..', '..', '..');
const SPEC_PATH = resolve(REPO_ROOT, 'openapi', 'openapi.yaml');
const OUT_PATH = resolve(__dirname, '..', 'src', 'types', 'api.gen.ts');

function writeGenerated(source: string): void {
  mkdirSync(dirname(OUT_PATH), { recursive: true });
  const header =
    `// AUTO-GENERATED FROM openapi/openapi.yaml — DO NOT EDIT.\n` +
    `// Regenerate via: pnpm run generate (sdks/ts)\n` +
    `// Source spec version tracked in openapi.yaml -> info.version\n\n`;
  writeFileSync(OUT_PATH, header + source + '\n', 'utf8');
  console.log(`[generate] wrote ${OUT_PATH} (${source.length} bytes)`);
}

async function main(): Promise<void> {
  // openapi-typescript v7+ loads YAML directly from a file:// URL.
  if (!existsSync(SPEC_PATH)) {
    console.error(`[generate] OpenAPI spec not found at ${SPEC_PATH}`);
    process.exit(2);
  }

  let ast;
  try {
    ast = await openapiTS(new URL(`file://${SPEC_PATH}`));
  } catch (err) {
    console.error('[generate] openapi-typescript failed:', err);
    process.exit(1);
  }

  try {
    writeGenerated(astToString(ast));
  } catch (err) {
    console.error('[generate] astToString/write failed:', err);
    process.exit(1);
  }
}

main().catch((err) => {
  console.error('[generate] unexpected error:', err);
  process.exit(1);
});
