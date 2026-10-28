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

test.describe('Sys actions', () => {
  test('tasks: add one to today and tick it', async ({ page }) => {
    await openSys(page, '#sys/tasks');
    const tasks = page.locator('#sys .tasks');
    await expect(tasks.locator('.th')).toBeVisible();
    const text = `Stake the tomatoes ${Date.now()}`;
    await tasks.locator('.pri').click(); // no priority → high
    await tasks.locator('.add input').fill(text);
    await tasks.locator('.add input').press('Enter');
    const row = tasks.locator('.tk', { hasText: text });
    await expect(row).toBeEnabled();
    await expect(row).toHaveClass(/p-high/);
    await expect(tasks.locator('.th')).toContainText('today');
    await row.click();
    await expect(row).toHaveClass(/done/);
    await expect(row).toHaveAttribute('aria-pressed', 'true');
  });

  test('index: decay dry run reports, rebuild needs confirmation, reindex runs', async ({ page }) => {
    await openSys(page, '#sys/index');
    const ix = page.locator('#sys .ix');
    await expect(ix.locator('.kv').first()).toBeVisible();

    await ix.locator('button.lk[data-a="decay"]').click();
    await expect(ix.locator('.out')).toContainText(/nothing to decay|decay would change/i);

    await ix.locator('button.lk[data-a="rebuild"]').click();
    await expect(ix.locator('.confirm')).toContainText('Re-embed all');
    await ix.locator('.confirm .no').click();
    await expect(ix.locator('.confirm')).toHaveCount(0);
    await expect(ix.locator('button.lk[data-a="rebuild"]')).toBeVisible();

    await ix.locator('button.lk[data-a="reindex"]').click();
    await expect(ix.locator('.out')).toContainText('index up to date', { timeout: 30_000 });
  });

  test('overview action links run in the Index tab', async ({ page }) => {
    await openSys(page);
    await page.locator('#sys .ov a[data-act="decay"]').click();
    await expect(page).toHaveURL(/#sys\/index$/);
    await expect(page.locator('#sys .ix .out')).toContainText(/nothing to decay|decay would change/i);
  });

  test('reorganize: lists suggestions; apply asks first', async ({ page }) => {
    await openSys(page, '#sys/reorganize');
    const rg = page.locator('#sys .rg');
    await expect(rg.locator('.sg').first()).toBeVisible({ timeout: 30_000 });
    const n = await rg.locator('.sg').count();
    await rg.locator('button.lk[data-a="apply"]').click();
    await expect(rg.locator('.confirm')).toContainText(`Apply ${n} suggestion`);
    await rg.locator('.confirm .no').click();
    await expect(rg.locator('.sg')).toHaveCount(n);
  });
});

test.describe('Sys import, tools and sync', () => {
  test('import: a markdown file lands in the chosen category', async ({ page }) => {
    await openSys(page, '#sys/import');
    const im = page.locator('#sys .im');
    await expect(im.locator('.go')).toBeDisabled();
    const name = `imported-${Date.now()}.md`;
    await im.locator('input[type="file"]').setInputFiles({
      name, mimeType: 'text/markdown',
      buffer: Buffer.from('# Imported note\n\nComets have two tails, one of dust and one of ions.\n'),
    });
    await expect(im.locator('.pf .t')).toHaveText(name);
    await im.locator('.cats button[data-c="opinions"]').click();
    await expect(im.locator('.hint')).toHaveText('1 file into opinions');
    await im.locator('.go').click();
    const row = im.locator('.ir.ok');
    await expect(row).toHaveCount(1, { timeout: 30_000 });
    const path = await row.getAttribute('data-path');
    expect(path).toMatch(/^opinions\//);
    await row.click();
    await expect(page.locator('#drawer .note-view .nt')).toHaveText('Imported note');
  });

  test('plugins and tools: tools run and show their result', async ({ page }) => {
    await openSys(page, '#sys/plugins');
    const pt = page.locator('#sys .pt');
    await expect(pt.locator('.pg').first()).toBeVisible();
    await pt.locator('.tn button[data-t="brain_search"]').click();
    await pt.locator('.tf input[name="query"]').fill('sourdough starter');
    await pt.locator('.tf .run').click();
    await expect(pt.locator('.tres')).toContainText('"chunk_id"');

    // A tool that changes the brain asks first when apply is ticked.
    await pt.locator('.tn button[data-t="brain_decay"]').click();
    await pt.locator('.tf input[name="apply"]').check();
    await pt.locator('.tf .run').click();
    await expect(pt.locator('.confirm')).toContainText('Run brain_decay for real?');
    await pt.locator('.confirm .no').click();
    await expect(pt.locator('.tres')).toHaveCount(0);

    // A plugin without its config reports why it cannot run.
    await pt.locator('.pg button[data-run]').first().click();
    await expect(pt.locator('.pg .res').first()).not.toBeEmpty();
  });

  test('sync: connections and an unconfigured backend', async ({ page }) => {
    await openSys(page, '#sys/sync');
    const sy = page.locator('#sys .sy');
    await expect(sy.locator('.svc .kv').filter({ hasText: 'Embeddings' }).locator('.v')).toHaveText('lsi');
    await expect(sy.locator('.svc .kv').filter({ hasText: 'LLM Provider' }).locator('.v')).toHaveText('not configured');
    await expect(sy.locator('.ss')).toContainText('no sync backend configured');
  });
});
