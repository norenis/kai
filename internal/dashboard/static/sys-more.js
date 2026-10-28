// Sys tabs Import, Plugins & tools and Sync (spec 7.6), ported from the
// classic dashboard onto the same endpoints.

import { esc, fmtInt, plural, post, confirmInline, running, errLine } from 'app/sys-actions.js';

const CATEGORIES = ['knowledge', 'identity', 'opinions', 'decisions', 'style'];
const CONFIDENCE = ['medium', 'high', 'low'];
const ACCEPT = '.md,.csv,.xlsx';

// ---------- Import ----------

export function renderImport(body, { api, onOpen, reloadVectors }) {
  let files = [], category = 'knowledge', confidence = 0, autoTag = true, autoChunk = true;
  body.innerHTML = `
    <div class="im">
      <label class="drop">
        <input type="file" multiple accept="${ACCEPT}" hidden>
        <span class="t">Drop notes here, or choose files</span>
        <span class="sc">markdown, csv or xlsx</span>
      </label>
      <div class="picked"></div>
      <div class="opts">
        <div class="kv"><span class="sc">category</span><span class="cats"></span></div>
        <div class="kv"><span class="sc">tags</span><input class="tags" type="text" autocomplete="off" placeholder="comma separated" aria-label="Tags"></div>
        <div class="kv"><span class="sc">confidence</span><button type="button" class="tog conf"></button></div>
        <div class="kv"><span class="sc">auto tag</span><button type="button" class="tog at" aria-pressed="true"></button></div>
        <div class="kv"><span class="sc">auto chunk</span><button type="button" class="tog ac" aria-pressed="true"></button></div>
      </div>
      <div class="ia"><div><button type="button" class="lk go">Import</button><span class="sc hint"></span></div></div>
      <div class="out"></div>
    </div>`;
  const page = body.querySelector('.im');
  const input = page.querySelector('input[type="file"]');
  const out = page.querySelector('.out');

  function render() {
    page.querySelector('.picked').innerHTML = files.map((f, i) => `
      <div class="pf"><span class="t">${esc(f.name)}</span><span class="sc">${fmtInt(Math.ceil(f.size / 1024))} kb</span><button type="button" class="sc rm" data-i="${i}" aria-label="Remove ${esc(f.name)}">remove</button></div>`).join('');
    page.querySelector('.cats').innerHTML = CATEGORIES.map((c) => `<button type="button" class="${c === category ? 'on' : ''}" data-c="${c}">${c}</button>`).join('');
    page.querySelector('.conf').textContent = CONFIDENCE[confidence];
    page.querySelector('.at').textContent = autoTag ? 'on' : 'off';
    page.querySelector('.at').setAttribute('aria-pressed', String(autoTag));
    page.querySelector('.ac').textContent = autoChunk ? 'on' : 'off';
    page.querySelector('.ac').setAttribute('aria-pressed', String(autoChunk));
    page.querySelector('.go').disabled = !files.length;
    page.querySelector('.hint').textContent = files.length ? `${plural(files.length, 'file')} into ${category}` : 'choose files first';
  }

  const add = (list) => {
    const names = new Set(files.map((f) => f.name));
    files = [...files, ...[...list].filter((f) => /\.(md|csv|xlsx)$/i.test(f.name) && !names.has(f.name))];
    render();
  };
  input.addEventListener('change', () => { add(input.files); input.value = ''; });
  const drop = page.querySelector('.drop');
  drop.addEventListener('dragover', (e) => { e.preventDefault(); drop.classList.add('over'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('over'));
  drop.addEventListener('drop', (e) => { e.preventDefault(); drop.classList.remove('over'); add(e.dataTransfer.files); });

  page.addEventListener('click', async (e) => {
    const t = e.target;
    if (t.closest('.rm')) { files.splice(Number(t.closest('.rm').dataset.i), 1); render(); return; }
    if (t.closest('.cats button')) { category = t.closest('.cats button').dataset.c; render(); return; }
    if (t.closest('.conf')) { confidence = (confidence + 1) % CONFIDENCE.length; render(); return; }
    if (t.closest('.at')) { autoTag = !autoTag; render(); return; }
    if (t.closest('.ac')) { autoChunk = !autoChunk; render(); return; }
    const res = t.closest('.out [data-path]');
    if (res) { onOpen(res.dataset.path); return; }
    if (!t.closest('.go') || !files.length || page.classList.contains('busy')) return;

    const form = new FormData();
    for (const f of files) form.append('files', f);
    form.append('category', category);
    form.append('tags', page.querySelector('.tags').value);
    form.append('confidence', CONFIDENCE[confidence]);
    form.append('autoTag', String(autoTag));
    form.append('autoChunk', String(autoChunk));
    try {
      const data = await running(page, out, `importing ${plural(files.length, 'file')}`, () => api('api/import', { method: 'POST', body: form }));
      if (data.error) throw new Error(data.error);
      const results = data.results || [];
      const ok = results.filter((r) => !r.error);
      out.innerHTML = `
        <h3 class="sc">imported ${fmtInt(ok.length)} of ${plural(results.length, 'file')}</h3>
        ${results.map((r) => r.error
          ? `<div class="ir"><span class="t">${esc(r.path)}</span><span class="sc co">${esc(r.error)}</span></div>`
          : `<div class="ir ok" data-path="${esc(r.path)}" role="link" tabindex="0"><span class="t">${esc(r.path)}</span><span class="sc">open</span></div>`).join('')}`;
      if (ok.length) { files = []; render(); await reloadVectors(); }
    } catch (err) {
      out.innerHTML = errLine(err);
    }
  });
  render();
}

// ---------- Plugins & tools ----------

export function renderPlugins(body, { api, reloadVectors }) {
  let tools = [], tool = null;
  body.innerHTML = `
    <div class="pt">
      <div class="row3">
        <section class="col"><h3 class="sc">Plugins</h3><div class="pl"><div class="empty sc">loading…</div></div></section>
        <section class="col wide"><h3 class="sc">Tools</h3><div class="tn"></div><form class="tf"></form><div class="out"></div></section>
      </div>
    </div>`;
  const page = body.querySelector('.pt');
  const pl = page.querySelector('.pl');
  const tn = page.querySelector('.tn');
  const tf = page.querySelector('.tf');
  const out = page.querySelector('.out');

  function renderTool() {
    tn.innerHTML = tools.map((t) => `<button type="button" class="${t === tool ? 'on' : ''}" data-t="${esc(t.name)}">${esc(t.name)}</button>`).join('');
    if (!tool) { tf.innerHTML = ''; return; }
    tf.innerHTML = `
      <div class="sc desc">${esc(tool.description)}</div>
      ${(tool.params || []).map((p) => `
        <label class="kv">
          <span class="sc">${esc(p.name)}${p.required ? ' <i>*</i>' : ''}</span>
          ${p.type === 'boolean'
            ? `<input type="checkbox" name="${esc(p.name)}">`
            : p.name === 'content'
              ? `<textarea name="${esc(p.name)}" rows="5" placeholder="${esc(p.description)}"></textarea>`
              : `<input type="${p.name === 'pin' ? 'password' : 'text'}" name="${esc(p.name)}" autocomplete="off" placeholder="${esc(p.description)}">`}
        </label>`).join('')}
      <div class="ia"><div><button type="submit" class="lk run">Run ${esc(tool.name)}</button></div></div>`;
  }

  async function load() {
    try {
      const [p, t] = await Promise.all([api('api/plugins'), api('api/tools')]);
      const plugins = p.plugins || [];
      pl.innerHTML = plugins.map((x) => `
        <div class="pg">
          <div><span class="t">${esc(x.name)}</span> <span class="sc ${x.enabled ? 'en' : ''}">${x.enabled ? 'enabled' : 'not enabled'}</span></div>
          <div class="d">${esc(x.description)}</div>
          <div class="ia"><div><button type="button" class="lk" data-run="${esc(x.name)}">Run</button><span class="sc res"></span></div></div>
        </div>`).join('') || '<div class="empty sc">no plugins available</div>';
      tools = t.tools || [];
      tool = tools[0] || null;
      renderTool();
    } catch (err) {
      pl.innerHTML = errLine(err);
    }
  }

  page.addEventListener('click', async (e) => {
    const pick = e.target.closest('.tn button');
    if (pick) { tool = tools.find((t) => t.name === pick.dataset.t); out.innerHTML = ''; renderTool(); return; }
    const run = e.target.closest('button[data-run]');
    if (!run || page.classList.contains('busy')) return;
    const res = run.parentElement.querySelector('.res');
    try {
      const data = await running(page, res, `running ${run.dataset.run}`, () => api('api/plugins/run', post({ name: run.dataset.run })));
      if (data.error) throw new Error(data.error);
      res.innerHTML = `<span class="done-line">${data.imported ? `imported ${plural(data.imported, 'note')}` : esc(data.message || 'nothing fetched')}</span>`;
      if (data.imported) await reloadVectors();
    } catch (err) {
      res.innerHTML = `<span class="co">${esc(err.message || err)}</span>`;
    }
  });

  tf.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (!tool || page.classList.contains('busy')) return;
    const params = {};
    for (const p of tool.params || []) {
      const el = tf.elements[p.name];
      if (p.type === 'boolean') { if (el.checked) params[p.name] = 'true'; } else if (el.value) params[p.name] = el.value;
    }
    const missing = (tool.params || []).filter((p) => p.required && !params[p.name]).map((p) => p.name);
    if (missing.length) { out.innerHTML = errLine(`missing ${missing.join(', ')}`); return; }
    // Anything that applies changes (reorganize, migrate, decay) asks first.
    if (params.apply === 'true' && !(await confirmInline(tf.querySelector('.run'), `Run ${tool.name} for real?`))) return;
    try {
      const data = await running(page, out, `running ${tool.name}`, () => api('api/tools/execute', post({ tool: tool.name, params })));
      if (data.error) throw new Error(data.error);
      out.innerHTML = `<pre class="tres">${esc(data.result)}</pre>`;
    } catch (err) {
      out.innerHTML = errLine(err);
    }
  });
  load();
}

// ---------- Sync ----------

const SERVICE_STATE = { connected: 'ok', disconnected: 'co', not_configured: '' };
const STATE_TEXT = { disconnected: 'disconnected', not_configured: 'not configured' };

// serviceText is "lsi", "openai · disconnected" or "not configured" (the
// server sends a dash as the value of an unconfigured service).
function serviceText(s) {
  const value = /^[-\u2014\s]*$/.test(s.value || '') ? '' : s.value;
  return [value, STATE_TEXT[s.status]].filter(Boolean).join(' · ');
}

export function renderSync(body, { api, reloadVectors }) {
  body.innerHTML = `
    <div class="sy">
      <div class="row3">
        <section class="col"><h3 class="sc">Connections</h3><div class="svc"><div class="empty sc">loading…</div></div></section>
        <section class="col wide"><h3 class="sc">Brain sync</h3><div class="ss"><div class="empty sc">loading…</div></div><div class="out"></div></section>
      </div>
    </div>`;
  const page = body.querySelector('.sy');
  const ss = page.querySelector('.ss');
  const out = page.querySelector('.out');

  async function loadServices() {
    try {
      const { services = [] } = await api('api/status');
      page.querySelector('.svc').innerHTML = services.map((s) => `
        <div class="kv" title="${esc(s.tooltip)}"><span class="sc">${esc(s.name)}</span><span class="v ${SERVICE_STATE[s.status] ?? ''}">${esc(serviceText(s))}</span></div>`).join('');
    } catch (err) {
      page.querySelector('.svc').innerHTML = errLine(err);
    }
  }

  async function loadStatus() {
    try {
      const d = await api('api/sync/status');
      if (d.error) throw new Error(d.error);
      if (!d.configured) {
        ss.innerHTML = '<div class="sc">no sync backend configured · set <i>sync</i> in config.yaml</div>';
        return;
      }
      const s = d.cloud || d.git || {};
      const n = (a, b) => (s[a] || s[b] || []).length;
      const rows = [
        ['backend', d.cloud ? 'cloud' : d.git ? 'git' : 'configured'],
        ['in sync', fmtInt(n('in_sync', 'InSync'))],
        ['local only', fmtInt(n('local_only', 'LocalOnly'))],
        ['remote only', fmtInt(n('remote_only', 'RemoteOnly'))],
        ['modified', fmtInt(n('modified', 'Modified'))],
      ];
      ss.innerHTML = `${rows.map(([k, v]) => `<div class="kv"><span class="sc">${k}</span><span class="v">${esc(v)}</span></div>`).join('')}
        <div class="ia"><div><button type="button" class="lk" data-a="push">Push</button><button type="button" class="lk" data-a="pull">Pull</button></div></div>`;
    } catch (err) {
      ss.innerHTML = errLine(err);
    }
  }

  page.addEventListener('click', async (e) => {
    const btn = e.target.closest('button.lk[data-a]');
    if (!btn || page.classList.contains('busy')) return;
    const push = btn.dataset.a === 'push';
    if (!(await confirmInline(btn, push ? 'Push local notes to the remote?' : 'Pull remote notes into this brain?'))) return;
    try {
      const data = await running(page, out, push ? 'pushing' : 'pulling', () => api(`api/sync/${btn.dataset.a}`, post()));
      if (data.error) throw new Error(data.error);
      out.innerHTML = `<div class="sc done-line">${esc(data.status)}${data.warning ? ` · <span class="co">${esc(data.warning)}</span>` : ''}</div>`;
      await loadStatus();
      if (!push) await reloadVectors();
    } catch (err) {
      out.innerHTML = errLine(err);
    }
  });
  loadServices();
  loadStatus();
}
