// The global ⌘K sheet (spec 7.2): type to search the brain, ↵ to read a
// note, ⌘↵ to ask (Task 10). The scene rises above the sheet, hits breathe
// and recall fibres stream from them to the sheet's top edge.

const DEBOUNCE_MS = 160;
const TRACE_STEP_MS = 140;

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const reEsc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// markTerms underlines the query's words (two letters or more) in a snippet.
export function markTerms(snippet, query) {
  const terms = [...new Set(String(query).toLowerCase().split(/[^\p{L}\p{N}]+/u).filter((t) => t.length >= 2))];
  const safe = esc(snippet);
  if (!terms.length) return safe;
  const re = new RegExp(`(${terms.map((t) => reEsc(esc(t))).join('|')})`, 'giu');
  return safe.replace(re, '<mark>$1</mark>');
}

const isTyping = (el) => el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));

// mountSearch builds the sheet inside el.
//   api(path, init), scene, labels, clusters() → array, llm() → bool,
//   onOpen(path), onToggle(open)
export function mountSearch(el, { api, scene, labels, clusters, llm, onOpen, onToggle }) {
  el.innerHTML = `
    <div class="in">
      <span class="ic" aria-hidden="true"></span>
      <input class="v" type="search" autocomplete="off" spellcheck="false" aria-label="Search memories" placeholder="Search memories…">
      <span class="sc meta" aria-live="polite"></span>
    </div>
    <div class="trace" aria-label="Recall trace"></div>
    <div class="res" role="listbox" aria-label="Results"></div>
    <div class="fb">
      <span class="sc"><kbd>↑↓</kbd>move</span><span class="sc"><kbd>↵</kbd>read</span><span class="sc"><kbd>esc</kbd>close</span>
      <span class="sc ask"><kbd>⌘↵</kbd><span class="ask-label"></span></span>
    </div>`;
  const input = el.querySelector('input');
  const meta = el.querySelector('.meta');
  const traceEl = el.querySelector('.trace');
  const list = el.querySelector('.res');
  const askEl = el.querySelector('.ask');

  let open = false, results = [], sel = 0, timer = 0, ctrl = null, seq = 0, query = '';
  let traceTimers = [];

  const numeral = (c) => clusters()?.[c]?.numeral || '·';

  function renderAsk() {
    const ok = llm();
    askEl.classList.toggle('dis', !ok);
    askEl.querySelector('.ask-label').innerHTML = ok ? '<i>ask</i>' : 'ask · no llm configured';
  }

  function renderTrace(trace) {
    traceTimers.forEach(clearTimeout);
    traceTimers = [];
    const stages = trace?.stages || [];
    traceEl.innerHTML = stages.map((s) => `<div class="tr"><b></b><span>${esc(s.name)}</span><em>${Math.round(s.ms)}ms</em></div>`).join('');
    [...traceEl.children].forEach((c, i) => traceTimers.push(setTimeout(() => c.classList.add('on'), 60 + i * TRACE_STEP_MS)));
  }

  function renderResults() {
    const top = results[0]?.score || 1;
    list.innerHTML = results.map((r, i) => `
      <div class="r${i === sel ? ' sel' : ''}" role="option" aria-selected="${i === sel}" data-i="${i}" data-path="${esc(r.doc_path)}">
        <span class="rn2">${esc(numeral(r.cluster))}</span>
        <div><div class="t">${esc(r.title)}</div><div class="sn">${markTerms(r.snippet, query)}</div></div>
        <div class="scv">${(r.score / top).toFixed(2)}<i style="width:${Math.max(4, (r.score / top) * 100).toFixed(0)}%"></i></div>
      </div>`).join('');
    const notes = new Set(results.map((r) => r.doc_path)).size;
    const nums = [...new Set(results.map((r) => r.cluster).filter((c) => c >= 0))].sort((a, b) => a - b).map(numeral);
    meta.innerHTML = query
      ? `${results.length} memories · ${notes} notes${nums.length ? ` · <i>${esc(nums.join(', '))}</i>` : ''}`
      : '';
    list.querySelector('.r.sel')?.scrollIntoView({ block: 'nearest' });
  }

  // The scene follows the results: hits breathe, fibres flow, only the
  // constellations holding hits keep their labels.
  function showInScene() {
    const ids = results.map((r) => `${r.doc_path}#${r.chunk_id}`);
    scene.highlight(ids, { color: '#ffffff' });
    labels.setHitClusters(results.map((r) => r.cluster).filter((c) => c >= 0));
    scene.recall(ids.length ? ids : null, sheetTopNdc);
  }

  // sheetTopNdc is the sheet's top edge in normalised device coordinates.
  function sheetTopNdc() {
    const host = document.getElementById('gl').getBoundingClientRect();
    const r = el.getBoundingClientRect();
    if (!r.width) return null;
    const nx = (x) => ((x - host.left) / host.width) * 2 - 1;
    return { x0: nx(r.left + r.width * 0.2), x1: nx(r.right - r.width * 0.2), y: 1 - ((r.top - host.top) / host.height) * 2 };
  }

  async function run(q) {
    const my = ++seq;
    ctrl?.abort();
    ctrl = new AbortController();
    if (!q.trim()) {
      results = []; query = '';
      renderResults(); renderTrace(null); showInScene();
      return;
    }
    try {
      const data = await api(`api/search?q=${encodeURIComponent(q)}`, { signal: ctrl.signal });
      if (my !== seq) return;
      query = q;
      results = data.results || [];
      sel = 0;
      renderResults();
      renderTrace(data.trace);
      showInScene();
    } catch (err) {
      if (err.name !== 'AbortError') console.error('search', err);
    }
  }

  function show() {
    if (open) { input.focus(); input.select(); return; }
    open = true;
    el.hidden = false;
    renderAsk();
    onToggle(true);
    input.focus();
    input.select();
    if (input.value.trim()) run(input.value);
  }

  function hide() {
    if (!open) return;
    open = false;
    el.hidden = true;
    ctrl?.abort();
    scene.highlight([]);
    scene.recall(null);
    labels.setHitClusters([]);
    onToggle(false);
  }

  function choose(i) {
    const r = results[i];
    if (!r) return;
    hide();
    onOpen(r.doc_path);
  }

  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(() => run(input.value), DEBOUNCE_MS);
  });
  input.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!results.length) return;
      sel = (sel + (e.key === 'ArrowDown' ? 1 : results.length - 1)) % results.length;
      renderResults();
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault(); // ask: Task 10
    } else if (e.key === 'Enter') {
      e.preventDefault();
      choose(sel);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      hide();
    }
  });
  list.addEventListener('mousemove', (e) => {
    const row = e.target.closest('.r');
    if (row && Number(row.dataset.i) !== sel) { sel = Number(row.dataset.i); renderResults(); }
  });
  list.addEventListener('click', (e) => {
    const row = e.target.closest('.r');
    if (row) choose(Number(row.dataset.i));
  });

  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      if (open && document.activeElement === input) hide(); else show();
    } else if (e.key === '/' && !open && !isTyping(e.target)) {
      e.preventDefault();
      show();
    } else if (e.key === 'Escape' && open) {
      hide();
    }
  });

  return { show, hide, get open() { return open; }, get results() { return results; } };
}
