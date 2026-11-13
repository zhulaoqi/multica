"use client";

import { useEffect, useRef } from "react";

/**
 * WebGL backdrop for the sign-in screens: a low-poly autumn landscape —
 * layered misty mountains at dawn, a still lake, a lakeside pavilion and a
 * lantern boat, with leaves drifting across the frame. three.js is imported
 * lazily so the login bundle stays small and nothing runs during SSR; if
 * WebGL is not available the poster image behind the canvas stays visible.
 */
export function LoginBackdrop() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    let disposed = false;
    let cleanup: (() => void) | undefined;

    void (async () => {
      try {
        const THREE = await import("three");
        if (disposed) return;
        cleanup = startAutumnLandscapeScene(THREE, canvas);
      } catch {
        // Keep the static poster: no WebGL, no extra network for the chunk.
      }
    })();

    return () => {
      disposed = true;
      cleanup?.();
    };
  }, []);

  return (
    <div className="absolute inset-0 overflow-hidden" data-testid="login-backdrop">
      <canvas ref={canvasRef} aria-hidden="true" className="block h-full w-full" />
    </div>
  );
}

type ThreeModule = typeof import("three");
type Mesh = InstanceType<ThreeModule["Mesh"]>;
type Texture = InstanceType<ThreeModule["Texture"]>;

const MIST = 0xc4c1b7;
const SUN = 0xffe3b8;
const LEAF_COLORS = [0xc7531f, 0xe0912f, 0xa8401d, 0xd7a63f, 0x8f3f1c, 0xb2692a];
const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const LEAF_COUNT = 320;
const FLOATING_LEAF_COUNT = 160;

type LeafState = {
  x: number;
  y: number;
  z: number;
  speed: number;
  sway: number;
  phase: number;
  spin: number;
  drift: number;
};

type MistState = {
  x: number;
  baseY: number;
  z: number;
  phase: number;
  amplitude: number;
};

function startAutumnLandscapeScene(THREE: ThreeModule, canvas: HTMLCanvasElement): () => void {
  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: true,
    powerPreference: "high-performance",
  });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.22;

  const scene = new THREE.Scene();
  scene.fog = new THREE.FogExp2(MIST, 0.0066);

  const camera = new THREE.PerspectiveCamera(50, 1, 0.1, 600);

  let seed = 37;
  const random = () => {
    seed = (seed * 16807) % 2147483647;
    return seed / 2147483647;
  };

  const glowTexture = createGlowTexture(THREE);
  const mistTexture = createMistTexture(THREE);
  const waterTexture = createWaterTexture(THREE);
  scene.add(createSkyDome(THREE));

  scene.add(new THREE.HemisphereLight(0xdde5ee, 0x64482c, 1.3));
  const dawn = new THREE.DirectionalLight(0xffc98f, 2.1);
  dawn.position.set(-26, 9, -120);
  const cool = new THREE.DirectionalLight(0xa9c2d6, 0.4);
  cool.position.set(18, 16, 20);
  scene.add(dawn, cool);

  // Dawn glow rising behind the furthest range. A sprite keeps it facing the
  // camera, and its halo spills over the ridge line as the local sun.
  const sunGlow = addGlow(THREE, glowTexture, [-22, 26, -168], 50, 0.8);
  scene.add(sunGlow);

  // Still lake. The emissive streak texture drifts sideways so the surface
  // reads as water without rebuilding geometry every frame.
  const water = new THREE.Mesh(
    new THREE.PlaneGeometry(520, 420),
    new THREE.MeshStandardMaterial({
      color: 0x3c4f59,
      roughness: 0.3,
      metalness: 0.16,
      emissive: 0x8fb7c4,
      emissiveMap: waterTexture,
      emissiveIntensity: 0.6,
    }),
  );
  water.rotation.x = -Math.PI / 2;
  water.position.set(0, 0, -140);
  scene.add(water);

  scene.add(addSunPath(THREE, glowTexture));

  // Four misty ranges, far to near, cooling off as they recede.
  addMountainRange(THREE, scene, random, {
    color: 0xa6a7a1,
    z: -150,
    count: 9,
    spread: 300,
    minHeight: 26,
    maxHeight: 54,
  });
  addMountainRange(THREE, scene, random, {
    color: 0x93958c,
    z: -112,
    count: 8,
    spread: 240,
    minHeight: 20,
    maxHeight: 40,
  });
  addMountainRange(THREE, scene, random, {
    color: 0x77816f,
    z: -74,
    count: 7,
    spread: 190,
    minHeight: 12,
    maxHeight: 26,
  });
  const nearHills = addMountainRange(THREE, scene, random, {
    color: 0x5a6450,
    z: -44,
    count: 5,
    spread: 150,
    minHeight: 6,
    maxHeight: 14,
  });
  nearHills.traverse((object) => {
    object.position.y -= 1.5;
  });

  // Mist banks between the ranges and over the water.
  const mistBanks: Array<{ state: MistState; sprite: InstanceType<ThreeModule["Sprite"]> }> = [];
  for (let i = 0; i < 26; i += 1) {
    const state: MistState = {
      x: (random() - 0.5) * 220,
      baseY: 2 + random() * 6,
      z: -12 - random() * 110,
      phase: random() * Math.PI * 2,
      amplitude: 4 + random() * 10,
    };
    const sprite = new THREE.Sprite(
      new THREE.SpriteMaterial({
        map: mistTexture,
        transparent: true,
        depthWrite: false,
        opacity: 0.1 + random() * 0.12,
      }),
    );
    const size = 34 + random() * 54;
    sprite.scale.set(size, size * 0.42, 1);
    sprite.position.set(state.x, state.baseY, state.z);
    scene.add(sprite);
    mistBanks.push({ state, sprite });
  }

  const trunkMaterial = new THREE.MeshStandardMaterial({ color: 0x3a2a20, roughness: 0.95 });
  const foliageMaterials = LEAF_COLORS.map(
    (color) => new THREE.MeshStandardMaterial({ color, roughness: 0.9, flatShading: true }),
  );
  const rockMaterial = new THREE.MeshStandardMaterial({ color: 0x323a3b, roughness: 0.95, flatShading: true });
  const rockGeometry = new THREE.IcosahedronGeometry(1, 0);

  // Autumn trees frame the shot on both banks, nearest the camera.
  const trees = new THREE.Group();
  for (const side of [-1, 1] as const) {
    for (let i = 0; i < 5; i += 1) {
      const z = 11.5 - i * 4.2 - random() * 1.6;
      // Keep each trunk just inside the horizontal field of view so the
      // banks read as framing trees rather than falling outside the shot.
      const x = side * ((24 - z) * 0.45 + 2.2 + random() * 1.4);
      const scale = 0.95 + random() * 0.6;
      const trunk = new THREE.Mesh(new THREE.CylinderGeometry(0.16, 0.28, 3.4 * scale, 6), trunkMaterial);
      trunk.position.set(x, 1.7 * scale, z);
      trees.add(trunk);
      for (let cluster = 0; cluster < 3; cluster += 1) {
        const material = foliageMaterials[Math.floor(random() * foliageMaterials.length)] ?? foliageMaterials[0];
        if (!material) continue;
        const blob = new THREE.Mesh(new THREE.IcosahedronGeometry(1.15 * scale, 0), material);
        blob.position.set(
          x + (random() - 0.5) * 1.5,
          (3.9 + cluster * 0.75 + random() * 0.4) * scale,
          z + (random() - 0.5) * 1.5,
        );
        const blobScale = 0.72 + random() * 0.4;
        blob.scale.set(blobScale, blobScale * 0.85, blobScale);
        blob.rotation.set(random() * Math.PI, random() * Math.PI, random() * Math.PI);
        trees.add(blob);
      }
      // Rocks anchor each tree instead of letting trunks meet the water plane.
      for (let rock = 0; rock < 2; rock += 1) {
        const lump = new THREE.Mesh(rockGeometry, rockMaterial);
        lump.position.set(x + (random() - 0.5) * 2.4, 0.25 + random() * 0.2, z + (random() - 0.5) * 2.2);
        lump.scale.set(1.1 + random() * 0.9, 0.5 + random() * 0.4, 1.1 + random() * 0.9);
        lump.rotation.set(random(), random(), random());
        trees.add(lump);
      }
    }
  }
  scene.add(trees);

  const pavilion = buildPavilion(THREE, glowTexture);
  pavilion.position.set(-15, 0.1, -9);
  pavilion.rotation.y = 0.35;
  scene.add(pavilion);

  const boat = buildBoat(THREE, glowTexture);
  boat.position.set(8, 0.16, -16);
  scene.add(boat);

  const birds = new THREE.Group();
  const birdStates: Array<{ bird: InstanceType<ThreeModule["Group"]>; wingL: Mesh; wingR: Mesh; phase: number; baseX: number; baseY: number; z: number }> = [];
  for (let i = 0; i < 5; i += 1) {
    const bird = new THREE.Group();
    const wingMaterial = new THREE.MeshBasicMaterial({ color: 0x2b3438 });
    const wingGeometry = new THREE.BoxGeometry(1.5, 0.05, 0.32);
    const wingL = new THREE.Mesh(wingGeometry, wingMaterial);
    const wingR = new THREE.Mesh(wingGeometry, wingMaterial);
    wingL.position.x = -0.7;
    wingR.position.x = 0.7;
    bird.add(wingL, wingR);
    const baseX = (random() - 0.5) * 70;
    const baseY = 20 + random() * 10;
    const z = -60 - random() * 50;
    bird.position.set(baseX, baseY, z);
    bird.scale.setScalar(0.7 + random() * 0.5);
    birds.add(bird);
    birdStates.push({ bird, wingL, wingR, phase: random() * Math.PI * 2, baseX, baseY, z });
  }
  scene.add(birds);

  const leafGeometry = new THREE.PlaneGeometry(0.3, 0.2);
  const leafMaterial = new THREE.MeshStandardMaterial({
    side: THREE.DoubleSide,
    roughness: 0.85,
    metalness: 0,
  });

  const fallingLeaves = new THREE.InstancedMesh(leafGeometry, leafMaterial, LEAF_COUNT);
  fallingLeaves.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
  const leafStates: LeafState[] = [];
  for (let i = 0; i < LEAF_COUNT; i += 1) {
    const side = random() < 0.5 ? -1 : 1;
    leafStates.push({
      x: side * (4 + random() * 22),
      y: 2 + random() * 12,
      z: 22 - random() * 34,
      speed: 0.3 + random() * 0.6,
      sway: 0.4 + random() * 1.1,
      phase: random() * Math.PI * 2,
      spin: (random() - 0.5) * 2.2,
      drift: 0.15 + random() * 0.5,
    });
    fallingLeaves.setColorAt(i, new THREE.Color(LEAF_COLORS[i % LEAF_COLORS.length] ?? 0xc7531f));
  }
  if (fallingLeaves.instanceColor) fallingLeaves.instanceColor.needsUpdate = true;
  scene.add(fallingLeaves);

  // Leaves that reached the lake keep drifting with the surface.
  const floatingLeaves = new THREE.InstancedMesh(leafGeometry, leafMaterial, FLOATING_LEAF_COUNT);
  floatingLeaves.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
  const floatingStates: Array<{ x: number; z: number; speed: number; spin: number }> = [];
  const floatingDummy = new THREE.Object3D();
  for (let i = 0; i < FLOATING_LEAF_COUNT; i += 1) {
    floatingStates.push({
      x: (random() - 0.5) * 70,
      z: 8 - random() * 70,
      speed: 0.1 + random() * 0.25,
      spin: random() * Math.PI * 2,
    });
    floatingLeaves.setColorAt(i, new THREE.Color(LEAF_COLORS[i % LEAF_COLORS.length] ?? 0xc7531f));
  }
  if (floatingLeaves.instanceColor) floatingLeaves.instanceColor.needsUpdate = true;
  scene.add(floatingLeaves);

  const updateFloating = (delta: number) => {
    for (let i = 0; i < floatingStates.length; i += 1) {
      const leaf = floatingStates[i];
      if (!leaf) continue;
      leaf.x += leaf.speed * delta;
      leaf.spin += delta * 0.25;
      if (leaf.x > 36) leaf.x = -36;
      floatingDummy.position.set(leaf.x, 0.05, leaf.z);
      floatingDummy.rotation.set(-Math.PI / 2, 0, leaf.spin);
      floatingDummy.scale.setScalar(0.9);
      floatingDummy.updateMatrix();
      floatingLeaves.setMatrixAt(i, floatingDummy.matrix);
    }
    floatingLeaves.instanceMatrix.needsUpdate = true;
  };

  const leafDummy = new THREE.Object3D();
  const updateLeaves = (delta: number) => {
    for (let i = 0; i < leafStates.length; i += 1) {
      const leaf = leafStates[i];
      if (!leaf) continue;
      leaf.y -= leaf.speed * delta;
      leaf.phase += delta * 1.3;
      leaf.x += leaf.drift * delta;
      if (leaf.y < 0.2 || leaf.x > 30) {
        leaf.y = 12 + random() * 3;
        leaf.x = (random() < 0.5 ? -1 : 1) * (4 + random() * 22);
        leaf.z = 22 - random() * 34;
      }
      leafDummy.position.set(
        leaf.x + Math.sin(leaf.phase) * leaf.sway,
        leaf.y,
        leaf.z + Math.cos(leaf.phase * 0.7) * leaf.sway * 0.6,
      );
      leafDummy.rotation.set(leaf.phase * 1.6, leaf.phase * 1.1, leaf.spin * leaf.phase);
      leafDummy.updateMatrix();
      fallingLeaves.setMatrixAt(i, leafDummy.matrix);
    }
    fallingLeaves.instanceMatrix.needsUpdate = true;
  };

  const lantern = pavilion.getObjectByName("lantern") as Mesh | null;
  const lanternLight = new THREE.PointLight(0xffb066, 26, 26, 2);
  lanternLight.position.set(-15, 2.2, -9);
  scene.add(lanternLight);

  const reduceMotion = window.matchMedia?.(REDUCED_MOTION_QUERY).matches ?? false;
  const pointer = { x: 0, y: 0 };
  const onPointerMove = (event: PointerEvent) => {
    pointer.x = (event.clientX / window.innerWidth) * 2 - 1;
    pointer.y = (event.clientY / window.innerHeight) * 2 - 1;
  };
  window.addEventListener("pointermove", onPointerMove, { passive: true });

  const placeCamera = (elapsed: number) => {
    camera.position.set(
      Math.sin(elapsed * 0.045) * 1.4 + pointer.x * 1.1,
      5.4 + Math.sin(elapsed * 0.07) * 0.18 - pointer.y * 0.4,
      24,
    );
    camera.lookAt(pointer.x * 1.2, 6.2 + pointer.y * 0.6, -70);
  };

  const resize = () => {
    const width = canvas.clientWidth || window.innerWidth;
    const height = canvas.clientHeight || window.innerHeight;
    renderer.setSize(width, height, false);
    camera.aspect = width / Math.max(height, 1);
    camera.updateProjectionMatrix();
    if (reduceMotion) {
      placeCamera(0);
      renderer.render(scene, camera);
    }
  };

  const resizeObserver = new ResizeObserver(resize);
  resizeObserver.observe(canvas);
  resize();

  let frame = 0;
  let elapsed = 0;
  const clock = new THREE.Clock();
  const tick = () => {
    if (document.hidden) {
      frame = requestAnimationFrame(tick);
      return;
    }
    const delta = Math.min(clock.getDelta(), 0.05);
    elapsed += delta;
    placeCamera(elapsed);
    updateLeaves(delta);
    updateFloating(delta);
    waterTexture.offset.x = (waterTexture.offset.x + delta * 0.006) % 1;
    waterTexture.offset.y = (waterTexture.offset.y + delta * 0.002) % 1;
    for (const bank of mistBanks) {
      bank.sprite.position.x = bank.state.x + Math.sin(elapsed * 0.035 + bank.state.phase) * bank.state.amplitude;
    }
    boat.position.z = -16 + Math.sin(elapsed * 0.08) * 4;
    boat.rotation.y = Math.sin(elapsed * 0.08) * 0.12;
    for (const state of birdStates) {
      const flap = Math.sin(elapsed * 5 + state.phase) * 0.55;
      state.wingL.rotation.z = flap;
      state.wingR.rotation.z = -flap;
      state.bird.position.x = state.baseX + Math.sin(elapsed * 0.12 + state.phase) * 9;
      state.bird.position.y = state.baseY + Math.sin(elapsed * 0.4 + state.phase) * 0.6;
    }
    if (lantern) {
      const glow = 1.8 + Math.sin(elapsed * 1.7) * 0.35;
      const material = lantern.material as InstanceType<ThreeModule["MeshStandardMaterial"]>;
      material.emissiveIntensity = glow;
      lanternLight.intensity = 24 + Math.sin(elapsed * 1.7) * 5;
    }
    renderer.render(scene, camera);
    frame = requestAnimationFrame(tick);
  };
  updateLeaves(0);
  updateFloating(0);
  placeCamera(0);
  if (!reduceMotion) frame = requestAnimationFrame(tick);

  return () => {
    cancelAnimationFrame(frame);
    resizeObserver.disconnect();
    window.removeEventListener("pointermove", onPointerMove);
    scene.traverse((object) => {
      const mesh = object as Mesh;
      mesh.geometry?.dispose();
      const material = mesh.material;
      if (Array.isArray(material)) material.forEach((entry) => entry.dispose());
      else material?.dispose();
    });
    glowTexture.dispose();
    mistTexture.dispose();
    waterTexture.dispose();
    renderer.dispose();
  };
}

/** Dawn sky: deep blue overhead, warm haze at the horizon. */
function createSkyDome(THREE: ThreeModule): Mesh {
  const canvas = document.createElement("canvas");
  canvas.width = 4;
  canvas.height = 256;
  const context = canvas.getContext("2d");
  if (context) {
    const gradient = context.createLinearGradient(0, 0, 0, 256);
    gradient.addColorStop(0, "#2b3f57");
    gradient.addColorStop(0.42, "#5e7a93");
    gradient.addColorStop(0.72, "#a8b7bd");
    gradient.addColorStop(0.88, "#e3c6a4");
    gradient.addColorStop(1, "#f4d9b4");
    context.fillStyle = gradient;
    context.fillRect(0, 0, 4, 256);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  const dome = new THREE.Mesh(
    new THREE.CylinderGeometry(320, 320, 220, 24, 1, true),
    new THREE.MeshBasicMaterial({ map: texture, side: THREE.BackSide, depthWrite: false, fog: false }),
  );
  dome.position.y = 70;
  return dome;
}

type RangeOptions = {
  color: number;
  z: number;
  count: number;
  spread: number;
  minHeight: number;
  maxHeight: number;
};

/** One cone-based ridge line; overlapping peaks read as a low-poly range. */
function addMountainRange(
  THREE: ThreeModule,
  scene: InstanceType<ThreeModule["Scene"]>,
  random: () => number,
  options: RangeOptions,
): InstanceType<ThreeModule["Group"]> {
  const range = new THREE.Group();
  const material = new THREE.MeshStandardMaterial({ color: options.color, roughness: 0.95, flatShading: true });
  for (let i = 0; i < options.count; i += 1) {
    const height = options.minHeight + random() * (options.maxHeight - options.minHeight);
    const radius = height * (0.8 + random() * 0.6);
    const peak = new THREE.Mesh(new THREE.ConeGeometry(radius, height, 5 + Math.floor(random() * 3)), material);
    peak.position.set(
      (random() - 0.5) * options.spread,
      height / 2 - 2,
      options.z + (random() - 0.5) * 34,
    );
    peak.rotation.y = random() * Math.PI;
    range.add(peak);
  }
  scene.add(range);
  return range;
}

/** Warm light path from the sun across the lake. */
function addSunPath(
  THREE: ThreeModule,
  texture: Texture,
): InstanceType<ThreeModule["Group"]> {
  const group = new THREE.Group();
  const streak = new THREE.Mesh(
    new THREE.PlaneGeometry(9, 120),
    new THREE.MeshBasicMaterial({
      map: texture,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.07,
    }),
  );
  streak.rotation.x = -Math.PI / 2;
  streak.position.set(-22, 0.07, -78);
  const horizonBand = new THREE.Mesh(
    new THREE.PlaneGeometry(150, 16),
    new THREE.MeshBasicMaterial({
      map: texture,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.12,
    }),
  );
  horizonBand.rotation.x = -Math.PI / 2;
  horizonBand.position.set(-10, 0.06, -150);
  group.add(streak, horizonBand);
  return group;
}

/** Open pavilion on the water: posts, floor, hip roof and a warm lantern. */
function buildPavilion(THREE: ThreeModule, glowTexture: Texture): InstanceType<ThreeModule["Group"]> {
  const pavilion = new THREE.Group();
  const wood = new THREE.MeshStandardMaterial({ color: 0x4a3627, roughness: 0.9 });
  const roofMaterial = new THREE.MeshStandardMaterial({ color: 0x33393c, roughness: 0.85, flatShading: true });

  const base = new THREE.Mesh(new THREE.CylinderGeometry(3.1, 3.4, 0.5, 7), rockMaterialFor(THREE));
  base.position.y = 0.25;
  const floor = new THREE.Mesh(new THREE.CylinderGeometry(2.6, 2.6, 0.22, 7), wood);
  floor.position.y = 0.62;
  pavilion.add(base, floor);

  const postGeometry = new THREE.CylinderGeometry(0.09, 0.11, 2.6, 6);
  for (let i = 0; i < 4; i += 1) {
    const angle = (i / 4) * Math.PI * 2 + Math.PI / 4;
    const post = new THREE.Mesh(postGeometry, wood);
    post.position.set(Math.cos(angle) * 1.9, 1.98, Math.sin(angle) * 1.9);
    pavilion.add(post);
  }
  const roof = new THREE.Mesh(new THREE.ConeGeometry(3.4, 1.5, 4), roofMaterial);
  roof.position.y = 3.95;
  roof.rotation.y = Math.PI / 4;
  pavilion.add(roof);

  const lanternMaterial = new THREE.MeshStandardMaterial({
    color: 0xffd9a0,
    emissive: 0xffa94d,
    emissiveIntensity: 1.8,
    roughness: 0.4,
  });
  const lantern = new THREE.Mesh(new THREE.SphereGeometry(0.22, 10, 8), lanternMaterial);
  lantern.name = "lantern";
  lantern.position.set(0, 2.5, 0);
  pavilion.add(lantern);
  pavilion.add(addGlow(THREE, glowTexture, [0, 2.5, 0], 3.6, 0.8));
  return pavilion;
}

function rockMaterialFor(THREE: ThreeModule): InstanceType<ThreeModule["MeshStandardMaterial"]> {
  return new THREE.MeshStandardMaterial({ color: 0x3a4244, roughness: 0.95, flatShading: true });
}

/** Small lantern boat, drifting slowly across the lake. */
function buildBoat(THREE: ThreeModule, glowTexture: Texture): InstanceType<ThreeModule["Group"]> {
  const boat = new THREE.Group();
  const wood = new THREE.MeshStandardMaterial({ color: 0x2f2620, roughness: 0.9 });
  const hull = new THREE.Mesh(new THREE.BoxGeometry(3.2, 0.4, 1.1), wood);
  hull.position.y = 0.12;
  const bow = new THREE.Mesh(new THREE.ConeGeometry(0.55, 1.1, 4), wood);
  bow.rotation.z = -Math.PI / 2;
  bow.position.set(2.1, 0.12, 0);
  const canopy = new THREE.Mesh(new THREE.CylinderGeometry(0.62, 0.62, 1.6, 6, 1, true), wood);
  canopy.rotation.z = Math.PI / 2;
  canopy.position.set(-0.3, 0.55, 0);
  const lanternMaterial = new THREE.MeshStandardMaterial({
    color: 0xffe0ae,
    emissive: 0xffb066,
    emissiveIntensity: 2.2,
    roughness: 0.4,
  });
  const lantern = new THREE.Mesh(new THREE.SphereGeometry(0.16, 10, 8), lanternMaterial);
  lantern.position.set(0.9, 0.55, 0);
  boat.add(hull, bow, canopy, lantern);
  boat.add(addGlow(THREE, glowTexture, [0.9, 0.55, 0], 2.4, 0.7));
  return boat;
}

/** Radial amber sprite used for the sun, lanterns and light paths. */
function createGlowTexture(THREE: ThreeModule): Texture {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (context) {
    const gradient = context.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
    gradient.addColorStop(0, "rgba(255, 231, 186, 1)");
    gradient.addColorStop(0.25, "rgba(255, 186, 110, 0.5)");
    gradient.addColorStop(1, "rgba(255, 160, 90, 0)");
    context.fillStyle = gradient;
    context.fillRect(0, 0, size, size);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/** Soft white puff for mist banks. */
function createMistTexture(THREE: ThreeModule): Texture {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (context) {
    const gradient = context.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
    gradient.addColorStop(0, "rgba(255, 255, 255, 0.85)");
    gradient.addColorStop(0.55, "rgba(240, 244, 248, 0.35)");
    gradient.addColorStop(1, "rgba(235, 240, 246, 0)");
    context.fillStyle = gradient;
    context.fillRect(0, 0, size, size);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/** Faint horizontal streaks that shimmer as the texture offset drifts. */
function createWaterTexture(THREE: ThreeModule): Texture {
  const size = 256;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (context) {
    context.fillStyle = "#000000";
    context.fillRect(0, 0, size, size);
    for (let i = 0; i < 46; i += 1) {
      const y = Math.random() * size;
      const width = 30 + Math.random() * 150;
      const x = Math.random() * size;
      context.strokeStyle = `rgba(220, 235, 240, ${0.05 + Math.random() * 0.12})`;
      context.lineWidth = 1 + Math.random() * 1.6;
      context.beginPath();
      context.moveTo(x, y);
      context.lineTo(x + width, y);
      context.stroke();
    }
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.wrapS = THREE.RepeatWrapping;
  texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(6, 14);
  return texture;
}

function addGlow(
  THREE: ThreeModule,
  texture: Texture,
  position: readonly [number, number, number],
  size: number,
  opacity: number,
): InstanceType<ThreeModule["Sprite"]> {
  const sprite = new THREE.Sprite(
    new THREE.SpriteMaterial({
      map: texture,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity,
    }),
  );
  sprite.position.set(...position);
  sprite.scale.set(size, size, 1);
  return sprite;
}
