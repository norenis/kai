import { test, expect, Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readFileSync, unlinkSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// Changes made by other processes reach the open dashboard through the
// file watcher (Checkpoint C).
const work = () => readFileSync(join(process.env.TMPDIR || tmpdir(), 'kai-e2e-9234.dir'), 'utf8').trim();

async function ready(page: Page) {
  await page.goto('/');
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
  await expect(page.locator('#inflow .ev').first()).toBeVisible();
  await page.waitForTimeout(300);
}

test('kai task add in another process shows in Inflow within 3 s', async ({ page }) => {
  await ready(page);
  const dir = work();
  execFileSync('./kai', ['--config', join(dir, 'config.yaml'), 'task', 'add', `External task ${Date.now()}`]);
  const first = page.locator('#inflow .ev').first();
  await expect(first.locator('.t')).toHaveText(/^\d{4}-\d{2}-\d{2}$/, { timeout: 3000 });
});

test('deleting a note file fades its memories and lists the removal', async ({ page }) => {
  await ready(page);
  const path = 'knowledge/street-photography-etiquette.md';
  const file = join(work(), 'brain', path);
  expect(existsSync(file)).toBe(true);
  const ids: string[] = await page.evaluate((p) => (window as any).fluctlight.scene.chunksOf(p), path);
  expect(ids.length).toBeGreaterThan(0);
  unlinkSync(file);
  const row = page.locator(`#inflow .ev[data-path="${path}"]`);
  await expect(row.locator('.sc')).toContainText('removed', { timeout: 3000 });
  await expect(row.locator('.t')).toHaveText('Street photography etiquette');
  const fade = (id: string) => page.evaluate((i) => (window as any).fluctlight.scene.fadeOf(i), id);
  // The fade starts within 3 s and runs to nothing.
  await expect.poll(() => fade(ids[0]), { timeout: 3000 }).toBeLessThan(0.9);
  await expect.poll(() => fade(ids[0]), { timeout: 15_000 }).toBe(0);
});
