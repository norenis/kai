import { test, expect, Page } from '@playwright/test';
import { fullTurnWithoutLabelOverlap } from './labels-turn';

// The < 900 px layout, run by the phone (390×844) and tablet (768×1024)
// projects: bottom bar, full-screen sheet and drawer, one-column Sys.

async function ready(page: Page, hash = '') {
  await page.emulateMedia({ reducedMotion: 'reduce' }); // a still sphere: positions stay put
  await page.goto(`/${hash}`);
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.constellations?.length > 0);
}

// Nothing may stick out sideways: the page itself never scrolls horizontally.
async function noSideways(page: Page, sel = 'body') {
  const over = await page.locator(sel).evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(over).toBeLessThanOrEqual(0);
}

const box = (page: Page, sel: string) => page.locator(sel).evaluate((el) => {
  const r = el.getBoundingClientRect();
  return { x0: r.left, y0: r.top, x1: r.right, y1: r.bottom, w: r.width, h: r.height };
});

test('the rail is a bottom bar and the Sky fits the screen', async ({ page }) => {
  await ready(page);
  const vp = page.viewportSize()!;
  const rail = await box(page, '.rail');
  expect(rail.y1).toBeCloseTo(vp.height, 0);
  expect(rail.w).toBeCloseTo(vp.width, 0);
  expect(rail.h).toBeLessThan(80);
  const q = await box(page, '#query');
  expect(q.y1).toBeLessThan(rail.y0);
  expect(q.x0).toBeGreaterThanOrEqual(0);
  expect(q.x1).toBeLessThanOrEqual(vp.width);
  // Identity and inflow do not collide.
  const id = await box(page, '#identity');
  const inf = await box(page, '#inflow');
  expect(id.x1 <= inf.x0 || id.y1 <= inf.y0).toBe(true);
  expect(await page.evaluate(() => (window as any).fluctlight.scene.bloom)).toBe(false);
  await noSideways(page);
});

test('constellation labels never overlap or leave the screen over a full turn', async ({ page }) => {
  await ready(page);
  await fullTurnWithoutLabelOverlap(page);
});

test('a tap on a memory opens its note', async ({ page }) => {
  await ready(page);
  await page.waitForTimeout(400); // the camera has settled
  const at = await page.evaluate(() => {
    const s = (window as any).fluctlight.scene;
    return s.screenPosition(s.chunksOf('knowledge/table-driven-tests-in-go.md')[0]);
  });
  expect(at).toBeTruthy();
  await page.touchscreen.tap(at.x, at.y);
  await expect(page).toHaveURL(/#read\//);
  await expect(page.locator('#drawer .nt')).toBeVisible();
});

test('search opens as a full-screen sheet with a close button', async ({ page }) => {
  await ready(page);
  await page.locator('#query').click();
  const vp = page.viewportSize()!;
  const sheet = await box(page, '#sheet');
  const rail = await box(page, '.rail');
  expect(sheet.x0).toBe(0);
  expect(sheet.y0).toBe(0);
  expect(sheet.w).toBeCloseTo(vp.width, 0);
  expect(sheet.y1).toBeCloseTo(rail.y0, 0);
  await page.locator('#sheet .v').fill('testing');
  await expect(page.locator('#sheet .r').first()).toBeVisible();
  await noSideways(page, '#sheet');
  await page.locator('#sheet .x').click();
  await expect(page.locator('#sheet')).toBeHidden();
});

test('Read fills the screen, list and note alike', async ({ page }) => {
  await ready(page, '#read');
  const vp = page.viewportSize()!;
  const drawer = await box(page, '#drawer');
  expect(drawer.x0).toBe(0);
  expect(drawer.w).toBeCloseTo(vp.width, 0);
  await expect(page.locator('#query')).toBeHidden();
  await page.locator('#drawer .li').first().click();
  await expect(page.locator('#drawer .nt')).toBeVisible();
  await noSideways(page, '#drawer .nv');
  await page.locator('#drawer .crumb a').first().click();
  await expect(page).toHaveURL(/#read$/);
});

test('Sys collapses to one column with scrolling tabs', async ({ page }) => {
  await ready(page, '#sys');
  await expect(page.locator('#sys .ov')).toBeVisible();
  const cols = await page.locator('#sys .ov .row3').evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(' ').length);
  expect(cols).toBe(1);
  // The thumbnail gives way to the header on small screens.
  expect(await page.locator('#gl').evaluate((el) => getComputedStyle(el).opacity)).toBe('0');
  await noSideways(page, '#sys');
  const last = page.locator('#sys .tabs a').last();
  await last.scrollIntoViewIfNeeded();
  await last.click();
  await expect(page).toHaveURL(/#sys\/sync$/);
  await noSideways(page, '#sys');
});
