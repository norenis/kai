import { test, expect } from '@playwright/test';

test.describe('Fluctlight Hub shell', () => {
  test('root serves the new shell with the rail', async ({ page }) => {
    await page.goto('/');
    const rail = page.locator('nav.rail[aria-label="Main navigation"]');
    await expect(rail).toBeVisible();
    for (const label of ['SKY', 'READ', 'SYS']) {
      await expect(rail.getByText(label, { exact: true })).toBeVisible();
    }
    await expect(rail.locator('.ri.on')).toHaveAttribute('data-r', 'sky');
  });

  test('rail switches screens', async ({ page }) => {
    await page.goto('/');
    await page.locator('.ri[data-r="sys"]').click();
    await expect(page.locator('#stage')).toHaveAttribute('data-s', 'sys');
    await expect(page.locator('.ri.on')).toHaveAttribute('data-r', 'sys');
  });

  test('makes no third-party requests and loads all assets', async ({ page, baseURL }) => {
    const external: string[] = [];
    const failed: string[] = [];
    page.on('request', (req) => {
      if (!req.url().startsWith(baseURL!)) external.push(req.url());
    });
    page.on('requestfailed', (req) => failed.push(req.url()));
    page.on('response', (res) => { if (res.status() >= 400) failed.push(`${res.status()} ${res.url()}`); });
    await page.goto('/');
    await page.waitForLoadState('networkidle');
    await page.evaluate(() => document.fonts.ready);
    expect(external).toEqual([]);
    expect(failed).toEqual([]);
  });

  test('vendored modules resolve through the import map', async ({ page }) => {
    await page.goto('/');
    const versions = await page.evaluate(async () => {
      const three = await import('three');
      await import('three/addons/controls/OrbitControls.js');
      await import('three/addons/postprocessing/EffectComposer.js');
      await import('three/addons/postprocessing/RenderPass.js');
      await import('three/addons/postprocessing/UnrealBloomPass.js');
      await import('three/addons/postprocessing/OutputPass.js');
      const { marked } = await import('marked');
      return { three: three.REVISION, marked: typeof marked.parse };
    });
    expect(versions).toEqual({ three: '169', marked: 'function' });
  });

  test('self-hosted fonts load', async ({ page }) => {
    await page.goto('/');
    const ok = await page.evaluate(async () => {
      const faces = ['italic 500 20px "Cormorant Garamond"', '300 12px Montserrat', '400 14px Inter'];
      await Promise.all(faces.map((f) => document.fonts.load(f, 'Aă')));
      return faces.map((f) => document.fonts.check(f, 'Aă'));
    });
    expect(ok).toEqual([true, true, true]);
  });

  test('/classic still serves the old overview', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();
    await expect(page.locator('nav[aria-label="Main navigation"]').getByText('Overview')).toBeVisible();
    await expect(page.locator('#overview-heading')).toHaveText('Overview');
    await expect(page.locator('#stat-files')).toBeVisible();
  });
});
