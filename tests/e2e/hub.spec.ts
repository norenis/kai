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
    // The event stream never goes idle; wait for the scene and fonts instead.
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(1000);
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

  test('/classic bookmarks land on the new UI', async ({ page }) => {
    await page.goto('/classic');
    await expect(page).toHaveURL(/\/$/);
    await expect(page.locator('.rail')).toBeVisible();
  });

  test('API endpoints respond', async ({ request }) => {
    for (const p of ['/api/overview', '/api/info', '/api/tags', '/api/files', '/api/status', '/api/sync/status', '/api/plugins', '/api/tools']) {
      expect((await request.get(p)).ok(), p).toBeTruthy();
    }
    expect(await (await request.get('/api/overview')).json()).toHaveProperty('total_files');
  });
});
