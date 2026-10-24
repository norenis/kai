// Fluctlight Hub app shell: screen routing between Sky, Read and Sys over
// one persistent scene, plus data loading.

import { createScene } from 'app/scene.js';

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
// Handle for e2e tests and performance measurements.
window.fluctlight = { scene };

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

async function loadVectors() {
  try {
    const vectors = await api('api/vectors');
    scene.setVectors(vectors);
    stage.dataset.memories = String(scene.count);
  } catch (err) {
    console.error('load vectors', err);
  }
}

addEventListener('hashchange', () => go(screenFromHash()));
go(screenFromHash());
mountFoot();
if (params.has('fps')) mountFps();
loadVectors();
