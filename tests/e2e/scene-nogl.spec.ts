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

test('every screen works on the fallback, wide and narrow', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  for (const size of [{ width: 1280, height: 720 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(size);
    await page.goto('/#sky');
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0, null, { timeout: 20_000 });
    const sample = await page.evaluate(() => (window as any).fluctlight.scene.sample());
    expect(sample.lit).toBeGreaterThan(0.0001);

    await page.locator('#query').click();
    await page.locator('#sheet .v').fill('testing');
    await expect(page.locator('#sheet .r').first()).toBeVisible();
    await page.locator('#sheet .r').first().click();
    await expect(page.locator('#drawer .nt')).toBeVisible();
    await expect(page.locator('#drawer .near')).toBeVisible();

    await page.evaluate(() => { location.hash = '#read'; });
    await expect(page.locator('#drawer .li').first()).toBeVisible();
    await page.evaluate(() => { location.hash = '#sys'; });
    await expect(page.locator('#sys .ov')).toBeVisible();
  }
  expect(errors).toEqual([]);
});
