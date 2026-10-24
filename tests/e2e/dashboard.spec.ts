import { test, expect } from '@playwright/test';

test.describe('Dashboard', () => {

  test('loads and shows sidebar navigation', async ({ page }) => {
    await page.goto('/classic');
    // Wait for Vue to mount (v-cloak removed)
    await expect(page.locator('#app')).toBeVisible();

    // Sidebar should be visible with nav items
    const nav = page.locator('nav[aria-label="Main navigation"]');
    await expect(nav).toBeVisible();

    // Check all nav labels are present
    // Chat only appears when an LLM is connected; the demo brain has none.
    const navLabels = ['Overview', 'Files', 'Tasks', 'Search', 'Import', 'Tools', 'Reorganize'];
    for (const label of navLabels) {
      await expect(nav.getByText(label)).toBeVisible();
    }
  });

  test('shows agent name and version in sidebar', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // Agent name or fallback 'Dashboard' should appear
    const sidebar = page.locator('nav[aria-label="Main navigation"]');
    await expect(sidebar).toBeVisible();

    // Version info at bottom of sidebar
    const versionInfo = sidebar.locator('[aria-label="Version info"]');
    await expect(versionInfo).toBeVisible();
  });

  test('overview page loads by default', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // Overview heading should be visible
    await expect(page.locator('#overview-heading')).toBeVisible();
    await expect(page.locator('#overview-heading')).toHaveText('Overview');

    // Stats section should be present
    const stats = page.locator('[aria-label="Brain statistics"]');
    await expect(stats).toBeVisible();
  });

  test('navigates between pages via sidebar', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // Navigate to Files
    await page.getByRole('button', { name: 'Files' }).click();
    await expect(page.locator('#files-heading')).toBeVisible();
    await expect(page.locator('#files-heading')).toHaveText('Brain Files');


    // Navigate to Search
    await page.getByRole('button', { name: 'Search' }).click();
    await expect(page.locator('#search-heading')).toBeVisible();

    // Navigate to Import
    await page.getByRole('button', { name: 'Import' }).click();
    await expect(page.locator('#import-heading')).toBeVisible();

    // Navigate to Tools
    await page.getByRole('button', { name: 'Tools' }).click();
    await expect(page.locator('#tools-heading')).toBeVisible();

    // Navigate back to Overview
    await page.getByRole('button', { name: 'Overview' }).click();
    await expect(page.locator('#overview-heading')).toBeVisible();
  });

  test('active nav item has aria-current="page"', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // Overview should be active by default
    const nav = page.locator('nav[aria-label="Main navigation"]');
    const overviewBtn = nav.getByRole('button', { name: 'Overview' });
    await expect(overviewBtn).toHaveAttribute('aria-current', 'page');

    // Navigate to Files
    const filesBtn = nav.getByRole('button', { name: 'Files' });
    await filesBtn.click();
    await expect(filesBtn).toHaveAttribute('aria-current', 'page');

    // Overview no longer active
    await expect(overviewBtn).not.toHaveAttribute('aria-current', 'page');
  });

  test('overview shows stat cards', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // Check stat labels exist
    await expect(page.locator('#stat-files')).toBeVisible();
    await expect(page.locator('#stat-words')).toBeVisible();
    await expect(page.locator('#stat-chunks')).toBeVisible();
    await expect(page.locator('#stat-tags')).toBeVisible();
  });

  // Needs an LLM provider; the demo brain has none.
  test.skip('chat page has mode toggle and input', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    await page.getByRole('button', { name: 'Chat' }).click();
    await expect(page.locator('#chat-heading')).toBeVisible();

    // Chat mode toggle (radiogroup)
    const modeGroup = page.locator('[aria-label="Chat mode"]');
    await expect(modeGroup).toBeVisible();

    // Brain and Free mode buttons
    await expect(modeGroup.getByRole('radio', { name: 'Brain' })).toBeVisible();
    await expect(modeGroup.getByRole('radio', { name: 'Free' })).toBeVisible();

    // Chat input
    await expect(page.locator('#chat-input')).toBeVisible();
  });

  // Needs an LLM provider; the demo brain has none.
  test.skip('chat mode toggle switches between brain and free', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    await page.getByRole('button', { name: 'Chat' }).click();

    // Brain should be selected by default
    const brainBtn = page.getByRole('radio', { name: 'Brain' });
    const freeBtn = page.getByRole('radio', { name: 'Free' });
    await expect(brainBtn).toHaveAttribute('aria-checked', 'true');
    await expect(freeBtn).toHaveAttribute('aria-checked', 'false');

    // Switch to Free mode
    await freeBtn.click();
    await expect(freeBtn).toHaveAttribute('aria-checked', 'true');
    await expect(brainBtn).toHaveAttribute('aria-checked', 'false');

    // Placeholder text should change
    await expect(page.locator('#chat-input')).toHaveAttribute('placeholder', /Chat freely/);
  });

  test('search page has input and search functionality', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    await page.getByRole('button', { name: 'Search' }).click();
    await expect(page.locator('#search-heading')).toBeVisible();

    // Search input should be visible
    const searchInput = page.locator('#search-input');
    await expect(searchInput).toBeVisible();
    await expect(searchInput).toHaveAttribute('placeholder', /Search brain files/);
  });

  test('import page has file upload and options', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    await page.getByRole('button', { name: 'Import' }).click();
    await expect(page.locator('#import-heading')).toBeVisible();

    // Import form should have category, confidence, tags fields
    await expect(page.locator('#import-category')).toBeVisible();
    await expect(page.locator('#import-confidence')).toBeVisible();
    await expect(page.locator('#import-tags')).toBeVisible();
  });

  test('skip navigation link exists for accessibility', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // Skip link should exist
    const skipLink = page.locator('a[href="#main-content"]');
    await expect(skipLink).toBeAttached();
  });

  test('API endpoints respond', async ({ request }) => {
    // Overview API
    const overview = await request.get('/api/overview');
    expect(overview.ok()).toBeTruthy();
    const data = await overview.json();
    expect(data).toHaveProperty('total_files');

    // Info API
    const info = await request.get('/api/info');
    expect(info.ok()).toBeTruthy();

    // Tags API
    const tags = await request.get('/api/tags');
    expect(tags.ok()).toBeTruthy();

    // Files API
    const files = await request.get('/api/files');
    expect(files.ok()).toBeTruthy();
  });

  test('CSS utility classes are applied correctly', async ({ page }) => {
    await page.goto('/classic');
    await expect(page.locator('#app')).toBeVisible();

    // card-base elements should exist
    const cards = page.locator('.card-base');
    expect(await cards.count()).toBeGreaterThan(0);

    // heading-xl elements should exist
    const headings = page.locator('.heading-xl');
    expect(await headings.count()).toBeGreaterThan(0);

    // stat-label elements should exist
    const statLabels = page.locator('.stat-label');
    expect(await statLabels.count()).toBeGreaterThan(0);

  });
});
