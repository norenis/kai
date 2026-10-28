// HTML overlays projected from the 3D scene each frame: constellation labels
// and the Roman numerals orbiting just outside the equator (spec 5.2).

import { ARMILLARY_RADIUS } from 'app/scene.js';

const SKY_LABELS = 3;
const NUMERAL_RADIUS = ARMILLARY_RADIUS + 0.2;
// Labels keep this many pixels clear of each other and of the fixed overlays.
const LABEL_GAP = 8;
const OBSTACLES = ['#identity', '#inflow', '#query', '#sheet', '.rail'];
const OBSTACLE_REFRESH_MS = 500;

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// constellationName turns "go · testing" into "Go & Testing".
export function constellationName(label) {
  return String(label || '')
    .split(' · ')
    .map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w))
    .join(' & ');
}

export function mountLabels(layer, scene) {
  let labels = [], numerals = [];
  // Callout beside the locked memory: {id, html} or null.
  let lock = null;
  const lockEl = document.createElement('div');
  lockEl.className = 'lock';
  // Clusters whose labels show on search/ask screens (set from Task 9 on).
  let hitClusters = new Set();

  function rebuild() {
    layer.replaceChildren(lockEl);
    labels = scene.constellations.map((c) => {
      const el = document.createElement('div');
      el.className = 'cl';
      el.dataset.cluster = c.cluster.id;
      el.innerHTML = `<div class="in"><div class="t">${esc(constellationName(c.cluster.label))}</div><div class="sc">${esc(c.cluster.numeral)} · ${c.cluster.size.toLocaleString('en-US')} memories</div></div>`;
      layer.appendChild(el);
      return { c, el, w: 0, h: 0 };
    });
    measure();
    const clusters = scene.constellations.map((c) => c.cluster);
    numerals = clusters.map((cl, i) => {
      const el = document.createElement('div');
      el.className = 'rn';
      el.innerHTML = `<span class="in">${esc(cl.numeral)}</span>`;
      layer.appendChild(el);
      return { el, angle: (i / clusters.length) * Math.PI * 2 };
    });
  }

  // Label sizes, re-read when fonts or the layout change.
  function measure() {
    for (const l of labels) {
      const box = l.el.firstElementChild;
      l.w = box.offsetWidth;
      l.h = box.offsetHeight;
    }
  }
  document.fonts?.ready.then(measure);
  addEventListener('resize', () => { measure(); obstaclesAt = 0; });

  // Fixed overlays (identity, inflow, query, sheet, rail) as rectangles in layer
  // pixels; refreshed twice a second rather than read every frame.
  let obstacles = [], obstaclesAt = 0, width = 0, height = 0;
  function refreshObstacles(now) {
    if (now - obstaclesAt < OBSTACLE_REFRESH_MS) return;
    obstaclesAt = now;
    const origin = layer.getBoundingClientRect();
    width = origin.width; height = origin.height;
    obstacles = [];
    for (const sel of OBSTACLES) {
      const r = document.querySelector(sel)?.getBoundingClientRect();
      if (r && r.width && r.height) obstacles.push({ x0: r.left - origin.left, y0: r.top - origin.top, x1: r.right - origin.left, y1: r.bottom - origin.top });
    }
  }
  const overlaps = (a, b) => a.x0 < b.x1 + LABEL_GAP && b.x0 < a.x1 + LABEL_GAP && a.y0 < b.y1 + LABEL_GAP && b.y0 < a.y1 + LABEL_GAP;

  const place = (el, x, y, opacity) => {
    el.style.transform = `translate(${x.toFixed(1)}px, ${y.toFixed(1)}px)`;
    el.style.opacity = opacity;
  };

  scene.onFrame(({ view, project, ringAngle }) => {
    // Collision hiding: larger clusters are placed first; a label that would
    // touch one already placed (or an overlay) is hidden.
    refreshObstacles(performance.now());
    const shown = [];
    for (const l of labels) {
      const [x, y, z] = l.c.anchor;
      const p = project(x, y, z);
      const id = l.c.cluster.id;
      const visible = view === 'sky' ? id < SKY_LABELS : (view === 'search' || view === 'ask') ? hitClusters.has(id) : false;
      l.x = p.x; l.y = p.y;
      l.opacity = visible && !p.behind ? (p.facing > -0.35 ? 1 : 0.3) : 0;
      if (l.opacity) shown.push(l);
    }
    shown.sort((a, b) => b.c.cluster.size - a.c.cluster.size);
    const taken = obstacles.slice();
    for (const l of shown) {
      // Anchored bottom-centre (see .cl .in in theme.css); slid inside the
      // stage on narrow screens rather than cut off at the edge.
      const half = l.w / 2;
      if (l.w + 2 * LABEL_GAP < width) l.x = Math.min(Math.max(l.x, half + LABEL_GAP), width - half - LABEL_GAP);
      const box = { x0: l.x - half, x1: l.x + half, y0: l.y - l.h, y1: l.y };
      if (box.y0 < 0 || box.y1 > height || taken.some((o) => overlaps(box, o))) l.opacity = 0;
      else taken.push(box);
    }
    for (const l of labels) place(l.el, l.x, l.y, l.opacity);
    const pos = lock && (view === 'sky' || view === 'note') ? scene.position(lock.id) : null;
    if (pos) {
      const p = project(...pos);
      // Right-aligned block left of and below the ring, as in the mock
      // (kept on screen on narrow stages).
      place(lockEl, Math.max(LABEL_GAP, p.x - 280), p.y + 26, p.behind ? 0 : 1);
    } else {
      lockEl.style.opacity = 0;
    }

    const showNumerals = view === 'sky' || view === 'read' || view === 'note';
    for (const n of numerals) {
      const a = n.angle + ringAngle;
      const p = project(Math.cos(a) * NUMERAL_RADIUS, 0, Math.sin(a) * NUMERAL_RADIUS);
      place(n.el, p.x, p.y, showNumerals ? (p.facing > -0.2 ? 0.95 : 0.25) : 0.15);
    }
  });

  return {
    rebuild,
    // setLock shows a callout ({id, html}) beside a memory, or hides it (null).
    setLock(next) {
      lock = next;
      if (next) lockEl.innerHTML = next.html;
    },
    setHitClusters(ids) { hitClusters = new Set(ids); },
  };
}
