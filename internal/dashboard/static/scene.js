// The persistent three.js scene: a golden armillary sphere around a
// holographic soul of real chunks. Screens only move the camera and change
// highlight state through the small API returned by createScene().

import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { EffectComposer } from 'three/addons/postprocessing/EffectComposer.js';
import { RenderPass } from 'three/addons/postprocessing/RenderPass.js';
import { UnrealBloomPass } from 'three/addons/postprocessing/UnrealBloomPass.js';
import { OutputPass } from 'three/addons/postprocessing/OutputPass.js';

// Camera per screen (spec 5.3). dim = how much non-highlighted memories fade.
export const VIEWS = {
  sky: { t: [0, -0.1, 0], d: 6.9, dim: 0 },
  search: { t: [0, -2.25, 0], d: 12.5, dim: 0.65 },
  ask: { t: [0, -1.85, 0], d: 12, dim: 0.65 },
  read: { t: [1.55, -0.1, 0], d: 7.2, dim: 0.35 },
  note: { t: [1.35, -0.15, 0], d: 7.6, dim: 0.5 },
  sys: { t: [0, -0.1, 0], d: 6.9, dim: 0 },
};

const R = 1.5; // armillary radius
const MAX_FILAMENTS = 1500;
const MAX_PIXEL_RATIO = 1.5;
// Adaptive quality: below this fps the pixel ratio steps down (never up).
const MIN_GOOD_FPS = 48;
const PIXEL_RATIO_STEP = 0.25;
// Point size scale per CSS pixel of viewport height (the approved mock used
// 430 on a 900 px tall stage).
const POINT_SCALE = 430 / 900;
const IDLE_RESUME_MS = 30_000;
const DAY_MS = 864e5;

const C = {
  gold: '#d8bc84',
  goldHi: '#e6c98e',
  champagne: '#f3dfae',
  soul: '#6fe3f5',
  soulHi: '#c8f6ff',
  filament: '#4fd6ee',
};

// ---------- data preparation (shared by WebGL and 2D fallback) ----------

// prepareVectors turns an /api/vectors payload into scene-space points:
// re-centred, then compressed radially into the sphere so direction (and so
// semantic neighbourhood) is preserved: r' = (1 - e^(-r/s)) * 1.22.
export function prepareVectors(payload, now = Date.now()) {
  const nodes = payload?.nodes || [];
  const n = nodes.length;
  const pos = new Float32Array(n * 3);
  const bright = new Float32Array(n);
  const fresh = new Uint8Array(n);
  const index = new Map();
  const docs = [];
  const docIndex = new Map();
  const docOf = new Int32Array(n);

  let cx = 0, cy = 0, cz = 0;
  for (const nd of nodes) { cx += nd.x; cy += nd.y; cz += nd.z; }
  if (n) { cx /= n; cy /= n; cz /= n; }
  const radii = nodes.map((nd) => Math.hypot(nd.x - cx, nd.y - cy, nd.z - cz));
  const sorted = [...radii].sort((a, b) => a - b);
  const s = sorted[Math.floor(n * 0.45)] || 1;

  nodes.forEach((nd, i) => {
    const r = radii[i] || 1;
    const r2 = (1 - Math.exp(-r / s)) * 1.22;
    pos[i * 3] = ((nd.x - cx) / r) * r2;
    pos[i * 3 + 1] = ((nd.y - cy) / r) * r2;
    pos[i * 3 + 2] = ((nd.z - cz) / r) * r2;

    const t = nd.updated ? Date.parse(nd.updated) : NaN;
    const age = Number.isFinite(t) ? Math.max(0, (now - t) / DAY_MS) : Infinity;
    bright[i] = age <= 30 ? Math.max(0.72, 1 - age / 25) : 0.42;
    fresh[i] = age <= 7 ? 1 : 0;

    index.set(nd.id, i);
    if (!docIndex.has(nd.doc)) { docIndex.set(nd.doc, docs.length); docs.push(nd.doc); }
    docOf[i] = docIndex.get(nd.doc);
  });

  const links = [];
  for (const l of payload?.links || []) {
    const a = index.get(l.source), b = index.get(l.target);
    if (a !== undefined && b !== undefined) links.push([a, b, l.value || 0]);
  }
  links.sort((x, y) => y[2] - x[2]);

  return { n, pos, bright, fresh, index, docs, docOf, links, nodes };
}

// ---------- WebGL scene ----------

const VS = `attribute float size;attribute float bright;attribute float phase;attribute vec3 color;
uniform float uScale;uniform float uTime;uniform float uTwinkle;varying vec3 vC;varying float vA;
void main(){vC=color;vec4 mv=modelViewMatrix*vec4(position,1.);gl_PointSize=size*uScale/(-mv.z);
gl_Position=projectionMatrix*mv;vA=bright*(1.-.28*uTwinkle+.28*uTwinkle*sin(uTime*1.6+phase));}`;
const FS = `varying vec3 vC;varying float vA;void main(){float d=length(gl_PointCoord-.5);
float a=smoothstep(.5,0.,d);a=pow(a,1.7);if(a<.01)discard;gl_FragColor=vec4(vC,a*vA);}`;

function pointMaterial() {
  return new THREE.ShaderMaterial({
    uniforms: { uScale: { value: 200 }, uTime: { value: 0 }, uTwinkle: { value: 1 } },
    vertexShader: VS, fragmentShader: FS,
    transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
  });
}

function pointGeometry(n) {
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.BufferAttribute(new Float32Array(n * 3), 3));
  g.setAttribute('color', new THREE.BufferAttribute(new Float32Array(n * 3), 3));
  g.setAttribute('size', new THREE.BufferAttribute(new Float32Array(n), 1));
  g.setAttribute('bright', new THREE.BufferAttribute(new Float32Array(n), 1));
  const ph = new Float32Array(n);
  for (let i = 0; i < n; i++) ph[i] = Math.random() * Math.PI * 2;
  g.setAttribute('phase', new THREE.BufferAttribute(ph, 1));
  return g;
}

const lineMat = (c, o) => new THREE.LineBasicMaterial({ color: c, transparent: true, opacity: o, depthWrite: false });

function ring(r, c, o, seg = 256) {
  const p = [];
  for (let i = 0; i <= seg; i++) { const a = (i / seg) * Math.PI * 2; p.push(new THREE.Vector3(Math.cos(a) * r, 0, Math.sin(a) * r)); }
  return new THREE.Line(new THREE.BufferGeometry().setFromPoints(p), lineMat(c, o));
}

function band(r, w, c, o) {
  const m = new THREE.Mesh(new THREE.TorusGeometry(r, w, 6, 256), new THREE.MeshBasicMaterial({ color: c, transparent: true, opacity: o }));
  m.rotation.x = Math.PI / 2;
  return m;
}

function ticks(r, n, len, c, o, major = 10) {
  const p = [];
  for (let i = 0; i < n; i++) {
    const a = (i / n) * Math.PI * 2, l = i % major === 0 ? len * 2.2 : len;
    p.push(Math.cos(a) * r, 0, Math.sin(a) * r, Math.cos(a) * (r - l), 0, Math.sin(a) * (r - l));
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(p, 3));
  return new THREE.LineSegments(g, lineMat(c, o));
}

function backgroundTexture() {
  const c = document.createElement('canvas');
  c.width = 1024; c.height = 640;
  const x = c.getContext('2d');
  const g = x.createRadialGradient(512, 320, 0, 512, 320, 640);
  g.addColorStop(0, '#11233b'); g.addColorStop(0.4, '#0a1526'); g.addColorStop(1, '#03060b');
  x.fillStyle = g; x.fillRect(0, 0, 1024, 640);
  const g2 = x.createRadialGradient(512, 350, 0, 512, 350, 260);
  g2.addColorStop(0, 'rgba(40,140,170,.2)'); g2.addColorStop(1, 'rgba(0,0,0,0)');
  x.fillStyle = g2; x.fillRect(0, 0, 1024, 640);
  for (let i = 0; i < 700; i++) {
    x.fillStyle = `rgba(230,225,210,${Math.random() * 0.5})`;
    const s = Math.random() < 0.1 ? 1.5 : 0.8;
    x.fillRect(Math.random() * 1024, Math.random() * 640, s, s);
  }
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  return t;
}

function buildArmillary(scene) {
  const arm = new THREE.Group();
  scene.add(arm);
  const eq = new THREE.Group();
  eq.add(band(R, 0.0032, C.gold, 0.85));
  eq.add(ticks(R, 360, 0.025, C.gold, 0.5));
  eq.add(ticks(R, 72, 0.06, C.champagne, 0.85, 6));
  arm.add(eq);
  const mer = new THREE.Group();
  mer.add(band(R + 0.02, 0.0028, C.gold, 0.7));
  mer.add(ticks(R + 0.02, 180, 0.03, C.gold, 0.45));
  mer.rotation.z = Math.PI / 2;
  arm.add(mer);
  const col = new THREE.Group();
  col.add(band(R + 0.04, 0.002, C.gold, 0.4));
  col.rotation.z = Math.PI / 2; col.rotation.y = Math.PI / 2;
  arm.add(col);
  const ecl = new THREE.Group();
  ecl.add(band(R + 0.06, 0.006, C.goldHi, 0.5));
  ecl.add(ticks(R + 0.06, 120, 0.03, C.champagne, 0.6));
  ecl.rotation.x = (23.4 * Math.PI) / 180;
  arm.add(ecl);
  arm.add(new THREE.Line(
    new THREE.BufferGeometry().setFromPoints([new THREE.Vector3(0, -R - 0.35, 0), new THREE.Vector3(0, R + 0.35, 0)]),
    lineMat(C.gold, 0.5)));
  for (const s of [1, -1]) {
    const m = new THREE.Mesh(new THREE.SphereGeometry(0.025, 16, 16), new THREE.MeshBasicMaterial({ color: C.champagne }));
    m.position.y = s * (R + 0.35);
    arm.add(m);
  }
  return { eq, ecl, col };
}

function buildShell(scene, material) {
  const n = 1600;
  const g = pointGeometry(n);
  const pos = g.attributes.position.array, col = g.attributes.color.array;
  for (let i = 0; i < n; i++) {
    const y = 1 - (i / (n - 1)) * 2, rr = Math.sqrt(1 - y * y), t = i * 2.39996;
    pos.set([Math.cos(t) * rr * R * 0.98, y * R * 0.98, Math.sin(t) * rr * R * 0.98], i * 3);
    col.set([0.85, 0.75, 0.55], i * 3);
    g.attributes.size.array[i] = 0.014;
    g.attributes.bright.array[i] = 0.4;
  }
  scene.add(new THREE.Points(g, material));
}

function buildOrbits(scene) {
  const orbits = [];
  for (let i = 0; i < 3; i++) {
    const g = new THREE.Group();
    const rr = 1.85 + i * 0.2;
    g.add(ring(rr, C.gold, 0.22));
    g.rotation.set(0.5 + i * 0.35, i * 1.1, 0.15 * i);
    scene.add(g);
    const pl = new THREE.Mesh(new THREE.SphereGeometry(0.012 + 0.007 * (3 - i), 20, 20),
      new THREE.MeshBasicMaterial({ color: i === 0 ? '#bff4ff' : C.champagne }));
    g.add(pl);
    orbits.push({ pl, rr, sp: 0.18 - 0.04 * i, o: i * 2 });
  }
  return orbits;
}

function buildBase(scene) {
  const base = new THREE.Group();
  base.position.y = -1.95;
  base.scale.setScalar(0.82);
  scene.add(base);
  [1.2, 1.55, 2.1, 2.15].forEach((rr, i) => base.add(ring(rr, C.gold, [0.25, 0.35, 0.55, 0.3][i])));
  base.add(ticks(2.1, 360, 0.05, C.gold, 0.35));
  base.add(ticks(2.1, 36, 0.12, C.champagne, 0.6, 1));
  const beam = new THREE.Mesh(
    new THREE.CylinderGeometry(0.6, 1.2, 1.9, 64, 1, true),
    new THREE.ShaderMaterial({
      transparent: true, depthWrite: false, blending: THREE.AdditiveBlending, side: THREE.DoubleSide,
      vertexShader: 'varying float vY;void main(){vY=uv.y;gl_Position=projectionMatrix*modelViewMatrix*vec4(position,1.);}',
      fragmentShader: 'varying float vY;void main(){gl_FragColor=vec4(.35,.8,.95,(1.-vY)*(1.-vY)*.03);}',
    }));
  beam.position.y = -1.0;
  scene.add(beam);
}

function webglAvailable() {
  try {
    const c = document.createElement('canvas');
    return !!(window.WebGL2RenderingContext && c.getContext('webgl2'));
  } catch {
    return false;
  }
}

// createScene mounts the scene into host and returns the scene API. When
// WebGL is unavailable it falls back to a static 2D render of the same points.
export function createScene(host, opts = {}) {
  if (opts.forceFallback || !webglAvailable()) return createFallback(host);
  try {
    return createWebGLScene(host);
  } catch (err) {
    console.warn('WebGL scene failed, using 2D fallback', err);
    host.replaceChildren();
    return createFallback(host);
  }
}

function createWebGLScene(host) {
  const renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' });
  let pixelRatio = Math.min(MAX_PIXEL_RATIO, window.devicePixelRatio || 1);
  renderer.setPixelRatio(pixelRatio);
  host.appendChild(renderer.domElement);

  const scene = new THREE.Scene();
  scene.background = backgroundTexture();
  const cam = new THREE.PerspectiveCamera(38, 1, 0.05, 100);
  cam.position.set(0, 1.05, 6.9);

  const ctl = new OrbitControls(cam, renderer.domElement);
  ctl.enableDamping = true;
  ctl.enableZoom = false;
  ctl.enablePan = false;
  ctl.autoRotateSpeed = 0.32;
  ctl.target.set(...VIEWS.sky.t);

  const composer = new EffectComposer(renderer);
  composer.setPixelRatio(pixelRatio);
  composer.addPass(new RenderPass(scene, cam));
  const bloom = new UnrealBloomPass(new THREE.Vector2(256, 256), 0.7, 0.45, 0.18);
  composer.addPass(bloom);
  composer.addPass(new OutputPass());

  const soulMat = pointMaterial();
  const shellMat = pointMaterial();
  const ringsAnim = buildArmillary(scene);
  buildShell(scene, shellMat);
  const orbits = buildOrbits(scene);
  buildBase(scene);

  const COL = { champagne: new THREE.Color(C.champagne), soul: new THREE.Color(C.soul), soulHi: new THREE.Color(C.soulHi) };

  // Soul state, rebuilt by setVectors.
  let data = prepareVectors(null);
  let soul = null, filaments = null;
  let baseCol = new Float32Array(0), baseSize = new Float32Array(0);
  let highlight = new Set(), highlightColor = new THREE.Color('#ffffff');
  let view = 'sky', dimOverride = null;

  function setVectors(payload) {
    data = prepareVectors(payload);
    if (soul) { scene.remove(soul); soul.geometry.dispose(); }
    if (filaments) { scene.remove(filaments); filaments.geometry.dispose(); filaments.material.dispose(); }

    const n = data.n;
    const g = pointGeometry(n);
    g.attributes.position.array.set(data.pos);
    baseCol = new Float32Array(n * 3);
    baseSize = new Float32Array(n);
    // The look was tuned on a ~3k chunk brain; small brains get larger points
    // so the soul does not read as empty.
    const density = Math.min(2.2, Math.max(1, Math.cbrt(3000 / Math.max(1, n))));
    for (let i = 0; i < n; i++) {
      const c = data.fresh[i] ? COL.champagne : (Math.random() < 0.35 ? COL.soulHi : COL.soul);
      baseCol[i * 3] = c.r; baseCol[i * 3 + 1] = c.g; baseCol[i * 3 + 2] = c.b;
      baseSize[i] = (0.03 + data.bright[i] * 0.045) * density;
    }
    soul = new THREE.Points(g, soulMat);
    scene.add(soul);

    const fp = [];
    for (const [a, b] of data.links.slice(0, MAX_FILAMENTS)) {
      fp.push(data.pos[a * 3], data.pos[a * 3 + 1], data.pos[a * 3 + 2], data.pos[b * 3], data.pos[b * 3 + 1], data.pos[b * 3 + 2]);
    }
    const fg = new THREE.BufferGeometry();
    fg.setAttribute('position', new THREE.Float32BufferAttribute(fp, 3));
    filaments = new THREE.LineSegments(fg, new THREE.LineBasicMaterial({
      color: C.filament, transparent: true, opacity: 0.06, blending: THREE.AdditiveBlending, depthWrite: false,
    }));
    scene.add(filaments);
    applyHighlight();
  }

  function currentDim() {
    return dimOverride ?? VIEWS[view].dim;
  }

  function applyHighlight() {
    if (!soul) return;
    const g = soul.geometry;
    const ca = g.attributes.color.array, sa = g.attributes.size.array, ba = g.attributes.bright.array;
    const dim = highlight.size ? currentDim() : 0;
    for (let i = 0; i < data.n; i++) {
      if (highlight.has(i)) {
        ca[i * 3] = highlightColor.r; ca[i * 3 + 1] = highlightColor.g; ca[i * 3 + 2] = highlightColor.b;
        sa[i] = baseSize[i] * 2.1;
        ba[i] = 1.6;
      } else {
        ca[i * 3] = baseCol[i * 3]; ca[i * 3 + 1] = baseCol[i * 3 + 1]; ca[i * 3 + 2] = baseCol[i * 3 + 2];
        sa[i] = baseSize[i];
        ba[i] = data.bright[i] * (1 - dim);
      }
    }
    g.attributes.color.needsUpdate = g.attributes.size.needsUpdate = g.attributes.bright.needsUpdate = true;
    if (filaments) filaments.material.opacity = 0.06 * (1 - dim * 0.7);
  }

  // ---------- sizing ----------
  function resize() {
    const w = host.clientWidth || window.innerWidth, h = host.clientHeight || window.innerHeight;
    renderer.setSize(w, h, false);
    composer.setSize(w, h);
    cam.aspect = w / h;
    cam.updateProjectionMatrix();
    const scale = POINT_SCALE * h * pixelRatio;
    soulMat.uniforms.uScale.value = scale;
    shellMat.uniforms.uScale.value = scale;
  }
  const ro = new ResizeObserver(resize);
  ro.observe(host);
  resize();

  // ---------- motion preferences and idle auto-rotation ----------
  const motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)');
  let reducedMotion = motionQuery.matches;
  let lastInteraction = -Infinity;
  const onMotionChange = (e) => { reducedMotion = e.matches; };
  motionQuery.addEventListener('change', onMotionChange);
  ctl.addEventListener('start', () => { lastInteraction = performance.now(); ctl.autoRotate = false; });

  // ---------- render loop ----------
  const tgt = new THREE.Vector3();
  const off = new THREE.Vector3();
  let dist = VIEWS.sky.d;
  let t = 0, last = performance.now(), raf = 0, running = false;
  let frames = 0, fpsWindowStart = last, fps = 0;
  const listeners = new Set();
  let sampleRequest = null;

  function frame(now) {
    raf = requestAnimationFrame(frame);
    const dt = Math.min(0.1, (now - last) / 1000);
    last = now;
    t += dt;

    frames++;
    if (now - fpsWindowStart >= 1000) {
      fps = (frames * 1000) / (now - fpsWindowStart);
      frames = 0; fpsWindowStart = now;
      adaptQuality(now);
    }

    soulMat.uniforms.uTime.value = t;
    shellMat.uniforms.uTime.value = t;
    if (!reducedMotion) {
      ringsAnim.eq.rotation.y = t * 0.04;
      ringsAnim.ecl.rotation.y = t * 0.06;
      ringsAnim.col.rotation.x = t * 0.02;
      for (const o of orbits) { const a = t * o.sp + o.o; o.pl.position.set(Math.cos(a) * o.rr, 0, Math.sin(a) * o.rr); }
    }
    ctl.autoRotate = !reducedMotion && now - lastInteraction > IDLE_RESUME_MS;

    // Camera glides to the current view (frame-rate independent ease).
    const v = VIEWS[view];
    const k = 1 - Math.pow(0.95, dt * 60);
    tgt.set(...v.t);
    ctl.target.lerp(tgt, k);
    dist += (v.d - dist) * k;
    off.copy(cam.position).sub(ctl.target).setLength(dist);
    cam.position.copy(ctl.target).add(off);

    ctl.update(dt);
    composer.render(dt);
    if (sampleRequest) { sampleRequest(sampleCanvas(renderer)); sampleRequest = null; }
    for (const fn of listeners) fn({ t, dt, camera: cam, target: ctl.target });
  }

  // Steps the pixel ratio down while the frame rate stays low, after a
  // warm-up and with a cooldown so one slow second does not cost quality.
  let qualityCheckAfter = performance.now() + 3000;
  function adaptQuality(now) {
    if (now < qualityCheckAfter || fps >= MIN_GOOD_FPS || pixelRatio <= 1) return;
    pixelRatio = Math.max(1, pixelRatio - PIXEL_RATIO_STEP);
    renderer.setPixelRatio(pixelRatio);
    composer.setPixelRatio(pixelRatio);
    resize();
    qualityCheckAfter = now + 3000;
  }

  function start() {
    if (running) return;
    running = true;
    last = performance.now();
    fpsWindowStart = last; frames = 0;
    qualityCheckAfter = Math.max(qualityCheckAfter, last + 3000);
    raf = requestAnimationFrame(frame);
  }
  function stop() {
    running = false;
    cancelAnimationFrame(raf);
  }
  const onVisibility = () => (document.hidden ? stop() : start());
  document.addEventListener('visibilitychange', onVisibility);
  if (!document.hidden) start();

  return {
    kind: 'webgl',
    setVectors,
    setView(name) {
      if (!VIEWS[name]) return;
      view = name;
      applyHighlight();
    },
    // highlight(chunkIds, {color, dim}) lights the given chunks and dims the rest.
    highlight(ids = [], { color = '#ffffff', dim = null } = {}) {
      highlight = new Set();
      for (const id of ids) { const i = data.index.get(id); if (i !== undefined) highlight.add(i); }
      highlightColor = new THREE.Color(color);
      dimOverride = dim;
      applyHighlight();
    },
    // onFrame registers a per-frame callback (overlays, readouts); returns an unsubscribe function.
    onFrame(fn) { listeners.add(fn); return () => listeners.delete(fn); },
    get running() { return running; },
    get fps() { return fps; },
    get pixelRatio() { return pixelRatio; },
    get reducedMotion() { return reducedMotion; },
    get autoRotating() { return ctl.autoRotate; },
    get count() { return data.n; },
    get distance() { return cam.position.distanceTo(ctl.target); },
    get azimuth() { return ctl.getAzimuthalAngle(); },
    // sample resolves with brightness stats of the next rendered frame (tests).
    sample() { return new Promise((resolve) => { sampleRequest = resolve; if (!running) resolve(null); }); },
    dispose() {
      stop();
      ro.disconnect();
      motionQuery.removeEventListener('change', onMotionChange);
      document.removeEventListener('visibilitychange', onVisibility);
      ctl.dispose();
      renderer.dispose();
      host.replaceChildren();
    },
  };
}

function sampleCanvas(renderer) {
  const gl = renderer.getContext();
  const w = gl.drawingBufferWidth, h = gl.drawingBufferHeight;
  const px = new Uint8Array(w * h * 4);
  gl.readPixels(0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, px);
  return brightnessStats(px);
}

function brightnessStats(px) {
  let lit = 0, max = 0;
  const total = px.length / 4;
  for (let i = 0; i < px.length; i += 4) {
    const l = 0.2126 * px[i] + 0.7152 * px[i + 1] + 0.0722 * px[i + 2];
    if (l > 120) lit++;
    if (l > max) max = l;
  }
  return { lit: lit / total, max };
}

// ---------- 2D fallback (no WebGL): static render of the same points ----------

function createFallback(host) {
  const canvas = document.createElement('canvas');
  canvas.className = 'fallback';
  host.appendChild(canvas);
  let data = prepareVectors(null);

  function draw() {
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    const w = host.clientWidth || window.innerWidth, h = host.clientHeight || window.innerHeight;
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    const x = canvas.getContext('2d');
    x.setTransform(dpr, 0, 0, dpr, 0, 0);

    const bg = x.createRadialGradient(w / 2, h / 2, 0, w / 2, h / 2, Math.max(w, h) * 0.65);
    bg.addColorStop(0, '#11233b'); bg.addColorStop(0.4, '#0a1526'); bg.addColorStop(1, '#03060b');
    x.fillStyle = bg;
    x.fillRect(0, 0, w, h);

    // Fixed oblique view: yaw then pitch, simple perspective.
    const scale = h / 4.6, cx = w / 2, cy = h / 2 + h * 0.02;
    const yaw = 0.6, pitch = 0.15, camD = 6.9;
    const project = (px, py, pz) => {
      const x1 = px * Math.cos(yaw) - pz * Math.sin(yaw);
      const z1 = px * Math.sin(yaw) + pz * Math.cos(yaw);
      const y1 = py * Math.cos(pitch) - z1 * Math.sin(pitch);
      const z2 = py * Math.sin(pitch) + z1 * Math.cos(pitch);
      const f = camD / (camD - z2);
      return [cx + x1 * scale * f, cy - y1 * scale * f, f];
    };

    // Armillary outline: sphere edge, equator, tilted ecliptic and polar axis.
    const rr = R * scale;
    x.lineWidth = 1;
    x.strokeStyle = 'rgba(216,188,132,.35)';
    x.beginPath(); x.arc(cx, cy, rr, 0, Math.PI * 2); x.stroke();
    x.strokeStyle = 'rgba(216,188,132,.7)';
    x.beginPath(); x.ellipse(cx, cy, rr, rr * Math.sin(pitch), 0, 0, Math.PI * 2); x.stroke();
    x.strokeStyle = 'rgba(230,201,142,.45)';
    x.beginPath(); x.ellipse(cx, cy, rr * 1.04, rr * 0.36, (-23.4 * Math.PI) / 180, 0, Math.PI * 2); x.stroke();
    x.strokeStyle = 'rgba(216,188,132,.5)';
    x.beginPath(); x.moveTo(cx, cy - rr - 0.35 * scale); x.lineTo(cx, cy + rr + 0.35 * scale); x.stroke();

    x.globalCompositeOperation = 'lighter';
    for (let i = 0; i < data.n; i++) {
      const [sx, sy, f] = project(data.pos[i * 3], data.pos[i * 3 + 1], data.pos[i * 3 + 2]);
      const b = data.bright[i];
      x.fillStyle = data.fresh[i] ? `rgba(243,223,174,${b})` : `rgba(111,227,245,${b * 0.9})`;
      x.beginPath();
      x.arc(sx, sy, (1 + b * 1.6) * f, 0, Math.PI * 2);
      x.fill();
    }
    x.globalCompositeOperation = 'source-over';
  }

  const ro = new ResizeObserver(draw);
  ro.observe(host);
  draw();
  const listeners = new Set();

  return {
    kind: 'fallback',
    setVectors(payload) { data = prepareVectors(payload); draw(); },
    setView() {},
    highlight() {},
    onFrame(fn) { listeners.add(fn); return () => listeners.delete(fn); },
    get running() { return false; },
    get fps() { return 0; },
    get reducedMotion() { return true; },
    get autoRotating() { return false; },
    get count() { return data.n; },
    get distance() { return VIEWS.sky.d; },
    get azimuth() { return 0; },
    sample() {
      const x = canvas.getContext('2d');
      return Promise.resolve(brightnessStats(x.getImageData(0, 0, canvas.width, canvas.height).data));
    },
    dispose() { ro.disconnect(); host.replaceChildren(); },
  };
}
