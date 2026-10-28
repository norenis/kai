import { test, expect, Page } from '@playwright/test';

const NOTE = 'knowledge/table-driven-tests-in-go.md';
const fl = (page: Page, expr: string) => page.evaluate(expr);

async function ready(page: Page, hash = '') {
  await page.goto(`/${hash}`);
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
}

const noteView = (page: Page) => page.locator('#drawer .note-view');

test.describe('Read note', () => {
  test('opening a note from search shows it with its neighbours lit in the sky', async ({ page }) => {
    await ready(page);
    await page.keyboard.press('Control+k');
    await page.keyboard.type('table driven tests');
    const row = page.locator(`#sheet .r[data-path="${NOTE}"]`).first();
    await expect(row).toBeAttached();
    await row.click();
    await expect(page).toHaveURL(/#read\/.+/);

    const view = noteView(page);
    await expect(view.locator('.nt')).toHaveText('Table driven tests in Go');
    await expect(view.locator('.crumb')).toContainText('knowledge');
    await expect(view.locator('.meta')).toContainText(/words/i);
    // The heading is the title, not repeated in the body.
    await expect(view.locator('.md h1')).toHaveCount(0);
    await expect(view.locator('.md pre .copy').first()).toBeAttached();

    const near = view.locator('.near .ri2');
    await expect(near.first()).toBeVisible();
    expect(await near.count()).toBeLessThanOrEqual(5);
    expect(await fl(page, 'fluctlight.scene.view')).toBe('note');
    await expect.poll(() => fl(page, 'fluctlight.scene.highlighted')).toBeGreaterThan(0);
    await expect.poll(() => fl(page, 'fluctlight.scene.secondaryHighlighted')).toBeGreaterThan(0);

    // A neighbour opens in the same view; esc goes back to the list.
    const next = await near.first().getAttribute('data-path');
    await near.first().click();
    await expect(page).toHaveURL(new RegExp(`#read/${encodeURIComponent(next!)}$`));
    await expect(view.locator('.crumb')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(/#read$/);
    await expect(page.locator('#drawer .li').first()).toBeVisible();
  });

  test('a note link survives a reload', async ({ page }) => {
    await ready(page, `#read/${encodeURIComponent(NOTE)}`);
    await expect(noteView(page).locator('.nt')).toHaveText('Table driven tests in Go');
    await page.reload();
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
    await expect(noteView(page).locator('.nt')).toHaveText('Table driven tests in Go');
    await expect.poll(() => fl(page, 'fluctlight.scene.highlighted')).toBeGreaterThan(0);
  });

  test('note markdown cannot run script', async ({ page }) => {
    await ready(page);
    const path = `knowledge/unsafe-${Date.now()}.md`;
    const content = '# Unsafe note\n\n<img src=x onerror="window.pwned=1">\n\n<script>window.pwned=2</script>\n\n[click](javascript:window.pwned=3)\n';
    await page.request.post('/api/tools/execute', { data: { tool: 'brain_write', params: { path, content, tags: 'demo' } } });
    await page.goto(`/#read/${encodeURIComponent(path)}`);
    const view = noteView(page);
    await expect(view.locator('.nt')).toHaveText('Unsafe note');
    await expect(view.locator('.md')).toContainText('onerror');
    await expect(view.locator('.md img, .md script, .md a[href^="javascript"]')).toHaveCount(0);
    await view.locator('.md').getByText('click').click();
    expect(await fl(page, 'window.pwned')).toBeUndefined();
  });

  test('a vault note asks for the PIN', async ({ page }) => {
    await ready(page, '#read/' + encodeURIComponent('vault/demo-locker-codes.md'));
    const view = noteView(page);
    const input = view.locator('input[type="password"]');
    await expect(input).toBeVisible();
    await expect(view.locator('.md')).toHaveCount(0);

    await input.fill('1111');
    await input.press('Enter');
    await expect(view.locator('.meta')).toContainText('wrong pin');

    await view.locator('input[type="password"]').fill('2468');
    await view.locator('input[type="password"]').press('Enter');
    await expect(view.locator('.md')).toContainText('Gym locker');
  });
});
