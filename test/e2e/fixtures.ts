/**
 * Shared Playwright entry point. Import `test` and `expect` from here instead
 * of '@playwright/test'.
 *
 * - Every worker starts its own E2E server on a free port, so workers never
 *   share the server's state (render counts, the hooks "database", the block
 *   source's keys). Set E2E_SERVER_URL to use a running server (--workers=1).
 * - Every test starts from POST /e2e/reset. The test app signs each new
 *   browser context in as a user of its own, so it starts with no layout.
 * - Every test fails when a page in its context throws, reports a
 *   Content-Security-Policy violation or logs a console error, even if its
 *   own assertions passed. A test that causes one on purpose lists it with
 *   `test.use({ allowedPageProblems: [/pattern/] })`: one pattern, with |
 *   for alternatives, since Playwright reads a two-element array as
 *   [value, fixture options].
 */
import { test as base, expect } from '@playwright/test';
import { spawn, type ChildProcess } from 'child_process';
import * as http from 'http';
import * as https from 'https';
import * as net from 'net';
import { repoRoot, serverBinary } from './global-setup';

export * from '@playwright/test';

type Fixtures = {
  resetServerState: void;
  allowedPageProblems: RegExp[];
  failOnPageProblems: void;
};

type WorkerFixtures = {
  /** Serve HTTPS (self-signed), as WebKit's projects do. */
  e2eTLS: boolean;
  e2eServerURL: string;
};

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.unref();
    server.on('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address() as net.AddressInfo;
      server.close(() => resolve(port));
    });
  });
}

/** GET url's health check; a self-signed certificate is accepted. */
function healthy(url: string): Promise<boolean> {
  return new Promise(resolve => {
    const get = url.startsWith('https:') ? https.get : http.get;
    const request = get(`${url}/e2e/health`, { rejectUnauthorized: false }, response => {
      response.resume();
      resolve(response.statusCode === 204);
    });
    request.on('error', () => resolve(false));
  });
}

async function waitUntilServing(url: string, server: ChildProcess, output: () => string) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (server.exitCode !== null) throw new Error(`The E2E server exited with ${server.exitCode}:\n${output()}`);
    if (await healthy(url)) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`The E2E server did not answer at ${url} within 30 seconds:\n${output()}`);
}

export const test = base.extend<Fixtures, WorkerFixtures>({
  e2eTLS: [false, { scope: 'worker', option: true }],

  e2eServerURL: [
    async ({ e2eTLS }, use) => {
      if (process.env.E2E_SERVER_URL) {
        await use(process.env.E2E_SERVER_URL);
        return;
      }
      const port = await freePort();
      let output = '';
      const server = spawn(serverBinary, [], {
        cwd: repoRoot,
        env: { ...process.env, E2E_PORT: String(port), ...(e2eTLS ? { E2E_TLS: '1' } : {}) },
        stdio: ['ignore', 'pipe', 'pipe'],
      });
      const collect = (chunk: Buffer) => { output = (output + chunk.toString()).slice(-10_000); };
      server.stdout!.on('data', collect);
      server.stderr!.on('data', chunk => {
        collect(chunk);
        process.stderr.write(chunk);
      });
      // The port was found free on IPv4 only, so name that address.
      const url = `${e2eTLS ? 'https' : 'http'}://127.0.0.1:${port}`;
      try {
        await waitUntilServing(url, server, () => output);
        await use(url);
      } finally {
        if (server.exitCode === null) {
          const exited = new Promise(resolve => server.once('exit', resolve));
          server.kill();
          await exited;
        }
      }
    },
    { scope: 'worker', timeout: 60_000 },
  ],

  baseURL: async ({ e2eServerURL }, use) => {
    await use(e2eServerURL);
  },

  resetServerState: [
    async ({ request }, use) => {
      const response = await request.post('/e2e/reset');
      expect(response.status(), 'POST /e2e/reset').toBe(204);
      await use();
    },
    { auto: true },
  ],

  allowedPageProblems: [[], { option: true }],

  failOnPageProblems: [
    async ({ context, allowedPageProblems }, use, testInfo) => {
      const problems: string[] = [];
      const record = (problem: string) => {
        if (!allowedPageProblems.some(pattern => pattern.test(problem))) problems.push(problem);
      };

      context.on('weberror', error => record(`page error: ${error.error().stack || error.error().message}`));
      context.on('console', message => {
        if (message.type() === 'error') record(`console error: ${message.text()}`);
      });
      await context.exposeBinding('__e2eReportViolation', (_source, violation: string) => record(`CSP violation: ${violation}`));
      await context.addInitScript(() => {
        document.addEventListener('securitypolicyviolation', event => {
          const where = event.sourceFile ? ` at ${event.sourceFile}:${event.lineNumber}` : '';
          (window as any).__e2eReportViolation?.(`${event.violatedDirective} ${event.blockedURI || 'inline'}${where}`);
        });
      });

      await use();

      if (problems.length === 0) return;
      if (testInfo.status !== testInfo.expectedStatus || testInfo.expectedStatus === 'failed') {
        // The test already failed (or is expected to): keep its own error.
        await testInfo.attach('page problems', { body: problems.join('\n') });
        return;
      }
      throw new Error(`The page reported problems the test did not expect:\n${problems.join('\n')}`);
    },
    { auto: true },
  ],
});
