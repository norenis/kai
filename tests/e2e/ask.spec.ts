import { test, expect } from '@playwright/test';
import { spawn, execFileSync, ChildProcess } from 'node:child_process';
import { createServer, Server } from 'node:http';
import { cpSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { AddressInfo } from 'node:net';

// Ask needs an LLM, which only the MCP server wires in. This suite runs
// `kai mcp` over HTTP against a copy of the demo brain, with the OpenAI
// provider pointed at a local fake that streams a cited answer.
const PORT = 9261;
let llm: Server, kai: ChildProcess, dir: string;
const prompts: { system: string; messages: { role: string; content: string }[] }[] = [];

test.beforeAll(async () => {
  llm = createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      const r = JSON.parse(body);
      const system = r.messages.find((m: any) => m.role === 'system')?.content || '';
      prompts.push({ system, messages: r.messages.filter((m: any) => m.role !== 'system') });
      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      const parts = ['Feed the starter ', 'twice a day [i] ', 'and keep it **warm** [ii].', ' <img src=x onerror="window.pwned=1">'];
      let i = 0;
      const tick = setInterval(() => {
        if (i < parts.length) {
          res.write(`data: ${JSON.stringify({ choices: [{ delta: { content: parts[i++] } }] })}\n\n`);
        } else {
          res.write('data: [DONE]\n\n');
          res.end();
          clearInterval(tick);
        }
      }, 120);
    });
  });
  await new Promise<void>((r) => llm.listen(0, '127.0.0.1', r));
  const llmPort = (llm.address() as AddressInfo).port;

  dir = mkdtempSync(join(tmpdir(), 'kai-ask-'));
  cpSync('tests/fixtures/demo-brain', join(dir, 'brain'), { recursive: true });
  writeFileSync(join(dir, 'config.yaml'), `agent:
  name: Demo
provider: openai
providers:
  openai:
    auth: api_key
    api_key: fake
    model: fake-model
    base_url: http://127.0.0.1:${llmPort}
brain:
  path: ${dir}/brain
  embeddings:
    provider: lsi
sessions_path: ${dir}/sessions
`);
  execFileSync('./kai', ['--config', join(dir, 'config.yaml'), 'reindex']);
  kai = spawn('./kai', ['--config', join(dir, 'config.yaml'), 'mcp', '--transport', 'http', '--addr', `127.0.0.1:${PORT}`], {
    env: { ...process.env, KAI_MCP_TOKEN: 'test-token' },
    stdio: 'ignore',
  });
  for (let i = 0; i < 50; i++) {
    try {
      if ((await fetch(`http://127.0.0.1:${PORT}/api/info`)).ok) break;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 200));
  }
});

test.afterAll(() => {
  kai?.kill();
  llm?.close();
  rmSync(dir, { recursive: true, force: true });
});

test.use({ baseURL: `http://127.0.0.1:${PORT}` });

test('⌘↵ streams a cited answer with sources, trace and follow-ups', async ({ page }) => {
  test.setTimeout(60_000);
  await page.goto('/');
  await page.waitForFunction(() => (window as any).fluctlight?.scene?.count > 0);
  await page.keyboard.press('/');
  const sheet = page.locator('#sheet');
  await expect(sheet.locator('.ask')).not.toHaveClass(/dis/);
  await sheet.locator('input').fill('how do I feed a sourdough starter');
  await page.keyboard.press('Control+Enter');

  await expect(sheet).toHaveAttribute('data-mode', 'ask');
  const ans = sheet.locator('.turn.last .ans');
  await expect(ans).toContainText('Feed the starter twice a day');
  await expect(sheet.locator('.turn.last .tq')).toHaveText('how do I feed a sourdough starter');
  await expect(sheet.locator('input')).toHaveAttribute('placeholder', 'Follow up…');
  await expect(ans.locator('sup')).toHaveText(['i', 'ii'], { timeout: 15_000 });
  await expect(ans.locator('strong')).toHaveText('warm');
  await expect(sheet.locator('.turn.last .src span').first()).toContainText(/^i/);
  await expect(sheet.locator('.tr')).toHaveText([/embed/i, /recall/i, /synthesize/i]);
  await expect(sheet.locator('.tr.on')).toHaveCount(3);
  await expect(sheet.locator('.meta')).toContainText(/\d+ sources/);
  // Raw HTML from the model is shown as text, never executed.
  expect(await page.evaluate(() => (window as any).pwned)).toBeUndefined();
  expect(await ans.locator('img').count()).toBe(0);
  expect(await page.evaluate(() => (window as any).fluctlight.scene.view)).toBe('ask');

  if (process.env.KAI_SHOTS) await page.screenshot({ path: join(process.env.KAI_SHOTS, 'ask.png') });

  // The system prompt numbers the sources the answer cites.
  expect(prompts[0].system).toContain('### [i] ');

  // A follow-up stays in the sheet and sends the earlier turn along.
  await sheet.locator('input').fill('and in winter?');
  await page.keyboard.press('Control+Enter');
  await expect(sheet.locator('.turn')).toHaveCount(2);
  await expect(sheet.locator('.turn.last .ans sup').first()).toBeVisible({ timeout: 15_000 });
  expect(prompts[1].messages.map((m) => m.role)).toEqual(['user', 'assistant', 'user']);
  expect(prompts[1].messages[0].content).toBe('how do I feed a sourdough starter');

  // esc collapses; reopening shows the kept conversation.
  await page.keyboard.press('Escape');
  await expect(sheet).toBeHidden();
  await page.keyboard.press('/');
  await expect(sheet.locator('.turn')).toHaveCount(2);

  // A source opens its note.
  const path = await sheet.locator('.turn.last .src span').first().getAttribute('data-path');
  await sheet.locator('.turn.last .src span').first().click();
  await expect(page).toHaveURL(/#read\/.+/);
  expect(await page.evaluate(() => (window as any).fluctlight.opened)).toBe(path);
});
