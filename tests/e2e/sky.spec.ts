import { test, expect } from '@playwright/test';
import { fullTurnWithoutLabelOverlap } from './labels-turn';

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

test.describe('Sky constellations', () => {
  test('labels the three largest constellations and orbits one numeral per cluster', async ({ page, request }) => {
    const vectors = await (await request.get('/api/vectors')).json();
    expect(vectors.clusters.length).toBeGreaterThanOrEqual(5);
    // A still sphere: while it turns, collision hiding may drop a label for a moment.
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/');
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.constellations?.length > 0);
    await expect(page.locator('#identity .n')).toContainText(`${vectors.clusters.length} constellations`);
    await expect(page.locator('#labels .rn')).toHaveCount(vectors.clusters.length);
    await page.waitForTimeout(800); // labels fade in
    // Only the three largest are labelled; one of them may give way to a
    // collision (earlier suites write to the brain and move the clusters),
    // but then it really does touch a shown label or an overlay.
    const { shown, unexplained } = await page.evaluate(() => {
      const els = [...document.querySelectorAll<HTMLElement>('#labels .cl')];
      const rect = (e: Element) => e.getBoundingClientRect();
      const gap = 8;
      const touch = (a: DOMRect, b: DOMRect) => a.left < b.right + gap && b.left < a.right + gap && a.top < b.bottom + gap && b.top < a.bottom + gap;
      const on = els.filter((e) => parseFloat(getComputedStyle(e).opacity) > 0.05);
      const blockers = [...on.map((e) => rect(e.firstElementChild!)),
        ...['#identity', '#inflow', '#query', '.rail'].map((s) => rect(document.querySelector(s)!))];
      const unexplained = els
        .filter((e) => Number(e.dataset.cluster) < 3 && !on.includes(e))
        .filter((e) => {
          const r = rect(e.firstElementChild!);
          return r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight && !blockers.some((b) => touch(r, b));
        })
        .map((e) => Number(e.dataset.cluster));
      return { shown: on.map((e) => Number(e.dataset.cluster)).sort(), unexplained };
    });
    expect(shown.every((c) => c < 3)).toBe(true);
    expect(shown).toContain(0);
    expect(unexplained).toEqual([]);
    const first = page.locator('#labels .cl[data-cluster="0"]');
    await expect(first.locator('.sc')).toHaveText(`I · ${vectors.clusters[0].size} memories`);
  });

  test('labels never overlap over a full turn', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/');
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.constellations?.length > 0);
    await fullTurnWithoutLabelOverlap(page);
  });

  test('labels hide outside the Sky', async ({ page }) => {
    await page.goto('/#read');
    await page.waitForFunction(() => (window as any).fluctlight?.scene?.constellations?.length > 0);
    await page.waitForTimeout(800);
    const visible = await page.$$eval('#labels .cl', (els) => els.filter((e) => parseFloat(getComputedStyle(e).opacity) > 0.05).length);
    expect(visible).toBe(0);
  });

  test('constellation names are title-cased and joined with an ampersand', async ({ page }) => {
    await page.goto('/');
    const names = await page.evaluate(async () => {
      const { constellationName } = await import('app/labels.js');
      return [constellationName('go · testing'), constellationName('tasks'), constellationName('go (2)')];
    });
    expect(names).toEqual(['Go & Testing', 'Tasks', 'Go (2)']);
  });
});
