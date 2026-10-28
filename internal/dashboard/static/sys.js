// The Sys page (spec 7.6): maintenance, with the Overview tab here and the
// action tabs filled in by later modules. The scene keeps running as a live
// thumbnail in the top-right corner.

import { relativeTime } from 'app/inflow.js';
import { constellationName } from 'app/labels.js';
import { renderTasks, renderIndex, renderReorganize } from 'app/sys-actions.js';

// Tabs ported so far; the rest point to /classic.
const ACTION_TABS = { tasks: renderTasks, index: renderIndex, reorganize: renderReorganize };

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fmtInt = (n) => Number(n || 0).toLocaleString('en-US');

export const TABS = [
  ['overview', 'Overview'],
  ['tasks', 'Tasks'],
  ['index', 'Index'],
  ['reorganize', 'Reorganize'],
  ['import', 'Import'],
  ['plugins', 'Plugins & tools'],
  ['sync', 'Sync'],
];

const DAY_MS = 864e5;
const LEARNING_DAYS = 14;
const QUIET_AFTER_DAYS = 2;
const RECENT_ROWS = 10;
const TAGS_SHOWN = 14;
// A note is unusual when it is this many times its category's median size.
const ANOMALY_FACTOR = 10;
const ANOMALY_MIN_WORDS = 500;
const ANOMALY_MIN_NOTES = 3;

const dayKey = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
const shortDate = (key) => {
  const [y, m, d] = key.split('-').map(Number);
  return new Date(y, m - 1, d).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
};

// learningDays counts notes learned (last updated) on each of the last 14
// days, oldest first, and says since when the brain has been quiet (null
// when it learned something in the last two days).
export function learningDays(files, now = Date.now()) {
  const counts = new Map();
  let newest = '';
  for (const f of files) {
    if (!f.updated) continue;
    counts.set(f.updated, (counts.get(f.updated) || 0) + 1);
    if (f.updated > newest) newest = f.updated;
  }
  const today = new Date(new Date(now).toDateString());
  const days = [];
  for (let i = LEARNING_DAYS - 1; i >= 0; i--) {
    const key = dayKey(new Date(today.getTime() - i * DAY_MS + 12 * 36e5)); // noon avoids DST edges
    days.push({ day: key, notes: counts.get(key) || 0 });
  }
  const quietSince = newest && newest < dayKey(new Date(today.getTime() - QUIET_AFTER_DAYS * DAY_MS + 12 * 36e5)) ? newest : null;
  return { days, quietSince };
}

// anomalies flags notes far larger than is usual for their category.
export function anomalies(files) {
  const byCat = new Map();
  for (const f of files) {
    const c = byCat.get(f.category) || [];
    c.push(f.word_count || 0);
    byCat.set(f.category, c);
  }
  const median = new Map();
  for (const [cat, ws] of byCat) {
    if (ws.length < ANOMALY_MIN_NOTES) continue;
    const s = [...ws].sort((a, b) => a - b);
    const mid = s.length >> 1;
    median.set(cat, s.length % 2 ? s[mid] : (s[mid - 1] + s[mid]) / 2);
  }
  const out = new Set();
  for (const f of files) {
    const m = median.get(f.category);
    if (m && f.word_count >= ANOMALY_MIN_WORDS && f.word_count > ANOMALY_FACTOR * m) out.add(f.path);
  }
  return out;
}

// recentRows is the latest notes plus any unusual ones, newest first.
export function recentRows(files, unusual, n = RECENT_ROWS) {
  const sorted = [...files].sort((a, b) => (b.updated || '').localeCompare(a.updated || '') || a.path.localeCompare(b.path));
  return sorted.filter((f, i) => i < n || unusual.has(f.path));
}

// mountSys builds the page inside el.
//   api, data() → {vectors}, onOpen(path), reloadVectors()
export function mountSys(el, { api, data, onOpen, reloadVectors }) {
  let tab = 'overview';
  let pendingAct = null; // an Overview action link to run once Index opens
  let cache = null, stale = true, loading = null;

  el.innerHTML = `
    <header class="sh">
      <h2>Maintenance</h2>
      <div class="sc sum"></div>
      <nav class="tabs" aria-label="Maintenance sections">
        ${TABS.map(([id, label]) => `<a href="#sys/${id}" data-t="${id}">${esc(label)}</a>`).join('')}
      </nav>
    </header>
    <div class="sb"></div>`;
  const sumEl = el.querySelector('.sum');
  const body = el.querySelector('.sb');

  async function load() {
    if (!stale && cache) return cache;
    if (!loading) {
      loading = Promise.all([api('api/overview'), api('api/files'), api('api/tags')])
        .then(([overview, files, tags]) => {
          cache = { overview, files: files.files || [], tags: tags.tags || [] };
          stale = false;
          return cache;
        })
        .finally(() => { loading = null; });
    }
    return loading;
  }

  function renderSummary() {
    const v = data().vectors;
    const parts = [];
    if (cache) parts.push(`${fmtInt(cache.files.length)} notes`, `${fmtInt(cache.overview.total_words)} words`);
    if (v) parts.push(`${fmtInt(v.nodes.length)} memories`, `${v.clusters?.length || 0} constellations`);
    sumEl.textContent = parts.join(' · ');
  }

  function constellations(v) {
    const cls = v?.clusters || [];
    const max = Math.max(1, ...cls.map((c) => c.size));
    return `<section class="col">
      <h3 class="sc">Constellations</h3>
      ${cls.map((c) => `
        <div class="cst">
          <span class="rn2">${esc(c.numeral)}</span>
          <span class="t">${esc(constellationName(c.label))}</span>
          <span class="n">${fmtInt(c.size)}</span>
          <span class="bar"><i style="width:${Math.round((c.size / max) * 100)}%"></i></span>
        </div>`).join('') || '<div class="empty sc">no constellations yet</div>'}
    </section>`;
  }

  function learning(files) {
    const { days, quietSince } = learningDays(files);
    const max = Math.max(1, ...days.map((d) => d.notes));
    const total = days.reduce((s, d) => s + d.notes, 0);
    return `<section class="col">
      <h3 class="sc">Learning · ${LEARNING_DAYS} days</h3>
      <div class="days" role="img" aria-label="${total} notes learned in the last ${LEARNING_DAYS} days">
        ${days.map((d) => {
          const size = d.notes ? Math.round(5 + 11 * Math.sqrt(d.notes / max)) : 3;
          return `<span class="dd${d.notes ? ' on' : ''}" data-day="${d.day}" data-notes="${d.notes}" title="${esc(shortDate(d.day))} · ${d.notes} ${d.notes === 1 ? 'note' : 'notes'}"><i style="width:${size}px;height:${size}px"></i></span>`;
        }).join('')}
      </div>
      <div class="sc dl2"><span>${esc(shortDate(days[0].day))}</span><span>today</span></div>
      <div class="big"><b>${fmtInt(total)}</b> notes learned</div>
      ${quietSince ? `<div class="sc quiet"><span class="co">quiet since ${esc(shortDate(quietSince))}</span></div>` : ''}
    </section>`;
  }

  function index(o, v) {
    const memories = v?.nodes?.length || 0;
    const rows = [
      ['memories embedded', fmtInt(memories)],
      ['links', fmtInt(v?.links?.length || 0)],
      ['dims · provider', v?.dims ? `${v.dims} · ${v.provider || '?'}` : 'no vectors'],
      ['bm25', `${o.index_health || 'missing'} · ${fmtInt(o.total_chunks)} chunks`],
    ];
    return `<section class="col">
      <h3 class="sc">Index</h3>
      ${rows.map(([k, val]) => `<div class="kv"><span class="sc">${esc(k)}</span><span class="v${k === 'bm25' && o.index_health !== 'ok' ? ' co' : ''}">${esc(val)}</span></div>`).join('')}
      <div class="acts">
        <a href="#sys/index" data-act="rebuild">Rebuild embeddings</a>
        <a href="#sys/index" data-act="reindex">Reindex</a>
        <a href="#sys/index" data-act="decay">Decay · dry run</a>
      </div>
    </section>`;
  }

  function activity(files, unusual) {
    const rows = recentRows(files, unusual);
    return `<section class="col wide">
      <h3 class="sc">Recent activity</h3>
      <table class="act">
        <thead><tr><th class="sc">note</th><th class="sc">category</th><th class="sc">when</th><th class="sc num">words</th><th class="sc num">reads</th></tr></thead>
        <tbody>${rows.map((f) => `
          <tr data-path="${esc(f.path)}"${unusual.has(f.path) ? ' class="unusual"' : ''} tabindex="0">
            <td class="t">${esc(f.title || f.path)}${unusual.has(f.path) ? ' <span class="sc flag">unusual</span>' : ''}</td>
            <td class="sc">${esc(f.category)}</td>
            <td>${esc(relativeTime(f.updated))}</td>
            <td class="num">${fmtInt(f.word_count)}</td>
            <td class="num">${fmtInt(f.access_count)}</td>
          </tr>`).join('')}</tbody>
      </table>
    </section>`;
  }

  function tags(list) {
    const shown = list.slice(0, TAGS_SHOWN);
    const max = Math.max(1, ...shown.map((t) => t.count));
    return `<section class="col">
      <h3 class="sc">Most used tags</h3>
      <div class="tg">${shown.map((t) => `<span style="font-size:${Math.round(14 + 8 * (t.count / max))}px">${esc(t.name)}<b>${fmtInt(t.count)}</b></span>`).join('') || '<div class="empty sc">no tags yet</div>'}</div>
    </section>`;
  }

  function renderOverview() {
    if (!cache) { body.innerHTML = '<div class="empty sc">loading…</div>'; return; }
    const v = data().vectors;
    const unusual = anomalies(cache.files);
    body.innerHTML = `
      <div class="ov">
        <div class="row3">${constellations(v)}${learning(cache.files)}${index(cache.overview, v)}</div>
        <div class="row2">${activity(cache.files, unusual)}${tags(cache.tags)}</div>
      </div>`;
  }

  function renderTab() {
    el.querySelectorAll('.tabs a').forEach((a) => {
      const on = a.dataset.t === tab;
      a.classList.toggle('on', on);
      if (on) a.setAttribute('aria-current', 'page');
      else a.removeAttribute('aria-current');
    });
    renderSummary();
    if (tab === 'overview') renderOverview();
    else if (ACTION_TABS[tab]) {
      ACTION_TABS[tab](body, { api, data, onOpen, reloadVectors });
      if (pendingAct) body.querySelector(`button.lk[data-a="${pendingAct}"]`)?.click();
      pendingAct = null;
    } else body.innerHTML = `<div class="soon"><p>This section is moving here.</p><p class="sc">meanwhile it lives in the <a href="classic">classic dashboard</a></p></div>`;
  }

  // The thumbnail sits over the page's top-right corner; once the page
  // scrolls past the header it fades out of the way.
  const SCROLL_HIDE = 48;
  el.addEventListener('scroll', () => el.parentElement.toggleAttribute('data-scrolled', el.scrollTop > SCROLL_HIDE), { passive: true });

  body.addEventListener('click', (e) => {
    const act = e.target.closest('.ov a[data-act]');
    if (act) { pendingAct = act.dataset.act; return; } // its href opens the Index tab
    const row = e.target.closest('.ov tr[data-path]');
    if (row) onOpen(row.dataset.path);
  });
  body.addEventListener('keydown', (e) => {
    const row = e.target.closest('.ov tr[data-path]');
    if (row && e.key === 'Enter') onOpen(row.dataset.path);
  });

  return {
    // show renders a tab. Action tabs render once per visit so a write
    // made by their own action does not wipe its result.
    async show(name) {
      tab = TABS.some(([id]) => id === name) ? name : 'overview';
      renderTab();
      await load();
      renderSummary();
      if (!el.hidden && tab === 'overview') renderOverview();
    },
    get tab() { return tab; },
    // markStale reloads the numbers after brain writes (or new vectors).
    markStale() {
      stale = true;
      if (el.hidden) return;
      load().then(() => {
        if (el.hidden) return;
        renderSummary();
        if (tab === 'overview') renderOverview();
      });
    },
  };
}
