import { test, expect } from '@playwright/test';
import { spawn, execFileSync, ChildProcess } from 'node:child_process';
import { cpSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// The PIN lock screen, served by a dashboard of its own with a PIN set.
const PORT = 9262;
const PIN = '1357';
let kai: ChildProcess, dir: string;

test.beforeAll(async () => {
  dir = mkdtempSync(join(tmpdir(), 'kai-lock-'));
  cpSync('tests/fixtures/demo-brain', join(dir, 'brain'), { recursive: true });
  writeFileSync(join(dir, 'config.yaml'), `agent:
  name: Demo
brain:
  path: ${dir}/brain
  embeddings:
    provider: lsi
sessions_path: ${dir}/sessions
server:
  listen_addr: 127.0.0.1:${PORT}
  dashboard_pin: "${PIN}"
`);
  execFileSync('./kai', ['--config', join(dir, 'config.yaml'), 'reindex']);
  kai = spawn('./kai', ['--config', join(dir, 'config.yaml'), 'dashboard', '--port', String(PORT), '--no-open'], { stdio: 'ignore' });
  for (let i = 0; i < 50; i++) {
    try {
      if ((await fetch(`http://127.0.0.1:${PORT}/api/auth`)).ok) break;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 200));
  }
});

test.afterAll(() => {
  kai?.kill();
  rmSync(dir, { recursive: true, force: true });
});

test.use({ baseURL: `http://127.0.0.1:${PORT}` });

test('the lock screen is themed, self-hosted and unlocks with the PIN', async ({ page, baseURL }) => {
  const external: string[] = [];
  page.on('request', (req) => { if (!req.url().startsWith(baseURL!)) external.push(req.url()); });
  await page.goto('/');
  await expect(page.locator('main.lockp h1')).toHaveText('Fluctlight');
  await expect(page.locator('.rail')).toHaveCount(0);
  expect(await page.evaluate(() => getComputedStyle(document.querySelector('h1')!).fontFamily)).toContain('Cormorant');

  await page.locator('#pin-input').fill('0000');
  await page.locator('#pin-input').press('Enter');
  await expect(page.locator('#pin-error')).toHaveText('Invalid PIN');

  await page.locator('#pin-input').fill(PIN);
  await page.locator('#pin-input').press('Enter');
  await expect(page.locator('.rail')).toBeVisible();
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
  expect(external).toEqual([]);
});
