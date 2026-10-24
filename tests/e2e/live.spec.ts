import { test, expect, Page } from '@playwright/test';

async function ready(page: Page) {
  await page.goto('/');
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
  // Inflow is seeded and the stream is open.
  await expect(page.locator('#inflow .ev').first()).toBeVisible();
  await page.waitForTimeout(300);
}

const tool = (page: Page, name: string, params: Record<string, string>) =>
  page.request.post('/api/tools/execute', { data: { tool: name, params } });

test.describe('Live inflow', () => {
  test('a note written through the API shows in Inflow within 3 s', async ({ page }) => {
    await ready(page);
    const text = `Inflow check ${Date.now()}`;
    await page.request.post('/api/tasks/add', { data: { text } });
    const first = page.locator('#inflow .ev').first();
    await expect(first.locator('.t')).toHaveText(/^\d{4}-\d{2}-\d{2}$/, { timeout: 3000 });
    await expect(first.locator('.sc')).toContainText(/learned|updated/);
  });

  test('an update to a known note plays a flow and glows its row on arrival', async ({ page }) => {
    await ready(page);
    const path = 'knowledge/hydration-and-dough-strength.md';
    const read = await (await tool(page, 'brain_read', { path })).json();
    const content = JSON.parse(read.result).content + `\n\nLive edit ${Date.now()}.`;
    await tool(page, 'brain_write', { path, content, tags: 'cooking,baking,bread' });
    await expect.poll(() => page.evaluate(() => (window as any).fluctlight.scene.flowsActive), { timeout: 3000 }).toBeGreaterThan(0);
    await expect(page.locator('#inflow .ev').first().locator('.t')).toHaveText('Hydration and dough strength');
    await expect(page.locator(`#inflow .ev.new[data-path="${path}"]`)).toHaveCount(1, { timeout: 8000 });
  });

  test('a brand-new note appears in the sky after the vectors refresh', async ({ page }) => {
    test.setTimeout(45_000);
    await ready(page);
    const path = `knowledge/live-note-${Date.now()}.md`;
    await tool(page, 'brain_write', { path, content: '# Live note\n\nA fresh note about comets and their tails seen through binoculars.', tags: 'astronomy' });
    await expect(page.locator('#inflow .ev').first().locator('.t')).toHaveText('Live note', { timeout: 15_000 });
    await expect.poll(() => page.evaluate((p) => (window as any).fluctlight.scene.has(`${p}#0`), path), { timeout: 30_000 }).toBe(true);
  });

  test('hovering an Inflow row locks its memory; clicking opens it', async ({ page }) => {
    await ready(page);
    const row = page.locator('#inflow .ev').filter({ hasText: /./ });
    // Find a row whose note is in the sky.
    const paths = await row.evaluateAll((els) => els.map((e) => (e as HTMLElement).dataset.path));
    const known = await page.evaluate((ps) => ps.find((p) => (window as any).fluctlight.scene.chunksOf(p).length > 0), paths);
    expect(known).toBeTruthy();
    const target = page.locator(`#inflow .ev[data-path="${known}"]`);
    await target.hover();
    await expect.poll(() => page.evaluate(() => (window as any).fluctlight.scene.locked)).toMatch(new RegExp(`^${known}#\\d+$`));
    await expect(page.locator('#labels .lock')).toContainText('locked · fragment');
    await expect(page.locator('#labels .lock')).toHaveCSS('opacity', '1');
    await target.click();
    await expect(page).toHaveURL(/#read$/);
    expect(await page.evaluate(() => (window as any).fluctlight.opened)).toBe(known);
  });

  test('hovering a memory in the sky locks it', async ({ page }) => {
    await ready(page);
    const pos = await page.evaluate(() => {
      const s = (window as any).fluctlight.scene;
      const id = s.chunksOf('knowledge/table-driven-tests-in-go.md')[0];
      return { id };
    });
    expect(pos.id).toBeTruthy();
    // Sweep the canvas until something is under the pointer.
    let locked = null;
    for (let y = 300; y < 600 && !locked; y += 12) {
      for (let x = 500; x < 950 && !locked; x += 12) {
        await page.mouse.move(x, y);
        locked = await page.evaluate(() => (window as any).fluctlight.scene.locked);
      }
    }
    expect(locked).toBeTruthy();
    await expect(page.locator('#labels .lock')).toContainText('locked · fragment');
  });
});

test.describe('Live inflow with reduced motion', () => {
  test('events glow without flows', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await ready(page);
    const path = 'knowledge/shaping-a-batard.md';
    const read = await (await tool(page, 'brain_read', { path })).json();
    await tool(page, 'brain_write', { path, content: JSON.parse(read.result).content + `\n\nCalm edit ${Date.now()}.`, tags: 'cooking,baking' });
    await expect(page.locator('#inflow .ev').first().locator('.t')).toHaveText('Shaping a batard', { timeout: 3000 });
    await page.waitForTimeout(300);
    expect(await page.evaluate(() => (window as any).fluctlight.scene.flowsActive)).toBe(0);
  });
});
