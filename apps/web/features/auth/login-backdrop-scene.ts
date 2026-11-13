import type * as THREE from "three";

import {
  BLOOM_FRAGMENT,
  COMPOSITE_FRAGMENT,
  COMPOSITE_VERTEX,
  SKY_FRAGMENT,
  SKY_VERTEX,
  WATER_FRAGMENT,
  WATER_VERTEX,
} from "./login-backdrop-shaders";
import {
  buildContainerYard,
  buildCranes,
  buildLampMasts,
  buildMist,
  buildQuay,
  buildRain,
  buildShip,
} from "./login-backdrop-props";
import {
  createBeamTexture,
  createContainerTexture,
  createGlowTexture,
  createMistTexture,
  createNoiseTexture,
  createStreakTexture,
} from "./login-backdrop-textures";

/**
 * Dawn port scene for the sign-in backdrop, built to read as a film still:
 * a container terminal at first light. One deliberate palette — port ink,
 * signal amber, rust and steel — with wet quay reflections, volumetric lamp
 * beams, drifting mist, rain and a grain + vignette composite pass.
 *
 * The scene is fully procedural (no textures over the network): sky, water
 * and grain are GLSL, ground puddles and lamp glows are canvas sprites.
 */

type ThreeModule = typeof import("three");
type Scene = InstanceType<ThreeModule["Scene"]>;
type Camera = InstanceType<ThreeModule["PerspectiveCamera"]>;
type Mesh = InstanceType<ThreeModule["Mesh"]>;
type Texture = InstanceType<ThreeModule["Texture"]>;
type Vector3 = InstanceType<ThreeModule["Vector3"]>;

const FOG_COLOR = 0x36404b;
const FOG_DENSITY = 0.0105;

const SUN_ELEVATION = 0.055;
const SUN_AZIMUTH_X = -0.5;

const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

type Cleanup = () => void;

export function startPortScene(THREE: ThreeModule, canvas: HTMLCanvasElement): Cleanup {
  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: true,
    powerPreference: "high-performance",
  });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.75));
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.08;
  renderer.outputColorSpace = THREE.SRGBColorSpace;

  const scene = new THREE.Scene();
  scene.fog = new THREE.FogExp2(FOG_COLOR, FOG_DENSITY);

  const camera = new THREE.PerspectiveCamera(54, 1, 0.1, 900);

  let seed = 11;
  const random = () => {
    seed = (seed * 16807) % 2147483647;
    return seed / 2147483647;
  };

  // Sun sits low over the water at the end of the terminal lane; every
  // reflection and beam in the scene points back at it.
  const sunDir = new THREE.Vector3(SUN_AZIMUTH_X, SUN_ELEVATION, -1).normalize();
  const sunPosition = sunDir.clone().multiplyScalar(420);

  const sky = buildSky(THREE, sunDir);
  scene.add(sky.mesh);

  scene.add(new THREE.HemisphereLight(0x55677d, 0x241a12, 1.15));
  const dawn = new THREE.DirectionalLight(0xffa45c, 3.1);
  dawn.position.copy(sunPosition);
  scene.add(dawn);
  // Cool fill from behind the viewer keeps the yard readable against the sun
  // instead of collapsing into silhouettes.
  const skyFill = new THREE.DirectionalLight(0x5d7086, 0.85);
  skyFill.position.set(40, 50, 140);
  scene.add(skyFill);

  // Bake the sky into an environment map once: the wet quay, steel and
  // container paint then carry a real dawn reflection instead of flat shading.
  const pmrem = new THREE.PMREMGenerator(renderer);
  const envScene = new THREE.Scene();
  envScene.add(new THREE.Mesh(sky.mesh.geometry, sky.mesh.material));
  const envMap = pmrem.fromScene(envScene, 0.02, 0.1, 1000).texture;
  pmrem.dispose();
  scene.environment = envMap;
  scene.environmentIntensity = 0.55;

  const glowTexture = createGlowTexture(THREE);
  const streakTexture = createStreakTexture(THREE);
  const beamTexture = createBeamTexture(THREE);
  const containerTexture = createContainerTexture(THREE);
  const mistTexture = createMistTexture(THREE);
  const noiseTexture = createNoiseTexture(THREE);
  // Separate tile so the puddle pattern on the quay can repeat independently
  // of the water ripple.
  const groundRoughness = createNoiseTexture(THREE);
  groundRoughness.repeat.set(64, 22);

  const sunGlow = new THREE.Sprite(
    new THREE.SpriteMaterial({
      map: glowTexture,
      color: 0xffbf7a,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.9,
    }),
  );
  sunGlow.position.copy(sunDir).multiplyScalar(760);
  sunGlow.scale.set(240, 240, 1);
  scene.add(sunGlow);

  const water = buildWater(THREE, noiseTexture, sunDir);
  scene.add(water.mesh);

  const quay = buildQuay(THREE, groundRoughness);
  scene.add(quay);

  const containers = buildContainerYard(THREE, random, containerTexture);
  scene.add(containers);

  const cranes = buildCranes(THREE, glowTexture);
  scene.add(cranes.group);

  const ship = buildShip(THREE, glowTexture);
  scene.add(ship);

  const lamps = buildLampMasts(THREE, glowTexture, streakTexture, beamTexture);
  scene.add(lamps.group);

  const mist = buildMist(THREE, mistTexture, random);
  scene.add(mist.group);

  const rain = buildRain(THREE);
  scene.add(rain.mesh);

  // Long sun streak on the water: a warm smear pointing from the horizon
  // toward the camera that sells the reflection without a mirror pass.
  const sunPath = new THREE.Mesh(
    new THREE.PlaneGeometry(26, 200),
    new THREE.MeshBasicMaterial({
      map: streakTexture,
      color: 0xff9e4d,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.1,
    }),
  );
  sunPath.rotation.x = -Math.PI / 2;
  sunPath.position.set(sunDir.x * 260, 0.05, -120);
  scene.add(sunPath);

  const post = buildCompositePass(THREE, renderer, scene, camera);

  const reduceMotion = window.matchMedia?.(REDUCED_MOTION_QUERY).matches ?? false;
  const pointer = { x: 0, y: 0 };
  const onPointerMove = (event: PointerEvent) => {
    pointer.x = (event.clientX / window.innerWidth) * 2 - 1;
    pointer.y = (event.clientY / window.innerHeight) * 2 - 1;
  };
  window.addEventListener("pointermove", onPointerMove, { passive: true });

  const lookTarget = new THREE.Vector3();
  const placeCamera = (elapsed: number) => {
    const drift = Math.sin(elapsed * 0.028) * 3.2;
    camera.position.set(
      drift + pointer.x * 1.4,
      5.1 + Math.sin(elapsed * 0.05) * 0.22 - pointer.y * 0.5,
      34,
    );
    lookTarget.set(sunDir.x * 60 + pointer.x * 2.2, 4.6 + pointer.y * 1.1, -120);
    camera.lookAt(lookTarget);
  };

  const renderFrame = (elapsed: number, delta: number) => {
    placeCamera(elapsed);
    sky.update(elapsed);
    water.update(elapsed);
    lamps.update(elapsed);
    cranes.update(elapsed);
    mist.update(elapsed);
    rain.update(elapsed, delta);
    post.render(elapsed);
  };

  const resize = () => {
    const width = canvas.clientWidth || window.innerWidth;
    const height = canvas.clientHeight || window.innerHeight;
    renderer.setSize(width, height, false);
    camera.aspect = width / Math.max(height, 1);
    camera.updateProjectionMatrix();
    post.resize(width, height);
    if (reduceMotion) renderFrame(0, 0);
  };
  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);
  resize();

  let frame = 0;
  let elapsed = 0;
  let last = performance.now();
  const tick = (now: number) => {
    if (document.hidden) {
      last = now;
      frame = requestAnimationFrame(tick);
      return;
    }
    const delta = Math.min((now - last) / 1000, 0.05);
    last = now;
    elapsed += delta;
    renderFrame(elapsed, delta);
    frame = requestAnimationFrame(tick);
  };
  if (!reduceMotion) frame = requestAnimationFrame(tick);
  else renderFrame(0, 0);

  return () => {
    cancelAnimationFrame(frame);
    resizeObserver.disconnect();
    window.removeEventListener("pointermove", onPointerMove);
    post.dispose();
    scene.traverse((object) => {
      const mesh = object as Mesh;
      mesh.geometry?.dispose();
      const material = mesh.material;
      if (Array.isArray(material)) material.forEach((entry) => entry.dispose());
      else material?.dispose();
    });
    glowTexture.dispose();
    streakTexture.dispose();
    beamTexture.dispose();
    containerTexture.dispose();
    mistTexture.dispose();
    noiseTexture.dispose();
    groundRoughness.dispose();
    envMap.dispose();
    renderer.dispose();
  };
}

// ---------------------------------------------------------------------------
// Sky: a procedural dawn — ink zenith, steel mid, burning band at the horizon
// where the sun sits, and a ridged cloud deck that catches fire on its rim.
// ---------------------------------------------------------------------------

function buildSky(THREE: ThreeModule, sunDir: Vector3) {
  const uTime = { value: 0 };
  const material = new THREE.ShaderMaterial({
    side: THREE.BackSide,
    depthWrite: false,
    fog: false,
    uniforms: {
      uSunDir: { value: sunDir },
      uTime,
    },
    vertexShader: SKY_VERTEX,
    fragmentShader: SKY_FRAGMENT,
  });
  const mesh = new THREE.Mesh(new THREE.SphereGeometry(800, 32, 20), material);
  return {
    mesh,
    update(elapsed: number) {
      uTime.value = elapsed;
    },
  };
}

// ---------------------------------------------------------------------------
// Water: dark harbor water with ripple-distorted sky reflection, a warm
// streak under the sun and distance fade into the fog.
// ---------------------------------------------------------------------------

function buildWater(THREE: ThreeModule, noiseTexture: Texture, sunDir: Vector3) {
  const uTime = { value: 0 };
  const material = new THREE.ShaderMaterial({
    fog: false,
    uniforms: {
      uTime,
      uNoise: { value: noiseTexture },
      uSunDir: { value: sunDir },
      uDeep: { value: new THREE.Color(0x060a0e) },
      uSky: { value: new THREE.Color(0x27313d) },
      uWarm: { value: new THREE.Color(0xff9e4d) },
      uFog: { value: new THREE.Color(FOG_COLOR) },
    },
    vertexShader: WATER_VERTEX,
    fragmentShader: WATER_FRAGMENT,
  });
  const mesh = new THREE.Mesh(new THREE.PlaneGeometry(900, 520), material);
  mesh.rotation.x = -Math.PI / 2;
  mesh.position.set(0, -0.55, -160);
  return {
    mesh,
    update(elapsed: number) {
      uTime.value = elapsed;
    },
  };
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Composite pass: scene renders into a target, then a full-screen quad adds
// film grain and a vignette — the "film still" finish. Keeps the pass cheap:
// no blur chain, glow sprites carry the bloom.
// ---------------------------------------------------------------------------

function buildCompositePass(
  THREE: ThreeModule,
  renderer: InstanceType<ThreeModule["WebGLRenderer"]>,
  scene: Scene,
  camera: Camera,
) {
  const size = new THREE.Vector2(1, 1);
  const target = new THREE.WebGLRenderTarget(1, 1, {
    samples: 4,
    colorSpace: THREE.SRGBColorSpace,
  });
  const bloomTarget = new THREE.WebGLRenderTarget(1, 1);
  const uTime = { value: 0 };
  const material = new THREE.ShaderMaterial({
    depthTest: false,
    depthWrite: false,
    uniforms: {
      tScene: { value: target.texture },
      tBloom: { value: bloomTarget.texture },
      uTime,
      uRes: { value: size },
    },
    vertexShader: COMPOSITE_VERTEX,
    fragmentShader: COMPOSITE_FRAGMENT,
  });
  const bloomTexel = new THREE.Vector2(1, 1);
  const bloomMaterial = new THREE.ShaderMaterial({
    depthTest: false,
    depthWrite: false,
    uniforms: {
      tScene: { value: target.texture },
      uTexel: { value: bloomTexel },
      uThreshold: { value: 0.78 },
    },
    vertexShader: COMPOSITE_VERTEX,
    fragmentShader: BLOOM_FRAGMENT,
  });
  const quadScene = new THREE.Scene();
  const quadCamera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
  const quad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), material);
  quadScene.add(quad);
  const bloomQuadScene = new THREE.Scene();
  const bloomQuad = new THREE.Mesh(new THREE.PlaneGeometry(2, 2), bloomMaterial);
  bloomQuadScene.add(bloomQuad);

  return {
    resize(width: number, height: number) {
      size.set(width, height);
      const dpr = Math.min(window.devicePixelRatio || 1, 1.75);
      const fullWidth = Math.max(1, Math.floor(width * dpr));
      const fullHeight = Math.max(1, Math.floor(height * dpr));
      target.setSize(fullWidth, fullHeight);
      const bloomWidth = Math.max(1, Math.floor(fullWidth / 4));
      const bloomHeight = Math.max(1, Math.floor(fullHeight / 4));
      bloomTarget.setSize(bloomWidth, bloomHeight);
      bloomTexel.set(1 / bloomWidth, 1 / bloomHeight);
    },
    render(elapsed: number) {
      uTime.value = elapsed;
      renderer.setRenderTarget(target);
      renderer.render(scene, camera);
      renderer.setRenderTarget(bloomTarget);
      renderer.render(bloomQuadScene, quadCamera);
      renderer.setRenderTarget(null);
      renderer.render(quadScene, quadCamera);
    },
    dispose() {
      target.dispose();
      bloomTarget.dispose();
      quad.geometry.dispose();
      material.dispose();
      bloomMaterial.dispose();
    },
  };
}
