/**
 * Builds the E2E server once before any worker starts, so each worker can
 * launch its own copy without compiling it again. Set E2E_SERVER_BIN to use a
 * binary built elsewhere, or E2E_SERVER_URL to test a server that is already
 * running (one worker only; see fixtures.ts).
 */
import { execFileSync } from 'child_process';
import * as path from 'path';
import { fileURLToPath } from 'url';

export const repoRoot = fileURLToPath(new URL('../..', import.meta.url));
export const serverBinary = process.env.E2E_SERVER_BIN || path.join(repoRoot, 'node_modules/.cache/layout-manager-e2e/server');

export default function globalSetup() {
  if (process.env.E2E_SERVER_URL || process.env.E2E_SERVER_BIN) return;
  execFileSync('go', ['build', '-o', serverBinary, './test/e2e/server'], {
    cwd: repoRoot,
    env: { ...process.env, GOWORK: 'off' },
    stdio: 'inherit',
  });
}
