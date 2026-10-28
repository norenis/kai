import { test, expect, Page } from '@playwright/test';

const fl = (page: Page, expr: string) => page.evaluate(expr);

async function openSys(page: Page, hash = '#sys') {
  await page.goto(`/${hash}`);
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
  await expect(page.locator('#sys .sum')).toContainText('memories');
}

test.describe('Sys overview', () => {
  test('numbers match the API and the oversized task file is flagged', async ({ page, request }) => {
    const vectors = await (await request.get('/api/vectors')).json();
    const files = (await (await request.get('/api/files')).json()).files;
    await openSys(page);
    const sys = page.locator('#sys');
    await expect(sys.locator('.cst').first()).toBeVisible();
    await expect(sys.locator('h2')).toHaveText('Maintenance');
    await expect(sys.locator('.tabs a.on')).toHaveText('Overview');
    await expect(sys.locator('.sum')).toContainText(`${files.length} notes`);
    await expect(sys.locator('.sum')).toContainText(`${vectors.nodes.length} memories`);

    await expect(sys.locator('.cst')).toHaveCount(vectors.clusters.length);
    await expect(sys.locator('.cst').first().locator('.n')).toHaveText(String(vectors.clusters[0].size));
    await expect(sys.locator('.kv').filter({ hasText: 'memories embedded' }).locator('.v')).toHaveText(String(vectors.nodes.length));
    await expect(sys.locator('.kv').filter({ hasText: 'links' }).locator('.v')).toHaveText(vectors.links.length.toLocaleString('en-US'));
    await expect(sys.locator('.dd')).toHaveCount(14);
    await expect(sys.locator('.tg span').first()).toBeVisible();

    // The demo brain has one daily task file far larger than the others.
    const flagged = sys.locator('tr.unusual');
    await expect(flagged).toHaveCount(1);
    await expect(flagged).toHaveAttribute('data-path', 'tasks/2026-10-19.md');
    await expect(flagged.locator('.flag')).toHaveText(/unusual/i);
  });

  test('the scene keeps running as a thumbnail and reacts to events', async ({ page }) => {
    await openSys(page);
    await page.waitForTimeout(1000); // the shrink transition
    expect(await fl(page, 'fluctlight.scene.view')).toBe('sys');
    expect(await fl(page, 'fluctlight.scene.running')).toBe(true);
    const box = await page.locator('#gl').boundingBox();
    expect(box!.width).toBeLessThan(400);

    const path = 'knowledge/dealing-with-aphids.md';
    const tool = (name: string, params: Record<string, string>) => page.request.post('/api/tools/execute', { data: { tool: name, params } });
    const read = await (await tool('brain_read', { path })).json();
    const note = JSON.parse(read.result);
    await tool('brain_write', { path, content: note.content + `\n\nSys edit ${Date.now()}.`, tags: note.tags.join(',') });
    await expect.poll(() => fl(page, 'fluctlight.scene.flowsActive'), { timeout: 5000 }).toBeGreaterThan(0);

    // Clicking the thumbnail returns to the Sky.
    await page.locator('#gl').click();
    await expect(page).toHaveURL(/#sky$/);
    await expect.poll(() => fl(page, 'fluctlight.scene.view')).toBe('sky');
  });

  test('tabs route and activity rows open notes', async ({ page }) => {
    await openSys(page, '#sys/tasks');
    await expect(page.locator('#sys .tabs a.on')).toHaveText('Tasks');
    await page.locator('#sys .tabs a[data-t="overview"]').click();
    await expect(page).toHaveURL(/#sys\/overview$/);
    const row = page.locator('#sys tr[data-path]').first();
    const path = await row.getAttribute('data-path');
    await row.click();
    await expect(page).toHaveURL(new RegExp(`#read/${encodeURIComponent(path!)}$`));
    await expect(page.locator('#sys')).toBeHidden();
  });
});
