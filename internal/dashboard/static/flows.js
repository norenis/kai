// Particle flows and event effects (spec 5.4). Every flow shares one
// preallocated Points buffer; per-frame work is O(active particles).
//
// The scene owns per-chunk effect arrays (fx) and applies them to the soul:
//   boost[i]  extra brightness, decaying every frame
//   fade[i]   1 visible .. 0 gone (deleted notes)

import * as THREE from 'three';
import { pointMaterial, pointGeometry } from 'app/points.js';

const CAPACITY = 1400;
const RIBBON = { learn: 220, update: 140 };
const DURATION = { learn: 5.2, update: 3.0 }; // seconds for the head to arrive
const TAIL_DELAY = 0.15; // fraction of the flight the tail lags behind the head
const DELETE_SECONDS = 1.5;
const BOOST_DECAY_PER_FRAME = 0.975; // at 60 fps
const FLOW_SCALE = 300 / 430; // flow sprites relative to soul sprites (mock)

const ease = (x) => (x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2);

function bezier(f, u, out) {
  const a = 1 - u, a3 = a * a * a, b = 3 * a * a * u, c = 3 * a * u * u, d = u * u * u;
  out.x = a3 * f.p0.x + b * f.p1.x + c * f.p2.x + d * f.p3.x;
  out.y = a3 * f.p0.y + b * f.p1.y + c * f.p2.y + d * f.p3.y;
  out.z = a3 * f.p0.z + b * f.p1.z + c * f.p2.z + d * f.p3.z;
  return out;
}

export function createFlows({ scene3, getData, fx }) {
  const geo = pointGeometry(CAPACITY);
  const mat = pointMaterial();
  mat.uniforms.uTwinkle.value = 0;
  const points = new THREE.Points(geo, mat);
  points.frustumCulled = false;
  scene3.add(points);
  const pos = geo.attributes.position.array, col = geo.attributes.color.array;
  const size = geo.attributes.size.array, bright = geo.attributes.bright.array;
  const P = Array.from({ length: CAPACITY }, () => ({ on: false }));
  let active = 0;

  const ripples = Array.from({ length: 4 }, () => {
    const m = new THREE.Mesh(new THREE.RingGeometry(0.05, 0.056, 64), new THREE.MeshBasicMaterial({
      color: '#f3dfae', transparent: true, opacity: 0, side: THREE.DoubleSide, blending: THREE.AdditiveBlending, depthWrite: false,
    }));
    // Always in the scene (transparent when idle) so its shader is compiled
    // up front instead of stalling the frame of the first arrival.
    scene3.add(m);
    return { m, a: 0, s: 1 };
  });

  const deleting = []; // {i, t}

  // Recall fibres: while set, fine particles stream from these chunks to a
  // sink point (the top edge of the open sheet).
  let recall = null; // {indices, sink: () => THREE.Vector3}
  let recallBudget = 0;
  let adjacency = null, adjacencyFor = null;
  const tmp = new THREE.Vector3();

  function neighbours() {
    const data = getData();
    if (adjacencyFor !== data) {
      adjacency = Array.from({ length: data.n }, () => []);
      for (const [a, b] of data.links) { adjacency[a].push(b); adjacency[b].push(a); }
      adjacencyFor = data;
    }
    return adjacency;
  }

  const chunkPos = (i) => {
    const d = getData();
    return new THREE.Vector3(d.pos[i * 3], d.pos[i * 3 + 1], d.pos[i * 3 + 2]);
  };

  // glow raises brightness on chunks; it decays in the scene's frame loop.
  function glow(indices, amount) {
    for (const i of indices) fx.boost[i] = Math.max(fx.boost[i], amount);
  }

  // arrive: the target swells, a ripple ring spreads and linked memories
  // brighten in two hops.
  function arrive(target, noteChunks, withRipple) {
    if (target < 0) return;
    glow([target], 1.6);
    glow(noteChunks, withRipple ? 1.2 : 0.9);
    if (withRipple) {
      const adj = neighbours();
      for (const n1 of adj[target] || []) {
        glow([n1], 0.9);
        for (const n2 of adj[n1] || []) glow([n2], 0.35);
      }
      const r = ripples.find((x) => x.a <= 0.01) || ripples[0];
      r.m.position.copy(chunkPos(target));
      r.a = 1; r.s = 1;
    }
  }

  // ribbon spawns a curved stream from the outer sky into `to`.
  function ribbon(to, kind, onHead) {
    const n = RIBBON[kind];
    const radius = kind === 'learn' ? 4.6 : 3.2;
    const a = Math.random() * Math.PI * 2, y = (Math.random() - 0.4) * 1.6;
    const p0 = new THREE.Vector3(Math.cos(a) * radius, y, Math.sin(a) * radius);
    const tan = new THREE.Vector3(-Math.sin(a), 0, Math.cos(a)).multiplyScalar(2.2 * (Math.random() < 0.5 ? 1 : -1));
    const p1 = p0.clone().add(tan).multiplyScalar(0.8);
    const dir = to.lengthSq() > 1e-6 ? to.clone().normalize() : new THREE.Vector3(0, 1, 0);
    const p2 = dir.multiplyScalar(2.1).add(tan.clone().multiplyScalar(-0.5));
    p2.y = to.y + 0.5;
    const speed = 1 / DURATION[kind];
    let k = 0;
    for (let j = 0; j < CAPACITY && k < n; j++) {
      if (P[j].on) continue;
      const g = k / n;
      const color = kind === 'learn' ? [1, 0.93 - 0.1 * g, 0.78 - 0.25 * g] : [0.8 + 0.2 * (1 - g), 0.95, 1];
      Object.assign(P[j], {
        on: true, kind: 'ribbon', p0, p1, p2, p3: to, t: -g * TAIL_DELAY, speed,
        s: 0.1 * Math.pow(1 - g, 1.4) + 0.01, c: color, head: k === 0 ? onHead : null,
      });
      active++;
      k++;
    }
  }

  // spawn turns a brain event into its effect. Returns the target chunk
  // index (or -1) so callers can lock onto it.
  function spawn(ev, { reduced = false, onArrive = null } = {}) {
    const data = getData();
    const known = (ev.chunks || []).map((id) => data.index.get(id)).filter((i) => i !== undefined);
    const target = known.length ? known[0] : -1;
    switch (ev.type) {
      case 'learn':
      case 'create':
      case 'update': {
        const kind = ev.type === 'update' ? 'update' : 'learn';
        if (reduced) {
          arrive(target, known, false);
          onArrive?.();
          break;
        }
        // Not embedded yet: the stream flows into the core.
        const to = target >= 0 ? chunkPos(target) : new THREE.Vector3(0, 0, 0);
        ribbon(to, kind, () => { arrive(target, known, kind === 'learn'); onArrive?.(); });
        break;
      }
      case 'delete':
        for (const i of known) {
          if (reduced) fx.fade[i] = 0;
          else deleting.push({ i, t: 0 });
        }
        fx.dirty = true;
        onArrive?.();
        break;
      case 'touch':
        glow(known, 0.5);
        onArrive?.();
        break;
      default:
        break;
    }
    return target;
  }

  function spawnFibre(sink) {
    const j = P.findIndex((f) => !f.on);
    if (j < 0) return;
    const i = recall.indices[(Math.random() * recall.indices.length) | 0];
    const p0 = chunkPos(i);
    const p1 = p0.clone().add(p0.clone().normalize().multiplyScalar(0.5 + Math.random() * 0.4));
    const p3 = sink.clone();
    const p2 = p3.clone().add(new THREE.Vector3((Math.random() - 0.5) * 0.6, 0.5 + Math.random() * 0.4, 0));
    Object.assign(P[j], {
      on: true, kind: 'fibre', p0, p1, p2, p3, t: 0, speed: 0.21 + Math.random() * 0.21,
      s: 0.11 + Math.random() * 0.08, c: Math.random() < 0.7 ? [1, 1, 1] : [1, 0.92, 0.75], head: null,
    });
    active++;
  }

  function update(dt, camera) {
    if (recall && recall.indices.length) {
      // About one fibre per frame at 60 fps, independent of frame rate.
      recallBudget += dt * 55;
      const sink = recallBudget >= 1 ? recall.sink() : null;
      while (recallBudget >= 1 && sink) { spawnFibre(sink); recallBudget--; }
    }
    if (active > 0) {
      for (let j = 0; j < CAPACITY; j++) {
        const f = P[j];
        if (!f.on) { size[j] = 0; continue; }
        f.t += f.speed * dt;
        if (f.t < 0) { size[j] = 0; continue; }
        if (f.kind === 'fibre') {
          if (f.t >= 1 || !recall) { f.on = false; active--; size[j] = 0; continue; }
          bezier(f, f.t, tmp);
          pos[j * 3] = tmp.x; pos[j * 3 + 1] = tmp.y; pos[j * 3 + 2] = tmp.z;
          col[j * 3] = f.c[0]; col[j * 3 + 1] = f.c[1]; col[j * 3 + 2] = f.c[2];
          size[j] = f.s;
          bright[j] = Math.sin(f.t * Math.PI) * 0.9;
          continue;
        }
        bezier(f, ease(Math.min(1, f.t)), tmp);
        pos[j * 3] = tmp.x; pos[j * 3 + 1] = tmp.y; pos[j * 3 + 2] = tmp.z;
        col[j * 3] = f.c[0]; col[j * 3 + 1] = f.c[1]; col[j * 3 + 2] = f.c[2];
        size[j] = f.s * (f.t > 0.92 ? Math.max(0, (1 - f.t) / 0.08) : 1);
        bright[j] = 1;
        if (f.t >= 1) {
          f.on = false;
          active--;
          size[j] = 0;
          if (f.head) f.head();
        }
      }
      geo.attributes.position.needsUpdate = geo.attributes.size.needsUpdate = true;
      geo.attributes.color.needsUpdate = geo.attributes.bright.needsUpdate = true;
    } else if (size[0] !== 0 || size[CAPACITY - 1] !== 0) {
      size.fill(0);
      geo.attributes.size.needsUpdate = true;
    }

    for (const r of ripples) {
      if (r.a <= 0.01) { r.m.material.opacity = 0; continue; }
      r.s += 3 * dt;
      r.a *= Math.pow(0.965, dt * 60);
      r.m.scale.setScalar(r.s);
      r.m.lookAt(camera.position);
      r.m.material.opacity = r.a * 0.8;
    }

    for (let k = deleting.length - 1; k >= 0; k--) {
      const d = deleting[k];
      d.t += dt / DELETE_SECONDS;
      fx.fade[d.i] = Math.max(0, 1 - d.t);
      fx.dirty = true;
      if (d.t >= 1) deleting.splice(k, 1);
    }
  }

  return {
    spawn,
    update,
    decay: (dt) => Math.pow(BOOST_DECAY_PER_FRAME, dt * 60),
    setScale(s) { mat.uniforms.uScale.value = s * FLOW_SCALE; },
    // setRecall streams fibres from chunk indices to sink(); null stops them.
    setRecall(indices, sink) {
      recall = indices && indices.length ? { indices, sink } : null;
      recallBudget = 0;
    },
    get active() { return active; },
    reset() {
      for (const f of P) f.on = false;
      active = 0;
      deleting.length = 0;
      size.fill(0);
      geo.attributes.size.needsUpdate = true;
    },
  };
}
