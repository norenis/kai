// The Read drawer (spec 7.4, 7.5): every note, filterable and sortable,
// grouped by category; opening a note shows it beside the sky.

import { relativeTime } from 'app/inflow.js';
import { constellationName } from 'app/labels.js';
import { renderMarkdown } from 'app/markdown.js';

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fmtInt = (n) => Number(n || 0).toLocaleString('en-US');

const CATEGORY_ORDER = ['knowledge', 'identity', 'opinions', 'decisions', 'style', 'tasks'];
const SORTS = { recent: 'recent', reads: 'reads', az: 'A-Z' };
const CHIPS_SHOWN = 3;
const NOTE_COLOR = '#ffd27a';
const PIN_HEADER = 'X-Kai-Vault-Pin';

const cap = (s) => (s ? s[0].toUpperCase() + s.slice(1) : s);

// noteClusters maps each note to the constellation holding most of its chunks.
export function noteClusters(nodes) {
  const counts = new Map();
  for (const n of nodes || []) {
    if (n.cluster < 0) continue;
    const c = counts.get(n.doc) || new Map();
    c.set(n.cluster, (c.get(n.cluster) || 0) + 1);
    counts.set(n.doc, c);
  }
  const out = new Map();
  for (const [doc, c] of counts) {
    let best = -1, bestN = -1;
    for (const [k, n] of c) if (n > bestN || (n === bestN && k < best)) { best = k; bestN = n; }
    out.set(doc, best);
  }
  return out;
}

// noteBody drops the leading "# Title" heading, which the view already
// shows as the note's title.
export function noteBody(content, title) {
  const m = /^\s*# +(.+?)\s*(?:\n|$)/.exec(content || '');
  return m && m[1].trim() === String(title || '').trim() ? content.slice(m[0].length) : content || '';
}

// copyText uses the clipboard API, which needs a secure context, and falls
// back to a hidden textarea on plain http.
async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;opacity:0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  }
}

// filterNotes applies the text filter (title, path, tags, constellation) and
// the constellation chip, then sorts.
export function filterNotes(files, { text = '', cluster = null, sort = 'recent', clusterOf = () => -1, clusters = [] } = {}) {
  const q = text.trim().toLowerCase();
  const words = q ? q.split(/\s+/) : [];
  const out = files.filter((f) => {
    const c = clusterOf(f.path);
    if (cluster !== null && c !== cluster) return false;
    if (!words.length) return true;
    const cl = clusters[c];
    const hay = [f.title, f.path, ...(f.tags || []), cl ? `${cl.numeral} ${cl.label}` : ''].join(' ').toLowerCase();
    return words.every((w) => hay.includes(w));
  });
  const byTitle = (a, b) => a.title.localeCompare(b.title);
  if (sort === 'reads') out.sort((a, b) => (b.access_count || 0) - (a.access_count || 0) || byTitle(a, b));
  else if (sort === 'az') out.sort(byTitle);
  else out.sort((a, b) => (b.updated || '').localeCompare(a.updated || '') || byTitle(a, b));
  return out;
}

// mountReader builds the drawer inside el.
//   api, scene, data() → {vectors, clusterOf}, onClose(), onOpen(path)
export function mountReader(el, { api, scene, data, onClose, onOpen }) {
  let files = null, stale = true, loading = null;
  let text = '', cluster = null, sort = 'recent', chipsOpen = false;
  let mode = 'list';

  el.innerHTML = `
    <div class="list-view">
      <div class="dh">
        <div class="t"><h2>Read</h2><span class="sc count"></span><button class="sc x" type="button" aria-label="Close">esc</button></div>
        <div class="filter">
          <span class="ic" aria-hidden="true"></span>
          <input type="search" autocomplete="off" spellcheck="false" placeholder="Filter by name, tag or constellation…" aria-label="Filter notes">
          <button class="sc sort" type="button" aria-label="Sort"></button>
        </div>
        <div class="chips" role="group" aria-label="Constellations"></div>
      </div>
      <div class="dl" role="list"></div>
    </div>
    <div class="note-view" hidden></div>`;
  const listView = el.querySelector('.list-view');
  const noteView = el.querySelector('.note-view');
  const input = el.querySelector('.filter input');
  const sortBtn = el.querySelector('.sort');
  const chipsEl = el.querySelector('.chips');
  const listEl = el.querySelector('.dl');
  const countEl = el.querySelector('.count');

  const clusters = () => data().vectors?.clusters || [];
  const clusterOf = (path) => data().clusterOf(path);
  const numeral = (path) => clusters()[clusterOf(path)]?.numeral || '·';

  async function load() {
    if (!stale && files) return files;
    if (!loading) {
      loading = api('api/files').then((d) => {
        files = (d.files || []).map((f) => ({ ...f, title: f.title || f.path }));
        stale = false;
        return files;
      }).finally(() => { loading = null; });
    }
    return loading;
  }

  function renderChips() {
    const cls = clusters();
    const shown = chipsOpen ? cls : cls.slice(0, CHIPS_SHOWN);
    const chip = (c, label) => `<button type="button" class="${cluster === c ? 'on' : ''}" data-c="${c === null ? '' : c}">${label}</button>`;
    chipsEl.innerHTML = chip(null, 'All') +
      shown.map((c) => chip(c.id, `${esc(c.numeral)} ${esc(constellationName(c.label))}`)).join('') +
      (!chipsOpen && cls.length > CHIPS_SHOWN ? `<button type="button" class="more">+${cls.length - CHIPS_SHOWN}</button>` : '');
  }

  function renderList() {
    if (!files) { listEl.innerHTML = '<div class="empty sc">loading…</div>'; return; }
    sortBtn.textContent = `${SORTS[sort]} ▾`;
    const shown = filterNotes(files, { text, cluster, sort, clusterOf, clusters: clusters() });
    countEl.textContent = shown.length === files.length ? `${fmtInt(files.length)} notes` : `${fmtInt(shown.length)} of ${fmtInt(files.length)} notes`;
    const groups = new Map();
    for (const f of shown) {
      const g = groups.get(f.category) || [];
      g.push(f);
      groups.set(f.category, g);
    }
    const order = [...groups.keys()].sort((a, b) => {
      const ia = CATEGORY_ORDER.indexOf(a), ib = CATEGORY_ORDER.indexOf(b);
      return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.localeCompare(b);
    });
    listEl.innerHTML = order.map((cat) => `
      <div class="gr"><span class="sc">${esc(cap(cat))} · ${fmtInt(groups.get(cat).length)}</span></div>
      ${groups.get(cat).map((f) => `
        <div class="li" role="listitem" tabindex="0" data-path="${esc(f.path)}">
          <span class="rn2">${esc(numeral(f.path))}</span>
          <div><div class="t">${esc(f.title)}</div><div class="sc">${esc((f.tags || []).slice(0, 3).join(' · '))}</div></div>
          <div class="m">${esc(relativeTime(f.updated))}<br>${fmtInt(f.word_count)} words</div>
        </div>`).join('')}`).join('') || '<div class="empty sc">no notes match</div>';
  }

  // Hovering a row lights the note's memories in gold.
  function hover(path) {
    scene.highlight(path ? scene.chunksOf(path) : [], { color: NOTE_COLOR });
  }

  input.addEventListener('input', () => { text = input.value; renderList(); });
  sortBtn.addEventListener('click', () => {
    const keys = Object.keys(SORTS);
    sort = keys[(keys.indexOf(sort) + 1) % keys.length];
    renderList();
  });
  chipsEl.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    if (b.classList.contains('more')) { chipsOpen = true; renderChips(); return; }
    cluster = b.dataset.c === '' ? null : Number(b.dataset.c);
    renderChips();
    renderList();
  });
  listEl.addEventListener('mouseover', (e) => {
    const row = e.target.closest('.li');
    if (row) hover(row.dataset.path);
  });
  listEl.addEventListener('mouseleave', () => hover(null));
  listEl.addEventListener('click', (e) => {
    const row = e.target.closest('.li');
    if (row) onOpen(row.dataset.path);
  });
  listEl.addEventListener('keydown', (e) => {
    const row = e.target.closest('.li');
    if (!row) return;
    if (e.key === 'Enter') onOpen(row.dataset.path);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const rows = [...listEl.querySelectorAll('.li')];
      rows[rows.indexOf(row) + (e.key === 'ArrowDown' ? 1 : -1)]?.focus();
    }
  });
  el.querySelector('.x').addEventListener('click', onClose);

  // ---------- note view ----------

  let note = null; // {path, file, related} of the open note
  let opening = 0; // ignores responses for a note that is no longer wanted
  let pin = ''; // vault PIN, kept in memory for this page only

  // fetchNote reads a note and its neighbours; null means the vault PIN is
  // needed (or was wrong).
  async function fetchNote(path) {
    const q = `?path=${encodeURIComponent(path)}`;
    const headers = pin ? { [PIN_HEADER]: pin } : {};
    const res = await fetch(`api/file${q}`, { headers });
    if (res.status === 401) { location.reload(); throw new Error('unauthorized'); }
    if (res.status === 403) return null;
    if (!res.ok) throw new Error(`api/file: ${res.status}`);
    const file = await res.json();
    let related = [];
    try {
      const r = await fetch(`api/related${q}`, { headers });
      if (r.ok) related = (await r.json()).related || [];
    } catch (err) {
      console.error('load related', err);
    }
    return { file, related };
  }

  function crumb(path) {
    const cat = path.split('/')[0];
    const cl = clusters()[clusterOf(path)];
    return `<div class="crumb sc">
      <a href="#read">← Read</a><span>·</span><span>${esc(cat)}</span>
      ${cl ? `<span>·</span><span><i>${esc(cl.numeral)}</i> ${esc(constellationName(cl.label))}</span>` : ''}
      <button class="sc x" type="button" aria-label="Close">esc</button></div>`;
  }

  function lightNote() {
    if (!note || mode !== 'note') return;
    const secondary = note.related.flatMap((r) => scene.chunksOf(r.path));
    scene.highlight(scene.chunksOf(note.path), { color: NOTE_COLOR, secondary });
  }

  function renderNote() {
    const { path, file: f, related } = note;
    const fragments = f.chunk_count || scene.chunksOf(path).length;
    const plural = (n, w) => `${fmtInt(n)} ${w}${n === 1 ? '' : 's'}`;
    const meta = [
      f.confidence && `${f.confidence} confidence`,
      f.source,
      relativeTime(f.updated),
      fragments && plural(fragments, 'fragment'),
      plural(f.word_count || 0, 'word'),
      plural(f.access_count || 0, 'read'),
    ].filter(Boolean);
    const top = related.length ? related[0].score : 1;
    noteView.innerHTML = `${crumb(path)}
      <div class="nv">
        <h2 class="nt">${esc(f.title || path)}</h2>
        <div class="sc meta">${meta.map(esc).join(' · ')}</div>
        ${(f.tags || []).length ? `<div class="tags">${f.tags.map((t) => `<span>${esc(t)}</span>`).join('')}</div>` : ''}
        <div class="md">${renderMarkdown(noteBody(f.content, f.title))}</div>
        <div class="near">
          <div class="sc">Nearest in the sky</div>
          ${related.map((r) => `
            <div class="ri2" role="link" tabindex="0" data-path="${esc(r.path)}">
              <span class="rn2">${esc(clusters()[r.cluster]?.numeral || '·')}</span>
              <span class="t">${esc(r.title)}</span>
              <span class="bar"><i style="width:${Math.max(6, Math.round((r.score / top) * 100))}%"></i></span>
            </div>`).join('') || '<div class="empty sc">no close neighbours yet</div>'}
        </div>
      </div>`;
    for (const pre of noteView.querySelectorAll('.md pre')) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'copy sc';
      b.textContent = 'copy';
      pre.appendChild(b);
    }
  }

  function renderPin(path, wrong) {
    noteView.innerHTML = `${crumb(path)}
      <form class="nv pin">
        <h2 class="nt">Vault</h2>
        <div class="sc meta">${wrong ? '<span class="co">wrong pin</span>' : 'this note is locked'}</div>
        <div class="filter"><span class="ic" aria-hidden="true"></span>
          <input type="password" inputmode="numeric" autocomplete="off" placeholder="Vault PIN…" aria-label="Vault PIN"></div>
      </form>`;
    const form = noteView.querySelector('form');
    form.querySelector('input').focus();
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      pin = form.querySelector('input').value;
      openNote(path, { force: true, retry: true });
    });
  }

  async function openNote(path, { force = false, retry = false } = {}) {
    mode = 'note';
    listView.hidden = true;
    noteView.hidden = false;
    hover(null);
    if (!force && note?.path === path) { lightNote(); return; }
    const token = ++opening;
    if (note?.path !== path) {
      note = null;
      noteView.innerHTML = `${crumb(path)}<div class="nv"><div class="empty sc">loading…</div></div>`;
    }
    let got;
    try {
      got = await fetchNote(path);
    } catch (err) {
      console.error('load note', err);
      if (token === opening) noteView.innerHTML = `${crumb(path)}<div class="nv"><div class="empty sc">note not found</div></div>`;
      return;
    }
    if (token !== opening || mode !== 'note') return;
    if (!got) {
      if (retry) pin = '';
      renderPin(path, retry);
      return;
    }
    const scroll = note?.path === path ? noteView.querySelector('.nv')?.scrollTop : 0;
    note = { path, ...got };
    renderNote();
    const nv = noteView.querySelector('.nv');
    if (nv) nv.scrollTop = scroll || 0;
    lightNote();
  }

  noteView.addEventListener('click', async (e) => {
    if (e.target.closest('.x')) { onClose(); return; }
    const copy = e.target.closest('.copy');
    if (copy) {
      copy.textContent = (await copyText(copy.parentElement.querySelector('code')?.textContent ?? '')) ? 'copied' : 'copy failed';
      setTimeout(() => { copy.textContent = 'copy'; }, 1500);
      return;
    }
    const row = e.target.closest('.ri2');
    if (row) onOpen(row.dataset.path);
  });
  noteView.addEventListener('keydown', (e) => {
    const row = e.target.closest('.ri2');
    if (row && e.key === 'Enter') onOpen(row.dataset.path);
  });
  // Hovering a neighbour lights it in gold beside the note.
  noteView.addEventListener('mouseover', (e) => {
    const row = e.target.closest('.ri2');
    if (row) scene.highlight(scene.chunksOf(row.dataset.path), { color: NOTE_COLOR, secondary: scene.chunksOf(note?.path) });
  });
  noteView.addEventListener('mouseout', (e) => {
    const row = e.target.closest('.ri2');
    if (row && !row.contains(e.relatedTarget)) lightNote();
  });

  async function showList() {
    mode = 'list';
    listView.hidden = false;
    noteView.hidden = true;
    hover(null);
    renderChips();
    renderList();
    await load();
    renderChips();
    renderList();
  }

  return {
    showList,
    showNote: (path) => openNote(path),
    get mode() { return mode; },
    get note() { return note; },
    // markStale reloads the list next time (after brain writes); an open
    // note is read again so edits show up.
    markStale() {
      stale = true;
      if (el.hidden) return;
      if (mode === 'list') load().then(renderList);
      else if (note) openNote(note.path, { force: true });
    },
    leave() { hover(null); mode = 'list'; },
    get rows() { return [...listEl.querySelectorAll('.li')].map((r) => r.dataset.path); },
  };
}
