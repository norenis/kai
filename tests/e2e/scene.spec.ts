import { test, expect, Page } from '@playwright/test';

async function waitForScene(page: Page) {
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0, null, { timeout: 20_000 });
}

test.describe('Sky scene', () => {
  test('renders every demo chunk into a non-blank canvas', async ({ page, request }) => {
    const vectors = await (await request.get('/api/vectors')).json();
    await page.goto('/');
    await waitForScene(page);
    await expect(page.locator('#gl canvas')).toBeVisible();
    const info = await page.evaluate(async () => {
      const s = (window as any).fluctlight.scene;
      return { kind: s.kind, count: s.count, sample: await s.sample() };
    });
    expect(info.kind).toBe('webgl');
    expect(info.count).toBe(vectors.nodes.length);
    expect(info.sample.lit).toBeGreaterThan(0.002);
  });

  test('stops rendering while the tab is hidden', async ({ page }) => {
    await page.goto('/');
    await waitForScene(page);
    const setHidden = (hidden: boolean) => page.evaluate((h) => {
      Object.defineProperty(document, 'hidden', { configurable: true, get: () => h });
      document.dispatchEvent(new Event('visibilitychange'));
    }, hidden);
    await setHidden(true);
    expect(await page.evaluate(() => (window as any).fluctlight.scene.running)).toBe(false);
    await setHidden(false);
    expect(await page.evaluate(() => (window as any).fluctlight.scene.running)).toBe(true);
  });

  test('scroll does not zoom the camera', async ({ page }) => {
    await page.goto('/');
    await waitForScene(page);
    await page.waitForTimeout(1500);
    const before = await page.evaluate(() => (window as any).fluctlight.scene.distance);
    await page.mouse.move(720, 450);
    await page.mouse.wheel(0, -800);
    await page.waitForTimeout(600);
    const after = await page.evaluate(() => (window as any).fluctlight.scene.distance);
    expect(Math.abs(after - before)).toBeLessThan(0.05);
  });

  test('drag orbits the camera', async ({ page }) => {
    await page.goto('/');
    await waitForScene(page);
    const angle = () => page.evaluate(() => {
      const c = (window as any).fluctlight.scene;
      return c.azimuth;
    });
    const a0 = await angle();
    await page.mouse.move(700, 450);
    await page.mouse.down();
    await page.mouse.move(950, 450, { steps: 8 });
    await page.mouse.up();
    await page.waitForTimeout(300);
    expect(Math.abs((await angle()) - a0)).toBeGreaterThan(0.2);
  });
});

test.describe('Sky scene with reduced motion', () => {
  test('disables auto-rotation', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/');
    await waitForScene(page);
    await page.waitForTimeout(300);
    const s = await page.evaluate(() => {
      const sc = (window as any).fluctlight.scene;
      return { reduced: sc.reducedMotion, rotating: sc.autoRotating };
    });
    expect(s).toEqual({ reduced: true, rotating: false });
  });
});
