// Fluctlight Hub app shell: screen routing between Sky, Read and Sys over
// one persistent scene, plus data loading.

import { createScene } from 'app/scene.js';
import { mountLabels } from 'app/labels.js';

const SCREENS = ['sky', 'read', 'sys'];

const stage = document.getElementById('stage');
const params = new URLSearchParams(location.search);

// API paths are relative so the dashboard works under a proxy path prefix.
export async function api(path, init) {
  const res = await fetch(path.replace(/^\//, ''), init);
  if (res.status === 401) {
    location.reload();
    throw new Error('unauthorized');
  }
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
  return res.json();
}

const scene = createScene(document.getElementById('gl'), { forceFallback: params.has('nogl') });
stage.dataset.render = scene.kind;
const labels = mountLabels(document.getElementById('labels'), scene);
// Handle for e2e tests and performance measurements.
window.fluctlight = { scene, get labels() { return labels; } };

function screenFromHash() {
  const s = location.hash.slice(1);
  return SCREENS.includes(s) ? s : 'sky';
}

function go(screen) {
  stage.dataset.s = screen;
  document.querySelectorAll('.rail .ri').forEach((el) => {
    const on = el.dataset.r === screen;
    el.classList.toggle('on', on);
    if (on) el.setAttribute('aria-current', 'page');
    else el.removeAttribute('aria-current');
  });
  scene.setView(screen);
}

// Bottom-right readout: right ascension follows the rotating equator.
function mountFoot() {
  const foot = document.getElementById('foot');
  foot.innerHTML = '<div class="sc">drag to turn the sphere</div><div class="sc">RA <span id="ra">0.00</span>h · Dec 23.4°</div>';
  const ra = foot.querySelector('#ra');
  let shown = '';
  scene.onFrame(({ t }) => {
    const v = (((t * 0.04 * 57.3) / 15) % 24).toFixed(2);
    if (v !== shown) { ra.textContent = v; shown = v; }
  });
}

// Optional fps readout (?fps) for performance checks.
function mountFps() {
  const el = document.createElement('div');
  el.className = 'fps sc';
  stage.appendChild(el);
  setInterval(() => { el.textContent = `${scene.fps.toFixed(0)} fps · ${scene.count} memories · pr ${scene.pixelRatio}`; }, 500);
}

const state = { name: '', vectors: null };

const fmtInt = (n) => Number(n || 0).toLocaleString('en-US');
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const displayName = (s) => (s ? s[0].toUpperCase() + s.slice(1) : 'Kai');

const HOUR_MS = 36e5;
const RESTING_AFTER_MS = 48 * HOUR_MS;

// statusLine describes the last write: coral "resting" after 48 h of quiet,
// otherwise gold "learning" with a relative time.
export function statusLine(lastWrite, now = Date.now()) {
  const t = Date.parse(lastWrite || '');
  if (!Number.isFinite(t)) return { resting: true, text: 'Resting · nothing learned yet' };
  const ago = Math.max(0, now - t);
  if (ago > RESTING_AFTER_MS) {
    const days = Math.floor(ago / (24 * HOUR_MS));
    return { resting: true, text: `Resting · learned ${days} days ago` };
  }
  const mins = Math.floor(ago / 6e4);
  const rel = mins < 2 ? 'just now' : mins < 60 ? `${mins} minutes ago` : `${Math.floor(mins / 60)} ${mins < 120 ? 'hour' : 'hours'} ago`;
  return { resting: false, text: `Learning · ${rel}` };
}

function renderIdentity() {
  const v = state.vectors;
  const memories = v ? v.nodes.length : 0;
  const constellations = v?.clusters?.length;
  const status = statusLine(v?.last_write);
  document.getElementById('identity').innerHTML = `
    <div class="k">Fluctlight of</div>
    <h1 class="nm">${esc(state.name)}</h1>
    <div class="n"><b>${fmtInt(memories)}</b>memories${constellations ? ` · ${constellations} constellations` : ''}</div>
    <div class="s ${status.resting ? 'resting' : 'learning'}">${esc(status.text)}</div>
    <div class="hr"></div>`;
  document.getElementById('query').innerHTML = `
    <span class="ic" aria-hidden="true"></span><span class="ph">Ask ${esc(state.name)} what it remembers…</span><kbd>⌘ K</kbd>`;
}

async function loadInfo() {
  try {
    const info = await api('api/info');
    state.name = displayName(info.agent_name);
    document.title = `Fluctlight of ${state.name}`;
  } catch (err) {
    console.error('load info', err);
    state.name = 'Kai';
  }
}

async function loadVectors() {
  try {
    state.vectors = await api('api/vectors');
    scene.setVectors(state.vectors);
    labels.rebuild();
    stage.dataset.memories = String(scene.count);
  } catch (err) {
    console.error('load vectors', err);
  }
}

addEventListener('hashchange', () => go(screenFromHash()));
go(screenFromHash());
mountFoot();
if (params.has('fps')) mountFps();
Promise.all([loadInfo(), loadVectors()]).then(renderIdentity);
// Keep the status line's relative time current.
setInterval(() => { if (state.vectors) renderIdentity(); }, 60_000);
