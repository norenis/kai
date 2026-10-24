import { test, expect, Page } from '@playwright/test';

async function openRead(page: Page) {
  await page.goto('/');
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
  await page.locator('.ri[data-r="read"]').click();
  await expect(page.locator('#drawer .li').first()).toBeVisible();
}

test.describe('Read list', () => {
  test('lists every note with titles, grouped by category', async ({ page, request }) => {
    const files = (await (await request.get('/api/files')).json()).files;
    await openRead(page);
    await expect(page.locator('#drawer .li')).toHaveCount(files.length);
    await expect(page.locator('#drawer .count')).toHaveText(`${files.length} notes`);
    await expect(page.locator('#drawer .gr').first()).toContainText('Knowledge');
    await expect(page.locator('#drawer .li[data-path="knowledge/table-driven-tests-in-go.md"] .t')).toHaveText('Table driven tests in Go');
    expect(await page.evaluate(() => (window as any).fluctlight.scene.view)).toBe('read');
  });

  test('filtering by a tag narrows the list', async ({ page, request }) => {
    const files = (await (await request.get('/api/files')).json()).files;
    const withTag = files.filter((f: any) => (f.tags || []).includes('photography')).length;
    expect(withTag).toBeGreaterThan(0);
    await openRead(page);
    await page.locator('#drawer .filter input').fill('photography');
    await expect(page.locator('#drawer .li')).toHaveCount(withTag);
    await expect(page.locator('#drawer .count')).toHaveText(`${withTag} of ${files.length} notes`);
  });

  test('constellation chips filter and sort cycles', async ({ page }) => {
    await openRead(page);
    const all = await page.locator('#drawer .li').count();
    await page.locator('#drawer .chips button[data-c="0"]').click();
    await expect(page.locator('#drawer .chips button[data-c="0"]')).toHaveClass(/on/);
    const one = await page.locator('#drawer .li').count();
    expect(one).toBeGreaterThan(0);
    expect(one).toBeLessThan(all);
    const numerals = await page.locator('#drawer .li .rn2').allTextContents();
    expect(new Set(numerals)).toEqual(new Set(['I']));

    await page.locator('#drawer .chips button[data-c=""]').click();
    await page.locator('#drawer .sort').click(); // recent → reads
    await expect(page.locator('#drawer .sort')).toContainText('reads');
    await page.locator('#drawer .sort').click(); // reads → A-Z
    const titles = await page.locator('#drawer .gr').first().locator('~ .li .t').allTextContents();
    const knowledge = titles.slice(0, 5);
    expect(knowledge).toEqual([...knowledge].sort((a, b) => a.localeCompare(b)));
  });

  test('hovering a row lights the note in the sky; esc returns to the Sky', async ({ page }) => {
    await openRead(page);
    await page.locator('#drawer .li[data-path="knowledge/table-driven-tests-in-go.md"]').hover();
    await expect.poll(() => page.evaluate(() => (window as any).fluctlight.scene.highlighted)).toBeGreaterThan(0);
    await page.mouse.move(300, 300);
    await expect.poll(() => page.evaluate(() => (window as any).fluctlight.scene.highlighted)).toBe(0);
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(/#sky$/);
    await expect(page.locator('#drawer')).toBeHidden();
  });
});
