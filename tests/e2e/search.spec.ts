import { test, expect, Page } from '@playwright/test';

async function ready(page: Page) {
  await page.goto('/');
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
}

const view = (page: Page) => page.evaluate(() => (window as any).fluctlight.scene.view);

test.describe('⌘K search sheet', () => {
  test('/ opens the sheet; typing shows results and the recall trace', async ({ page }) => {
    await ready(page);
    await page.keyboard.press('/');
    const sheet = page.locator('#sheet');
    await expect(sheet).toBeVisible();
    await expect(sheet.locator('input')).toBeFocused();
    await page.keyboard.type('sourdough starter');
    await expect(sheet.locator('.r').first()).toBeVisible();
    await expect(sheet.locator('.r .t').first()).not.toBeEmpty();
    await expect(sheet.locator('.r .sn mark').first()).toBeVisible();
    await expect(sheet.locator('.tr')).toHaveText([/embed/i, /bm25/i, /vector/i, /prf/i, /fuse/i]);
    await expect(sheet.locator('.tr.on')).toHaveCount(5);
    await expect(sheet.locator('.meta')).toContainText(/\d+ memories · \d+ notes/);
    expect(await view(page)).toBe('search');
    // Recall fibres flow while the sheet is open.
    await expect.poll(() => page.evaluate(() => (window as any).fluctlight.scene.flowsActive)).toBeGreaterThan(0);
  });

  test('↓ then ↵ opens the selected note; esc restores the Sky', async ({ page }) => {
    await ready(page);
    await page.keyboard.press('Control+k');
    await page.keyboard.type('telescope collimation mirrors');
    const rows = page.locator('#sheet .r');
    await expect(rows.nth(1)).toBeVisible();
    await page.keyboard.press('ArrowDown');
    await expect(rows.nth(1)).toHaveClass(/sel/);
    const path = await rows.nth(1).getAttribute('data-path');
    await page.keyboard.press('Enter');
    await expect(page.locator('#sheet')).toBeHidden();
    await expect(page).toHaveURL(/#read$/);
    expect(await page.evaluate(() => (window as any).fluctlight.opened)).toBe(path);

    await page.goto('/#sky');
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
    await page.keyboard.press('/');
    await page.keyboard.type('budget');
    await expect(rows.first()).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('#sheet')).toBeHidden();
    expect(await view(page)).toBe('sky');
    expect(await page.evaluate(() => (window as any).fluctlight.scene.flowsActive)).toBe(0);
  });

  test('ask is disabled without an LLM', async ({ page }) => {
    await ready(page);
    await page.locator('#query').click();
    await expect(page.locator('#sheet .ask')).toHaveClass(/dis/);
    await expect(page.locator('#sheet .ask')).toContainText('no llm configured');
  });

  test('only constellations holding hits keep their labels', async ({ page }) => {
    await ready(page);
    await page.keyboard.press('/');
    await page.keyboard.type('collimating newtonian telescope');
    await expect(page.locator('#sheet .r').first()).toBeVisible();
    const clusters: number[] = await page.evaluate(() => [...new Set((window as any).fluctlight.search.results.map((r: any) => r.cluster))] as number[]);
    await page.waitForTimeout(800);
    const shown = await page.$$eval('#labels .cl', (els) =>
      els.filter((e) => parseFloat(getComputedStyle(e).opacity) > 0.05).map((e) => Number((e as HTMLElement).dataset.cluster)));
    for (const c of shown) expect(clusters).toContain(c);
  });

  test('matched terms are underlined without breaking markup', async ({ page }) => {
    await ready(page);
    const html = await page.evaluate(async () => {
      const { markTerms } = await import('app/cmdk.js');
      return [markTerms('Nấu phở <b>bò</b> tại nhà', 'phở bò'), markTerms('a & b', 'a')];
    });
    expect(html[0]).toBe('Nấu <mark>phở</mark> &lt;b&gt;<mark>bò</mark>&lt;/b&gt; tại nhà');
    expect(html[1]).toBe('a &amp; b');
  });
});
