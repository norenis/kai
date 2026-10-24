// Shared point sprite shader: round soft sprites, additive, with per-point
// size, brightness and twinkle phase.

import * as THREE from 'three';

const VS = `attribute float size;attribute float bright;attribute float phase;attribute vec3 color;
uniform float uScale;uniform float uTime;uniform float uTwinkle;varying vec3 vC;varying float vA;
void main(){vC=color;vec4 mv=modelViewMatrix*vec4(position,1.);gl_PointSize=size*uScale/(-mv.z);
gl_Position=projectionMatrix*mv;vA=bright*(1.-.28*uTwinkle+.28*uTwinkle*sin(uTime*1.6+phase));}`;
const FS = `varying vec3 vC;varying float vA;void main(){float d=length(gl_PointCoord-.5);
float a=smoothstep(.5,0.,d);a=pow(a,1.7);if(a<.01)discard;gl_FragColor=vec4(vC,a*vA);}`;

export function pointMaterial() {
  return new THREE.ShaderMaterial({
    uniforms: { uScale: { value: 200 }, uTime: { value: 0 }, uTwinkle: { value: 1 } },
    vertexShader: VS, fragmentShader: FS,
    transparent: true, depthWrite: false, blending: THREE.AdditiveBlending,
  });
}

export function pointGeometry(n) {
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

