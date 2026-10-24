import { test, expect } from '@playwright/test';

// A browser without WebGL must still get a static sky and a working page.
test.use({ launchOptions: { args: ['--disable-webgl', '--disable-3d-apis'] } });

test('falls back to a static 2D render and the page still works', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto('/');
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0, null, { timeout: 20_000 });
  await expect(page.locator('#stage')).toHaveAttribute('data-render', 'fallback');
  const sample = await page.evaluate(() => (window as any).fluctlight.scene.sample());
  expect(sample.max).toBeGreaterThan(200);
  expect(sample.lit).toBeGreaterThan(0.0001);
  await page.locator('.ri[data-r="read"]').click();
  await expect(page.locator('#stage')).toHaveAttribute('data-s', 'read');
  expect(errors).toEqual([]);
});
