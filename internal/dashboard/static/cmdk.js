// The global ⌘K sheet (spec 7.2, 7.3): type to search the brain, ↵ to read
// a note, ⌘↵ to ask. The scene rises above the sheet, hits breathe and
// recall fibres stream from them to the sheet's top edge. An ask streams the
// answer with numbered citations; follow-ups stay in the same conversation,
// which survives closing the sheet until the page reloads.

import { renderAnswer } from 'app/markdown.js';

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
//   onOpen(path), onToggle(open), onMode('search'|'ask')
export function mountSearch(el, { api, scene, labels, clusters, llm, onOpen, onToggle, onMode = () => {} }) {
  el.innerHTML = `
    <div class="in">
      <span class="ic" aria-hidden="true"></span>
      <input class="v" type="search" autocomplete="off" spellcheck="false" aria-label="Search memories" placeholder="Search memories…">
      <span class="sc meta" aria-live="polite"></span>
      <button type="button" class="sc x" aria-label="Close search">close</button>
    </div>
    <div class="trace" aria-label="Recall trace"></div>
    <div class="res" role="listbox" aria-label="Results"></div>
    <div class="conv" aria-live="polite" hidden></div>
    <div class="fb">
      <span class="sc for-search"><kbd>↑↓</kbd>move</span><span class="sc for-search"><kbd>↵</kbd>read</span><span class="sc for-search"><kbd>esc</kbd>close</span>
      <span class="sc for-ask">follow up…</span><span class="sc for-ask"><kbd>esc</kbd>collapse · history kept</span>
      <button type="button" class="sc fm" aria-pressed="false" title="Answer from memories, or chat freely without them">from memories</button>
      <span class="sc ask"><kbd>⌘↵</kbd><span class="ask-label"></span></span>
    </div>`;
  const input = el.querySelector('input');
  const meta = el.querySelector('.meta');
  const traceEl = el.querySelector('.trace');
  const list = el.querySelector('.res');
  const askEl = el.querySelector('.ask');
  const freeEl = el.querySelector('.fm');
  const conv = el.querySelector('.conv');

  // Ask conversation: [{q, answer, sources, trace, streaming, error}].
  let mode = 'search';
  const turns = [];
  let asking = false;
  let free = false; // free chat: the LLM answers without recalling memories

  let open = false, results = [], sel = 0, timer = 0, ctrl = null, seq = 0, query = '';
  let traceTimers = [];

  const numeral = (c) => clusters()?.[c]?.numeral || '·';

  function renderAsk() {
    const ok = llm();
    askEl.classList.toggle('dis', !ok);
    askEl.querySelector('.ask-label').innerHTML = ok ? '<i>ask</i>' : 'ask · no llm configured';
    freeEl.hidden = !ok;
    freeEl.textContent = free ? 'free chat' : 'from memories';
    freeEl.setAttribute('aria-pressed', String(free));
  }

  // renderTrace lights the first `lit` stages in sequence (all by default);
  // pending stages show no timing yet.
  function renderTrace(trace, lit = Infinity) {
    traceTimers.forEach(clearTimeout);
    traceTimers = [];
    const stages = trace?.stages || [];
    traceEl.innerHTML = stages.map((s, i) => `<div class="tr" data-stage="${esc(s.name)}"><b></b><span>${esc(s.name)}</span><em>${i < lit ? `${Math.round(s.ms)}ms` : ''}</em></div>`).join('');
    [...traceEl.children].forEach((c, i) => { if (i < lit) traceTimers.push(setTimeout(() => c.classList.add('on'), 60 + i * TRACE_STEP_MS)); });
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

  function setMode(next) {
    if (mode === next) return;
    mode = next;
    el.dataset.mode = next;
    list.hidden = next !== 'search';
    conv.hidden = next !== 'ask';
    input.placeholder = next === 'ask' ? 'Follow up…' : 'Search memories…';
    onMode(next);
  }

  function renderConv() {
    conv.innerHTML = turns.map((t, k) => `
      <div class="turn${k === turns.length - 1 ? ' last' : ''}">
        <div class="tq">${esc(t.q)}${t.free ? ' <span class="sc">free chat</span>' : ''}</div>
        <div class="ans">${t.error ? `<span class="err">${esc(t.error)}</span>` : renderAnswer(t.answer)}${t.streaming ? '<span class="caret"></span>' : ''}</div>
        ${t.sources?.length ? `<div class="src">${t.sources.map((s) => `<span data-path="${esc(s.path)}" role="button" tabindex="0"><b>${esc(s.numeral)}</b>${esc(s.title)}</span>`).join('')}</div>` : ''}
      </div>`).join('');
    conv.scrollTop = conv.scrollHeight;
    const t = turns[turns.length - 1];
    if (t) {
      const n = t.sources?.length || 0;
      meta.innerHTML = t.streaming ? `answering · <i>${n} sources</i>` : `<i>${n} sources</i>`;
    }
  }

  let convFrame = 0;
  const renderConvSoon = () => { if (!convFrame) convFrame = requestAnimationFrame(() => { convFrame = 0; renderConv(); }); };

  // ask streams an answer for q, sending earlier turns as history.
  async function ask(q) {
    if (!llm() || asking || !q.trim()) return;
    asking = true;
    ctrl?.abort();
    clearTimeout(timer);
    setMode('ask');
    // Free chat sends one message: the server keeps no history for it.
    const history = free ? [] : turns.filter((t) => !t.error && !t.free).flatMap((t) => [{ role: 'user', content: t.q }, { role: 'assistant', content: t.answer }]);
    const turn = { q, answer: '', sources: [], trace: null, streaming: true, error: '', free };
    turns.push(turn);
    input.value = '';
    renderConv();
    const t0 = performance.now();
    renderTrace({ stages: free ? [{ name: 'synthesize', ms: 0 }] : [{ name: 'embed', ms: 0 }, { name: 'recall', ms: 0 }, { name: 'synthesize', ms: 0 }] }, 0);
    try {
      const res = await fetch('api/chat/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: q, mode: free ? 'free' : 'brain', history }),
      });
      if (res.status === 401) { location.reload(); return; }
      if (!res.ok || !res.body) throw new Error((await res.text()).replace(/[{}"]/g, '').replace(/^error:/, '') || `ask failed (${res.status})`);
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        let nl;
        while ((nl = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, nl);
          buf = buf.slice(nl + 2);
          for (const line of block.split('\n')) {
            if (!line.startsWith('data: ')) continue;
            const evt = JSON.parse(line.slice(6));
            if (evt.error) turn.error = evt.error;
            if (evt.sources) {
              turn.sources = evt.sources;
              showAskInScene(turn);
              renderTrace({ stages: [...evt.trace.stages, { name: 'synthesize', ms: 0 }] }, 2);
            }
            if (evt.content) turn.answer += evt.content;
            if (evt.done && evt.trace) { turn.trace = evt.trace; renderTrace(evt.trace); }
            if (evt.done && turn.free) renderTrace({ stages: [{ name: 'synthesize', ms: performance.now() - t0, count: 0 }] });
          }
          renderConvSoon();
        }
      }
    } catch (err) {
      turn.error = String(err.message || err);
    } finally {
      turn.streaming = false;
      asking = false;
      renderConv();
    }
  }

  function showAskInScene(turn) {
    const ids = (turn.sources || []).map((s) => s.chunk);
    scene.highlight(ids, { color: '#ffffff' });
    labels.setHitClusters(ids.map((id) => scene.node(id)?.cluster).filter((c) => c != null && c >= 0));
    scene.recall(ids.length ? ids : null, sheetTopNdc);
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
    onMode(mode);
    input.focus();
    input.select();
    if (mode === 'ask' && turns.length) {
      renderConv();
      const last = turns[turns.length - 1];
      if (last.trace) renderTrace(last.trace);
      showAskInScene(last);
    } else if (input.value.trim()) {
      run(input.value);
    }
  }

  function hide() {
    if (!open) return;
    open = false;
    // Blur now: the browser only moves focus off a hidden input at its next
    // rendering update, and until then "/" would count as typing.
    if (el.contains(document.activeElement)) document.activeElement.blur();
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
    if (mode === 'ask' && input.value.trim()) setMode('search'); // typing searches again; the conversation stays
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
      e.preventDefault();
      ask(input.value);
    } else if (e.key === 'Enter' && mode === 'ask') {
      e.preventDefault();
      ask(input.value);
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
  // Touch screens have no esc or ⌘↵: a close button and a tappable ask.
  el.querySelector('.x').addEventListener('click', hide);
  askEl.addEventListener('click', () => { if (llm()) ask(input.value); });
  freeEl.addEventListener('click', () => {
    free = !free;
    renderAsk();
    input.focus();
  });
  conv.addEventListener('click', (e) => {
    const src = e.target.closest('.src span');
    if (src) { hide(); onOpen(src.dataset.path); }
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

  el.dataset.mode = mode;
  return { show, hide, ask, get open() { return open; }, get results() { return results; }, get mode() { return mode; }, get turns() { return turns; } };
}
