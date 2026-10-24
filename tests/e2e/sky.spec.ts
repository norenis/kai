import { test, expect } from '@playwright/test';

test.describe('Sky identity', () => {
  test('shows the brain name, memory count and status line', async ({ page, request }) => {
    const vectors = await (await request.get('/api/vectors')).json();
    await page.goto('/');
    const id = page.locator('#identity');
    await expect(id.locator('.nm')).toHaveText('Demo');
    await expect(id.locator('.n b')).toHaveText(vectors.nodes.length.toLocaleString('en-US'));
    await expect(id.locator('.s')).toHaveText(/^(Resting · learned \d+ days ago|Learning · .+)$/);
    await expect(page.locator('#query .ph')).toHaveText('Ask Demo what it remembers…');
  });

  test('status line switches from learning to resting after 48 hours', async ({ page }) => {
    await page.goto('/');
    const lines = await page.evaluate(async () => {
      const { statusLine } = await import('app/app.js');
      const now = Date.parse('2026-10-24T12:00:00Z');
      return [
        statusLine('2026-10-24T11:59:30Z', now),
        statusLine('2026-10-24T09:00:00Z', now),
        statusLine('2026-10-22T13:00:00Z', now),
        statusLine('2026-10-19T12:00:00Z', now),
        statusLine('', now),
      ];
    });
    expect(lines).toEqual([
      { resting: false, text: 'Learning · just now' },
      { resting: false, text: 'Learning · 3 hours ago' },
      { resting: false, text: 'Learning · 47 hours ago' },
      { resting: true, text: 'Resting · learned 5 days ago' },
      { resting: true, text: 'Resting · nothing learned yet' },
    ]);
  });

  test('fresh notes render champagne and brighter, old notes cyan and dimmer', async ({ page }) => {
    await page.goto('/');
    const pts = await page.evaluate(async () => {
      const { prepareVectors } = await import('app/scene.js');
      const now = Date.parse('2026-10-24T12:00:00Z');
      const node = (id: string, updated: string, x: number) => ({ id, doc: id, x, y: x / 2, z: -x, updated });
      const d = prepareVectors({ nodes: [node('a', '2026-10-24', 1), node('b', '2026-10-14', 2), node('c', '2026-06-01', 3), node('d', '', 4)], links: [] }, now);
      return { fresh: [...d.fresh], bright: [...d.bright].map((b) => +b.toFixed(2)) };
    });
    expect(pts.fresh).toEqual([1, 0, 0, 0]);
    expect(pts.bright).toEqual([0.98, 0.72, 0.42, 0.42]);
  });
});
