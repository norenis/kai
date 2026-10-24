// HTML overlays projected from the 3D scene each frame: constellation labels
// and the Roman numerals orbiting just outside the equator (spec 5.2).

import { ARMILLARY_RADIUS } from 'app/scene.js';

const SKY_LABELS = 3;
const NUMERAL_RADIUS = ARMILLARY_RADIUS + 0.2;

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
      return { c, el, shown: null };
    });
    const clusters = scene.constellations.map((c) => c.cluster);
    numerals = clusters.map((cl, i) => {
      const el = document.createElement('div');
      el.className = 'rn';
      el.innerHTML = `<span class="in">${esc(cl.numeral)}</span>`;
      layer.appendChild(el);
      return { el, angle: (i / clusters.length) * Math.PI * 2 };
    });
  }

  const place = (el, x, y, opacity) => {
    el.style.transform = `translate(${x.toFixed(1)}px, ${y.toFixed(1)}px)`;
    el.style.opacity = opacity;
  };

  scene.onFrame(({ view, project, ringAngle }) => {
    for (const l of labels) {
      const [x, y, z] = l.c.anchor;
      const p = project(x, y, z);
      const id = l.c.cluster.id;
      const visible = view === 'sky' ? id < SKY_LABELS : (view === 'search' || view === 'ask') ? hitClusters.has(id) : false;
      place(l.el, p.x, p.y, visible && !p.behind ? (p.facing > -0.35 ? 1 : 0.3) : 0);
    }
    const pos = lock && (view === 'sky' || view === 'note') ? scene.position(lock.id) : null;
    if (pos) {
      const p = project(...pos);
      // Right-aligned block left of and below the ring, as in the mock.
      place(lockEl, p.x - 280, p.y + 26, p.behind ? 0 : 1);
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
