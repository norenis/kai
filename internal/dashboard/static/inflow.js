// Live brain activity: the SSE client and the Sky's Inflow panel (spec 7.1).

const WRITE_TYPES = new Set(['learn', 'create', 'update', 'delete']);
const VERB = { learn: 'learned', create: 'learned', update: 'updated', delete: 'removed' };
const ROWS = 3;
const RECONNECT_MS = 5000;
const DAY_MS = 864e5;

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// filenameTitle mirrors brain.FilenameTitle for notes the sky does not know yet.
export function filenameTitle(path) {
  const base = String(path).split('/').pop().replace(/\.md$/, '');
  if (/^\d{4}-\d{2}-\d{2}$/.test(base)) return base;
  const name = base.split(/[-_]+/).filter(Boolean).join(' ');
  return name ? name[0].toUpperCase() + name.slice(1) : path;
}

// relativeTime renders an ISO time or a YYYY-MM-DD date for small caps lines.
export function relativeTime(when, now = Date.now()) {
  if (!when) return '';
  if (/^\d{4}-\d{2}-\d{2}$/.test(when)) {
    const [y, m, d] = when.split('-').map(Number);
    const days = Math.round((new Date(new Date(now).toDateString()) - new Date(y, m - 1, d)) / DAY_MS);
    return days <= 0 ? 'today' : days === 1 ? 'yesterday' : `${days} days ago`;
  }
  const t = Date.parse(when);
  if (!Number.isFinite(t)) return '';
  const mins = Math.floor(Math.max(0, now - t) / 6e4);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`;
  const days = Math.floor(hours / 24);
  return days === 1 ? 'yesterday' : `${days} days ago`;
}

// connectEvents keeps an EventSource open; the browser retries on its own
// after drops, and a closed stream (e.g. an expired login) is reopened after
// a pause with an auth probe first.
export function connectEvents(url, onEvent, probe) {
  let es = null;
  const open = () => {
    es = new EventSource(url);
    es.onmessage = (m) => {
      try { onEvent(JSON.parse(m.data)); } catch (err) { console.error('event', err); }
    };
    es.onerror = () => {
      if (es.readyState !== EventSource.CLOSED) return;
      setTimeout(async () => { try { await probe(); } catch { /* reload or retry */ } open(); }, RECONNECT_MS);
    };
  };
  open();
  return { close: () => es?.close() };
}

// mountInflow renders the last write events and plays every event on the scene.
//   titleOf(path) → title, onOpen(path), onLock(path|null), onWrite(event)
export function mountInflow(el, scene, { titles, onOpen, onLock, onWrite }) {
  const titleOf = (path) => titles.get(path) || filenameTitle(path);
  let rows = []; // {key, path, type, when, live}
  const startedAt = Date.now();

  function render() {
    el.innerHTML = `<div class="k">Inflow</div>` + rows.map((r) => `
      <div class="ev" data-key="${esc(r.key)}" data-path="${esc(r.path)}"${r.arrived ? ' data-arrived="1"' : ''} role="button" tabindex="0">
        <div class="t">${esc(titleOf(r.path))}</div>
        <div class="sc"><i>${VERB[r.type] || r.type}</i> · ${esc(relativeTime(r.when))}</div>
      </div>`).join('');
  }

  function glowRow(key) {
    const r = rows.find((x) => x.key === key);
    if (r) r.arrived = true;
    const row = el.querySelector(`.ev[data-key="${CSS.escape(key)}"]`);
    if (!row) return;
    row.classList.remove('new');
    void row.offsetWidth; // restart the animation
    row.classList.add('new');
    row.dataset.arrived = '1';
  }

  function push(row) {
    rows = [row, ...rows.filter((r) => r.path !== row.path)].slice(0, ROWS);
    render();
  }

  // seed shows recent writes from before the page opened.
  function seed(activity) {
    if (rows.length) return;
    rows = (activity || []).slice(0, ROWS).map((a) => ({ key: `seed:${a.path}`, path: a.path, type: 'learn', when: a.updated }));
    render();
  }

  function handle(ev) {
    // Events replayed on connect happened before this page: list them, but
    // only play effects for what happens while watching.
    const live = Date.parse(ev.ts) >= startedAt - 2000;
    if (ev.title) titles.set(ev.path, ev.title);
    if (WRITE_TYPES.has(ev.type)) {
      const key = `ev:${ev.id}`;
      push({ key, path: ev.path, type: ev.type, when: ev.ts });
      onWrite?.(ev, live);
      if (live) scene.onEvent(ev, { onArrive: () => glowRow(key) });
    } else if (live) {
      scene.onEvent(ev);
    }
  }

  el.addEventListener('mouseover', (e) => {
    const row = e.target.closest('.ev');
    if (row) onLock(row.dataset.path);
  });
  el.addEventListener('mouseleave', () => onLock(null));
  el.addEventListener('click', (e) => {
    const row = e.target.closest('.ev');
    if (row) onOpen(row.dataset.path);
  });
  el.addEventListener('keydown', (e) => {
    const row = e.target.closest('.ev');
    if (row && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onOpen(row.dataset.path); }
  });
  setInterval(render, 30_000); // keep relative times current

  render();
  return { handle, seed, render, get rows() { return rows; } };
}
