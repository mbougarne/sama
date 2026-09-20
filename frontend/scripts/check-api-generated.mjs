import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const generator = resolve(
  frontendRoot,
  'node_modules/openapi-typescript/bin/cli.js',
);
const formatter = resolve(
  frontendRoot,
  'node_modules/prettier/bin/prettier.cjs',
);
const prettierConfig = resolve(frontendRoot, '.prettierrc.json');
const contract = resolve(frontendRoot, '../backend/api/openapi.yaml');
const checkedInTypes = resolve(frontendRoot, 'src/api/generated/openapi.ts');
const temporaryDirectory = mkdtempSync(join(tmpdir(), 'sama-contract-check-'));
const generatedTypes = join(temporaryDirectory, 'openapi.ts');

try {
  const result = spawnSync(
    process.execPath,
    [generator, contract, '-o', generatedTypes],
    {
      cwd: frontendRoot,
      encoding: 'utf8',
      maxBuffer: 4 * 1024 * 1024,
    },
  );
  if (result.status !== 0) {
    process.stderr.write('API type generation failed.\n');
    process.exitCode = 1;
  } else {
    const formatted = spawnSync(
      process.execPath,
      [formatter, '--config', prettierConfig, '--write', generatedTypes],
      {
        cwd: frontendRoot,
        encoding: 'utf8',
        maxBuffer: 4 * 1024 * 1024,
      },
    );
    if (formatted.status !== 0) {
      process.stderr.write('Generated API type formatting failed.\n');
      process.exitCode = 1;
    } else if (
      readFileSync(generatedTypes, 'utf8') !==
      readFileSync(checkedInTypes, 'utf8')
    ) {
      process.stderr.write(
        'Generated API types are stale; run pnpm run api:generate.\n',
      );
      process.exitCode = 1;
    }
  }
} catch {
  process.stderr.write('API type drift check could not read its inputs.\n');
  process.exitCode = 1;
} finally {
  rmSync(temporaryDirectory, { recursive: true, force: true });
}
