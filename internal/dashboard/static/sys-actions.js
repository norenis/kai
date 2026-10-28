// Sys action tabs (spec 7.6): Tasks, Index and Reorganize. Long or
// destructive actions ask for an inline confirmation and show progress.

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fmtInt = (n) => Number(n || 0).toLocaleString('en-US');
const plural = (n, w) => `${fmtInt(n)} ${w}${n === 1 ? '' : 's'}`;
const post = (body) => ({ method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body ?? {}) });

const PRIORITIES = ['', 'high', 'medium', 'low'];
const PRIORITY_MARK = /\s*[🔴🟡🟢]\s*/gu;

// confirmInline swaps an action link for "question · confirm · cancel" and
// resolves with the choice.
function confirmInline(btn, question) {
  return new Promise((resolve) => {
    const box = document.createElement('span');
    box.className = 'confirm';
    box.innerHTML = `<span class="q2">${esc(question)}</span><button type="button" class="yes">confirm</button><button type="button" class="no">cancel</button>`;
    btn.hidden = true;
    btn.after(box);
    const done = (ok) => { box.remove(); btn.hidden = false; resolve(ok); };
    box.querySelector('.yes').addEventListener('click', () => done(true));
    box.querySelector('.no').addEventListener('click', () => done(false));
    box.querySelector('.no').focus();
  });
}

// running shows an elapsed-time progress line in el while fn runs; the
// page's action links are disabled meanwhile.
async function running(page, el, label, fn) {
  const t0 = performance.now();
  const tick = () => { el.innerHTML = `<div class="prog sc"><b></b>${esc(label)} · ${((performance.now() - t0) / 1000).toFixed(0)} s</div>`; };
  tick();
  const timer = setInterval(tick, 1000);
  page.classList.add('busy');
  try {
    return await fn();
  } finally {
    clearInterval(timer);
    page.classList.remove('busy');
  }
}

const errLine = (err) => `<div class="sc res-err"><span class="co">${esc(err.message || err)}</span></div>`;

// ---------- Tasks ----------

export function renderTasks(body, { api }) {
  let state = null, priority = 0;
  body.innerHTML = '<div class="tasks"><div class="empty sc">loading…</div></div>';
  const el = body.querySelector('.tasks');

  function render() {
    const items = state.items || [];
    const open = items.filter((t) => !t.done).length;
    const day = state.path ? state.path.replace(/^tasks\/|\.md$/g, '') : '';
    const head = state.today ? `today · ${plural(open, 'open task')} · ${fmtInt(items.length - open)} done`
      : day ? `from ${day} · read only · adding a task starts today's list` : 'no task list yet';
    el.innerHTML = `
      <div class="sc th">${esc(head)}</div>
      <div class="tl">${items.map((t, i) => `
        <button type="button" class="tk${t.done ? ' done' : ''}${t.priority ? ` p-${t.priority}` : ''}" data-i="${i}" ${state.today ? '' : 'disabled'} aria-pressed="${t.done}">
          <span class="bx" aria-hidden="true"></span><span class="tt">${esc(t.text.replace(PRIORITY_MARK, ' ').trim())}</span>${t.priority ? `<span class="sc pr">${t.priority}</span>` : ''}
        </button>`).join('')}</div>
      <form class="add filter">
        <span class="ic" aria-hidden="true"></span>
        <input type="text" autocomplete="off" placeholder="Add a task…" aria-label="New task">
        <button type="button" class="sc pri" aria-label="Priority">${PRIORITIES[priority] || 'no priority'}</button>
      </form>
      <div class="msg"></div>`;
  }

  async function load() {
    try {
      state = await api('api/tasks');
      render();
    } catch (err) {
      el.innerHTML = errLine(err);
    }
  }

  el.addEventListener('click', async (e) => {
    const pri = e.target.closest('.pri');
    if (pri) { priority = (priority + 1) % PRIORITIES.length; pri.textContent = PRIORITIES[priority] || 'no priority'; return; }
    const tk = e.target.closest('.tk');
    if (!tk || tk.disabled) return;
    const i = Number(tk.dataset.i);
    try {
      state = { ...state, ...(await api('api/tasks', post({ index: i, done: !state.items[i].done }))) };
      render();
    } catch (err) {
      el.querySelector('.msg').innerHTML = errLine(err);
    }
  });
  el.addEventListener('submit', async (e) => {
    e.preventDefault();
    const input = el.querySelector('.add input');
    const text = input.value.trim();
    if (!text) return;
    try {
      await api('api/tasks/add', post({ text, priority: PRIORITIES[priority] }));
      priority = 0;
      await load();
      el.querySelector('.add input').focus();
    } catch (err) {
      el.querySelector('.msg').innerHTML = errLine(err);
    }
  });
  load();
}

// ---------- Index ----------

function decayRows(results) {
  return results.map((r) => {
    const path = r.Path ?? r.path, from = r.OldConf ?? r.old_conf, to = r.NewConf ?? r.new_conf, days = r.DaysSince ?? r.days_since;
    return `<tr data-path="${esc(path)}"><td class="t">${esc(path)}</td><td>${esc(from)} → ${esc(to)}</td><td class="num">${plural(days, 'day')}</td></tr>`;
  }).join('');
}

function migrateRows(actions) {
  return actions.map((a) => `
    <tr data-path="${esc(a.source_paths?.[0])}"${a.low_confidence ? ' class="unusual"' : ''}>
      <td class="t">${esc((a.source_paths || []).join(', '))}</td><td>→ ${esc(a.target_path)}</td>
      <td class="num">${Math.round((a.confidence || 0) * 100)}%${a.low_confidence ? ' <span class="sc flag">check</span>' : ''}</td>
    </tr>`).join('');
}

export function renderIndex(body, { api, data, reloadVectors, onOpen }) {
  body.innerHTML = `
    <div class="ix">
      <div class="row3">
        <section class="col"><h3 class="sc">Index</h3><div class="stats"></div></section>
        <section class="col wide">
          <h3 class="sc">Actions</h3>
          <div class="ia">
            <div><button type="button" class="lk" data-a="reindex">Reindex</button><span class="sc">changed notes only, then constellations</span></div>
            <div><button type="button" class="lk" data-a="rebuild">Rebuild embeddings</button><span class="sc">re-embeds every memory; slow with a remote provider</span></div>
            <div><button type="button" class="lk" data-a="decay">Decay · dry run</button><span class="sc">lowers confidence of notes not touched for months</span></div>
            <div><button type="button" class="lk" data-a="migrate">Migrate · dry run</button><span class="sc">moves notes out of unknown categories</span></div>
          </div>
        </section>
      </div>
      <div class="out"></div>
    </div>`;
  const page = body.querySelector('.ix');
  const stats = body.querySelector('.stats');
  const out = body.querySelector('.out');

  async function renderStats() {
    const v = data().vectors;
    let o = {};
    try { o = await api('api/overview'); } catch (err) { console.error('overview', err); }
    const rows = [
      ['notes', fmtInt(o.total_files)],
      ['chunks', fmtInt(o.total_chunks)],
      ['memories embedded', fmtInt(v?.nodes?.length)],
      ['dims · provider', v?.dims ? `${v.dims} · ${v.provider || '?'}` : 'no vectors'],
      ['bm25', o.index_health || 'missing'],
    ];
    stats.innerHTML = rows.map(([k, val]) => `<div class="kv"><span class="sc">${esc(k)}</span><span class="v${k === 'bm25' && o.index_health !== 'ok' ? ' co' : ''}">${esc(val)}</span></div>`).join('');
  }

  async function dryRun(kind) {
    const isDecay = kind === 'decay';
    const res = await running(page, out, isDecay ? 'checking stale notes' : 'checking categories', () => api(isDecay ? 'api/decay' : 'api/migrate'));
    const list = isDecay ? res.results || [] : res.actions || [];
    if (!list.length) {
      out.innerHTML = `<div class="sc done-line">${isDecay ? 'no stale notes · nothing to decay' : 'every note is in a valid category'}</div>`;
      return;
    }
    out.innerHTML = `
      <h3 class="sc">${isDecay ? `Decay would change ${plural(list.length, 'note')}` : `Migration would move ${plural(list.length, 'note')}`}</h3>
      <table class="act"><tbody>${isDecay ? decayRows(list) : migrateRows(list)}</tbody></table>
      <div class="ia"><div><button type="button" class="lk warn" data-apply="${kind}">${isDecay ? 'Apply decay' : 'Apply migration'}</button></div></div>`;
  }

  async function apply(kind, btn) {
    const isDecay = kind === 'decay';
    const n = out.querySelectorAll('tbody tr').length;
    if (!(await confirmInline(btn, isDecay ? `Lower the confidence of ${plural(n, 'note')}?` : `Move ${plural(n, 'note')} to new categories?`))) return;
    const res = await running(page, out, isDecay ? 'applying decay' : 'migrating', () => api(isDecay ? 'api/decay' : 'api/migrate', post({ apply: true })));
    const count = res.count ?? 0;
    out.innerHTML = `<div class="sc done-line">${isDecay ? `decayed ${plural(count, 'note')}` : `moved ${plural(count, 'note')}`}</div>`;
    if (!isDecay) await reloadVectors();
  }

  page.addEventListener('click', async (e) => {
    const row = e.target.closest('tr[data-path]');
    if (row && row.dataset.path) { onOpen(row.dataset.path); return; }
    const btn = e.target.closest('button.lk');
    if (!btn || page.classList.contains('busy')) return;
    try {
      if (btn.dataset.apply) { await apply(btn.dataset.apply, btn); return; }
      const a = btn.dataset.a;
      if (a === 'reindex') {
        await running(page, out, 'reindexing', () => api('api/reindex', post()));
        await reloadVectors();
        out.innerHTML = '<div class="sc done-line">index up to date · constellations recomputed</div>';
      } else if (a === 'rebuild') {
        const n = data().vectors?.nodes?.length || 0;
        if (!(await confirmInline(btn, `Re-embed all ${fmtInt(n)} ${n === 1 ? 'memory' : 'memories'}?`))) return;
        await running(page, out, 'rebuilding embeddings', () => api('api/vectors/build', post()));
        await reloadVectors();
        out.innerHTML = '<div class="sc done-line">embeddings rebuilt · constellations recomputed</div>';
      } else if (a === 'decay' || a === 'migrate') {
        await dryRun(a);
      }
      renderStats();
    } catch (err) {
      out.innerHTML = errLine(err);
    }
  });
  renderStats();
}

// ---------- Reorganize ----------

export function renderReorganize(body, { api, onOpen, reloadVectors }) {
  body.innerHTML = '<div class="rg"><div class="out"></div></div>';
  const page = body.querySelector('.rg');
  const out = body.querySelector('.out');
  let actions = [];

  async function load() {
    try {
      const res = await running(page, out, 'analyzing the brain', () => api('api/reorg'));
      actions = res.actions || [];
      render();
    } catch (err) {
      out.innerHTML = errLine(err);
    }
  }

  function render() {
    if (!actions.length) { out.innerHTML = '<div class="sc done-line">the brain looks tidy · nothing to reorganize</div>'; return; }
    out.innerHTML = `
      <h3 class="sc">${plural(actions.length, 'suggestion')}</h3>
      <div class="sug">${actions.map((a) => `
        <div class="sg">
          <div class="sc"><i>${esc(a.type)}</i> · ${esc(a.reason)}</div>
          <div class="paths">${(a.source_paths || []).map((p) => `<a href="#read/${encodeURIComponent(p)}" data-path="${esc(p)}">${esc(p)}</a>`).join('')}</div>
          ${a.target_path ? `<div class="to sc">into <a href="#read/${encodeURIComponent(a.target_path)}" data-path="${esc(a.target_path)}">${esc(a.target_path)}</a></div>` : ''}
        </div>`).join('')}</div>
      <div class="ia"><div><button type="button" class="lk warn" data-a="apply">Apply all ${plural(actions.length, 'suggestion')}</button><span class="sc">merges and moves notes; review each one first</span></div></div>`;
  }

  page.addEventListener('click', async (e) => {
    const link = e.target.closest('a[data-path]');
    if (link) { e.preventDefault(); onOpen(link.dataset.path); return; }
    const btn = e.target.closest('button.lk');
    if (!btn || page.classList.contains('busy')) return;
    if (!(await confirmInline(btn, `Apply ${plural(actions.length, 'suggestion')}? Notes will be merged or moved.`))) return;
    try {
      const res = await running(page, out, 'reorganizing', () => api('api/tools/execute', post({ tool: 'brain_reorganize', params: { apply: 'true' } })));
      if (res.error) throw new Error(res.error);
      out.innerHTML = `<div class="sc done-line">applied ${plural(actions.length, 'suggestion')}</div>`;
      await reloadVectors();
    } catch (err) {
      out.innerHTML = errLine(err);
    }
  });
  load();
}
