import { expect, Page } from '@playwright/test';

// fullTurnWithoutLabelOverlap drags the sphere through one full turn and,
// at every step, checks that the shown constellation labels stay on screen
// and clear of each other. Expects reduced motion (no damping drift).
export async function fullTurnWithoutLabelOverlap(page: Page) {
  const vp = page.viewportSize()!;
  const h = await page.locator('#gl').evaluate((el) => el.clientHeight);
  // OrbitControls turns 2π per canvas height dragged sideways.
  const steps = 16, dx = h / steps;
  const y = vp.height * 0.55;
  const azimuth = () => page.evaluate(() => (window as any).fluctlight.scene.azimuth as number);
  let seen = 0, turned = 0, prev = await azimuth();
  for (let i = 0; i <= steps; i++) {
    if (i) {
      await page.mouse.move(vp.width / 2 - dx / 2, y);
      await page.mouse.down();
      await page.mouse.move(vp.width / 2 + dx / 2, y, { steps: 4 });
      await page.mouse.up();
      const a = await azimuth();
      turned += Math.abs(Math.atan2(Math.sin(a - prev), Math.cos(a - prev)));
      prev = a;
    }
    await page.waitForTimeout(250);
    const rects = await page.$$eval('#labels .cl', (els) => els
      .filter((e) => parseFloat((e as HTMLElement).style.opacity) > 0)
      .map((e) => { const r = e.firstElementChild!.getBoundingClientRect(); return { x0: r.left, y0: r.top, x1: r.right, y1: r.bottom }; }));
    seen += rects.length;
    for (const r of rects) {
      expect(r.x0, `label off screen at step ${i}`).toBeGreaterThanOrEqual(0);
      expect(r.x1, `label off screen at step ${i}`).toBeLessThanOrEqual(vp.width);
    }
    for (let a = 0; a < rects.length; a++) for (let b = a + 1; b < rects.length; b++) {
      const p = rects[a], q = rects[b];
      expect(p.x1 <= q.x0 || q.x1 <= p.x0 || p.y1 <= q.y0 || q.y1 <= p.y0, `labels overlap at step ${i}`).toBe(true);
    }
  }
  expect(turned).toBeGreaterThan(Math.PI * 1.8); // it really went (about) all the way round
  expect(seen).toBeGreaterThan(steps); // and labels showed along the way
}
