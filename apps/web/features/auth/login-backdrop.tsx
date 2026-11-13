"use client";

import { useEffect, useRef } from "react";

/**
 * WebGL backdrop for the sign-in screens: a quiet autumn evening in a
 * low-poly city — lit facades, street lamps, amber trees and leaves drifting
 * down the avenue. three.js is imported lazily so the login bundle stays
 * small and nothing runs during SSR; if WebGL is not available the poster
 * image behind the canvas stays visible.
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
        cleanup = startAutumnCityScene(THREE, canvas);
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

const NIGHT = 0x080c13;
const AMBER = 0xffb347;
const STREET_HALF_WIDTH = 5.6;
const LEAF_COLORS = [0xc7531f, 0xe0912f, 0xa8401d, 0xd7a63f, 0x8f3f1c, 0xb2692a];
const BUILDING_TINTS = ["#171c24", "#201720", "#1a2029"];
const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const LEAF_COUNT = 300;
const FALLEN_LEAF_COUNT = 240;

function startAutumnCityScene(THREE: ThreeModule, canvas: HTMLCanvasElement): () => void {
  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: true,
    powerPreference: "high-performance",
  });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.08;

  const scene = new THREE.Scene();
  scene.background = new THREE.Color(NIGHT);
  scene.fog = new THREE.FogExp2(NIGHT, 0.022);

  const camera = new THREE.PerspectiveCamera(46, 1, 0.1, 260);

  let seed = 21;
  const random = () => {
    seed = (seed * 16807) % 2147483647;
    return seed / 2147483647;
  };

  const glowTexture = createGlowTexture(THREE);
  scene.add(createSkyDome(THREE));

  scene.add(new THREE.HemisphereLight(0x91a4c6, 0x1a1310, 0.8));
  const moon = new THREE.DirectionalLight(0xb6c8de, 2);
  moon.position.set(-10, 20, 12);
  const dusk = new THREE.DirectionalLight(0xff9f5a, 0.75);
  dusk.position.set(8, 6, -30);
  scene.add(moon, dusk);

  const ground = new THREE.Mesh(
    new THREE.PlaneGeometry(280, 280),
    new THREE.MeshStandardMaterial({ color: 0x090b0f, roughness: 0.95 }),
  );
  ground.rotation.x = -Math.PI / 2;
  scene.add(ground);

  const asphalt = new THREE.Mesh(
    new THREE.PlaneGeometry(STREET_HALF_WIDTH * 2, 220),
    new THREE.MeshStandardMaterial({ color: 0x101319, roughness: 0.7, metalness: 0.34 }),
  );
  asphalt.rotation.x = -Math.PI / 2;
  asphalt.position.set(0, 0.02, -80);
  scene.add(asphalt);

  const sidewalkMaterial = new THREE.MeshStandardMaterial({ color: 0x181c23, roughness: 0.9 });
  const curbMaterial = new THREE.MeshStandardMaterial({ color: 0x262b33, roughness: 0.8 });
  for (const side of [-1, 1] as const) {
    const walk = new THREE.Mesh(new THREE.BoxGeometry(4.4, 0.3, 220), sidewalkMaterial);
    walk.position.set(side * (STREET_HALF_WIDTH + 2.2), 0.15, -80);
    const curb = new THREE.Mesh(new THREE.BoxGeometry(0.24, 0.36, 220), curbMaterial);
    curb.position.set(side * STREET_HALF_WIDTH, 0.18, -80);
    scene.add(walk, curb);
  }

  const dashDummy = new THREE.Object3D();
  const dashes = new THREE.InstancedMesh(
    new THREE.BoxGeometry(0.16, 0.02, 3),
    new THREE.MeshStandardMaterial({ color: 0xd9cfae, roughness: 0.6 }),
    32,
  );
  for (let i = 0; i < 32; i += 1) {
    dashDummy.position.set(0, 0.04, 12 - i * 4.4);
    dashDummy.updateMatrix();
    dashes.setMatrixAt(i, dashDummy.matrix);
  }
  scene.add(dashes);

  const facadeTextures = BUILDING_TINTS.map((tint, index) =>
    createFacadeTexture(THREE, tint, 0.22 + index * 0.06),
  );
  const buildings = new THREE.Group();
  for (const side of [-1, 1] as const) {
    let z = 8;
    while (z > -130) {
      const width = 5 + random() * 6;
      const depth = 6 + random() * 7;
      const height = 8 + random() * 22;
      const facade =
        facadeTextures[Math.floor(random() * facadeTextures.length)] ?? facadeTextures[0];
      const building = new THREE.Mesh(
        new THREE.BoxGeometry(depth, height, width),
        new THREE.MeshStandardMaterial({
          map: facade?.map ?? null,
          emissiveMap: facade?.emissive ?? null,
          emissive: 0xffffff,
          emissiveIntensity: 0.9,
          roughness: 0.85,
          metalness: 0.05,
        }),
      );
      building.position.set(side * (STREET_HALF_WIDTH + 4.4 + depth / 2), height / 2, z - width / 2);
      buildings.add(building);
      z -= width + 0.8 + random() * 1.6;
    }
  }
  scene.add(buildings);

  const poleMaterial = new THREE.MeshStandardMaterial({ color: 0x232a33, roughness: 0.6, metalness: 0.5 });
  const bulbMaterial = new THREE.MeshStandardMaterial({
    color: AMBER,
    emissive: AMBER,
    emissiveIntensity: 2.6,
    roughness: 0.4,
  });
  const lampLights: InstanceType<ThreeModule["PointLight"]>[] = [];
  for (let i = 0; i < 6; i += 1) {
    const side = i % 2 === 0 ? -1 : 1;
    const z = 8 - i * 15;
    const x = side * (STREET_HALF_WIDTH + 1.6);
    const bulbX = x - side * 1.5;
    const pole = new THREE.Mesh(new THREE.CylinderGeometry(0.1, 0.15, 7.2, 6), poleMaterial);
    pole.position.set(x, 3.6, z);
    const arm = new THREE.Mesh(new THREE.BoxGeometry(1.5, 0.12, 0.12), poleMaterial);
    arm.position.set(x - side * 0.75, 7.1, z);
    const bulb = new THREE.Mesh(new THREE.SphereGeometry(0.18, 10, 8), bulbMaterial);
    bulb.position.set(bulbX, 7.0, z);
    scene.add(pole, arm, bulb);
    scene.add(addGlow(THREE, glowTexture, [bulbX, 7.0, z], 3.4));
    scene.add(addReflection(THREE, glowTexture, [bulbX, z], 2.2));
    if (i < 2) {
      const light = new THREE.PointLight(AMBER, 110, 45, 2);
      light.position.set(bulbX, 7.0, z);
      scene.add(light);
      lampLights.push(light);
    }
  }

  const trunkGeometry = new THREE.CylinderGeometry(0.16, 0.28, 3.4, 6);
  const foliageGeometry = new THREE.IcosahedronGeometry(1.15, 0);
  const trunkMaterial = new THREE.MeshStandardMaterial({ color: 0x3a2a20, roughness: 0.95 });
  const foliageMaterials = LEAF_COLORS.map(
    (color) => new THREE.MeshStandardMaterial({ color, roughness: 0.9, flatShading: true }),
  );
  const trees = new THREE.Group();
  for (let i = 0; i < 14; i += 1) {
    const side = i % 2 === 0 ? -1 : 1;
    const x = side * (STREET_HALF_WIDTH + 1.6);
    const z = 2 - i * 9 - random() * 2;
    const trunk = new THREE.Mesh(trunkGeometry, trunkMaterial);
    trunk.position.set(x, 1.7, z);
    trees.add(trunk);
    for (let cluster = 0; cluster < 3; cluster += 1) {
      const material = foliageMaterials[Math.floor(random() * foliageMaterials.length)] ?? foliageMaterials[0];
      if (!material) continue;
      const blob = new THREE.Mesh(foliageGeometry, material);
      blob.position.set(
        x + (random() - 0.5) * 1.3,
        4.0 + cluster * 0.7 + random() * 0.4,
        z + (random() - 0.5) * 1.3,
      );
      const scale = 0.72 + random() * 0.42;
      blob.scale.set(scale, scale * 0.85, scale);
      blob.rotation.set(random() * Math.PI, random() * Math.PI, random() * Math.PI);
      trees.add(blob);
    }
  }
  scene.add(trees);

  const cars = new THREE.Group();
  const carColors = [0x3c4653, 0x4a352c, 0x2b3a40];
  for (let i = 0; i < 3; i += 1) {
    const side = i % 2 === 0 ? -1 : 1;
    const car = buildCar(THREE, carColors[i] ?? 0x3c4653);
    car.position.set(side * (STREET_HALF_WIDTH - 1.3), 0, -6 - i * 16 - random() * 4);
    cars.add(car);
  }
  scene.add(cars);

  const leafGeometry = new THREE.PlaneGeometry(0.23, 0.15);
  const leafMaterial = new THREE.MeshStandardMaterial({
    side: THREE.DoubleSide,
    roughness: 0.85,
    metalness: 0,
  });

  const fallingLeaves = new THREE.InstancedMesh(leafGeometry, leafMaterial, LEAF_COUNT);
  fallingLeaves.instanceMatrix.setUsage(THREE.DynamicDrawUsage);
  const leafStates: LeafState[] = [];
  for (let i = 0; i < LEAF_COUNT; i += 1) {
    leafStates.push({
      x: (random() - 0.5) * 24,
      y: 0.6 + random() * 9.4,
      z: 6 - random() * 74,
      speed: 0.35 + random() * 0.7,
      sway: 0.4 + random() * 1.05,
      phase: random() * Math.PI * 2,
      spin: (random() - 0.5) * 2.4,
    });
    fallingLeaves.setColorAt(i, new THREE.Color(LEAF_COLORS[i % LEAF_COLORS.length] ?? 0xc7531f));
  }
  if (fallingLeaves.instanceColor) fallingLeaves.instanceColor.needsUpdate = true;
  scene.add(fallingLeaves);

  const fallenLeaves = new THREE.InstancedMesh(leafGeometry, leafMaterial, FALLEN_LEAF_COUNT);
  const fallenDummy = new THREE.Object3D();
  for (let i = 0; i < FALLEN_LEAF_COUNT; i += 1) {
    const onWalk = random() < 0.4;
    const side = random() < 0.5 ? -1 : 1;
    const x = onWalk
      ? side * (STREET_HALF_WIDTH + 0.4 + random() * 3.6)
      : (random() - 0.5) * STREET_HALF_WIDTH * 2;
    fallenDummy.position.set(x, 0.04 + random() * 0.02, 14 - random() * 80);
    fallenDummy.rotation.set(-Math.PI / 2, 0, random() * Math.PI * 2);
    fallenDummy.scale.setScalar(0.8 + random() * 0.8);
    fallenDummy.updateMatrix();
    fallenLeaves.setMatrixAt(i, fallenDummy.matrix);
    fallenLeaves.setColorAt(i, new THREE.Color(LEAF_COLORS[i % LEAF_COLORS.length] ?? 0xc7531f));
  }
  if (fallenLeaves.instanceColor) fallenLeaves.instanceColor.needsUpdate = true;
  scene.add(fallenLeaves);

  const leafDummy = new THREE.Object3D();
  const updateLeaves = (delta: number) => {
    for (let i = 0; i < leafStates.length; i += 1) {
      const leaf = leafStates[i];
      if (!leaf) continue;
      leaf.y -= leaf.speed * delta;
      leaf.phase += delta * 1.4;
      if (leaf.y < 0.2) {
        leaf.y = 9 + random() * 2;
        leaf.x = (random() - 0.5) * 24;
        leaf.z = 6 - random() * 74;
      }
      leafDummy.position.set(
        leaf.x + Math.sin(leaf.phase) * leaf.sway,
        leaf.y,
        leaf.z + Math.cos(leaf.phase * 0.7) * leaf.sway * 0.6,
      );
      leafDummy.rotation.set(leaf.phase * 1.7, leaf.phase * 1.1, leaf.spin * leaf.phase);
      leafDummy.updateMatrix();
      fallingLeaves.setMatrixAt(i, leafDummy.matrix);
    }
    fallingLeaves.instanceMatrix.needsUpdate = true;
  };

  const reduceMotion = window.matchMedia?.(REDUCED_MOTION_QUERY).matches ?? false;
  const pointer = { x: 0, y: 0 };
  const onPointerMove = (event: PointerEvent) => {
    pointer.x = (event.clientX / window.innerWidth) * 2 - 1;
    pointer.y = (event.clientY / window.innerHeight) * 2 - 1;
  };
  window.addEventListener("pointermove", onPointerMove, { passive: true });

  const placeCamera = (elapsed: number) => {
    camera.position.set(
      0.4 + Math.sin(elapsed * 0.05) * 0.9 + pointer.x * 0.7,
      4.9 + Math.sin(elapsed * 0.08) * 0.16 - pointer.y * 0.35,
      15.5,
    );
    camera.lookAt(0.3 + pointer.x * 0.6, 4.4, -18);
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
    for (const light of lampLights) {
      light.intensity = 110 + Math.sin(elapsed * 0.9 + light.position.z) * 14;
    }
    renderer.render(scene, camera);
    frame = requestAnimationFrame(tick);
  };
  updateLeaves(0);
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
    for (const facade of facadeTextures) {
      facade.map.dispose();
      facade.emissive.dispose();
    }
    glowTexture.dispose();
    renderer.dispose();
  };
}

type LeafState = {
  x: number;
  y: number;
  z: number;
  speed: number;
  sway: number;
  phase: number;
  spin: number;
};

/** Night sky with a warm band of city light at the horizon. */
function createSkyDome(THREE: ThreeModule): Mesh {
  const canvas = document.createElement("canvas");
  canvas.width = 4;
  canvas.height = 128;
  const context = canvas.getContext("2d");
  if (context) {
    const gradient = context.createLinearGradient(0, 0, 0, 128);
    gradient.addColorStop(0, "#05070d");
    gradient.addColorStop(0.55, "#0a1220");
    gradient.addColorStop(0.86, "#2a1d1c");
    gradient.addColorStop(1, "#45281f");
    context.fillStyle = gradient;
    context.fillRect(0, 0, 4, 128);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  const dome = new THREE.Mesh(
    new THREE.CylinderGeometry(150, 150, 120, 24, 1, true),
    new THREE.MeshBasicMaterial({ map: texture, side: THREE.BackSide, depthWrite: false, fog: false }),
  );
  dome.position.y = 40;
  return dome;
}

/**
 * Lit-window facade for one city block: the wall colour map, plus a glow map
 * where only the lit windows are bright. Keeping the glow separate stops the
 * wall itself from emitting, which otherwise turned every building into a
 * warm sepia slab.
 */
function createFacadeTexture(
  THREE: ThreeModule,
  tint: string,
  litChance: number,
): { map: Texture; emissive: Texture } {
  const width = 96;
  const height = 192;
  const wallCanvas = document.createElement("canvas");
  wallCanvas.width = width;
  wallCanvas.height = height;
  const glowCanvas = document.createElement("canvas");
  glowCanvas.width = width;
  glowCanvas.height = height;
  const wall = wallCanvas.getContext("2d");
  const glow = glowCanvas.getContext("2d");
  if (wall) {
    wall.fillStyle = tint;
    wall.fillRect(0, 0, width, height);
  }
  if (glow) {
    glow.fillStyle = "#000000";
    glow.fillRect(0, 0, width, height);
  }
  if (wall && glow) {
    const columns = 5;
    const rows = 12;
    const padX = 9;
    const padY = 8;
    const cellWidth = (width - padX * 2) / columns;
    const cellHeight = (height - padY * 2) / rows;
    for (let row = 0; row < rows; row += 1) {
      for (let column = 0; column < columns; column += 1) {
        const lit = Math.random() < litChance;
        const windowColor = lit
          ? Math.random() < 0.5
            ? "#ffc978"
            : "#ffe2ac"
          : "#0c1016";
        const x = padX + column * cellWidth + 3;
        const y = padY + row * cellHeight + 3;
        const windowWidth = cellWidth - 6;
        const windowHeight = cellHeight - 10;
        wall.fillStyle = windowColor;
        wall.fillRect(x, y, windowWidth, windowHeight);
        if (lit) {
          glow.fillStyle = windowColor;
          glow.fillRect(x, y, windowWidth, windowHeight);
        }
      }
    }
  }
  const map = new THREE.CanvasTexture(wallCanvas);
  map.colorSpace = THREE.SRGBColorSpace;
  const emissive = new THREE.CanvasTexture(glowCanvas);
  emissive.colorSpace = THREE.SRGBColorSpace;
  return { map, emissive };
}

/** Radial amber sprite used for lamp halos and wet-asphalt light pools. */
function createGlowTexture(THREE: ThreeModule): Texture {
  const size = 128;
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  const context = canvas.getContext("2d");
  if (context) {
    const gradient = context.createRadialGradient(size / 2, size / 2, 0, size / 2, size / 2, size / 2);
    gradient.addColorStop(0, "rgba(255, 219, 160, 1)");
    gradient.addColorStop(0.3, "rgba(255, 173, 82, 0.5)");
    gradient.addColorStop(1, "rgba(255, 150, 60, 0)");
    context.fillStyle = gradient;
    context.fillRect(0, 0, size, size);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

function addGlow(
  THREE: ThreeModule,
  texture: Texture,
  position: readonly [number, number, number],
  size: number,
): InstanceType<ThreeModule["Sprite"]> {
  const sprite = new THREE.Sprite(
    new THREE.SpriteMaterial({
      map: texture,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.85,
    }),
  );
  sprite.position.set(...position);
  sprite.scale.set(size, size, 1);
  return sprite;
}

function addReflection(
  THREE: ThreeModule,
  texture: Texture,
  position: readonly [number, number],
  width: number,
): Mesh {
  const mesh = new THREE.Mesh(
    new THREE.PlaneGeometry(width, width * 3.2),
    new THREE.MeshBasicMaterial({
      map: texture,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.14,
    }),
  );
  mesh.rotation.x = -Math.PI / 2;
  mesh.position.set(position[0], 0.05, position[1] + width * 1.2);
  return mesh;
}

/** Parked car silhouette: body, cabin and two tail lights. */
function buildCar(THREE: ThreeModule, color: number): InstanceType<ThreeModule["Group"]> {
  const car = new THREE.Group();
  const body = new THREE.Mesh(
    new THREE.BoxGeometry(1.9, 0.62, 4.2),
    new THREE.MeshStandardMaterial({ color, roughness: 0.42, metalness: 0.5 }),
  );
  body.position.y = 0.55;
  const cabin = new THREE.Mesh(
    new THREE.BoxGeometry(1.66, 0.56, 2.1),
    new THREE.MeshStandardMaterial({ color: 0x0d1116, roughness: 0.25, metalness: 0.4 }),
  );
  cabin.position.set(0, 1.12, -0.15);
  const tailMaterial = new THREE.MeshStandardMaterial({
    color: 0xff5a3c,
    emissive: 0xff3b1f,
    emissiveIntensity: 1.8,
  });
  const tailGeometry = new THREE.BoxGeometry(0.32, 0.12, 0.06);
  const tailLeft = new THREE.Mesh(tailGeometry, tailMaterial);
  tailLeft.position.set(-0.62, 0.62, 2.11);
  const tailRight = new THREE.Mesh(tailGeometry, tailMaterial);
  tailRight.position.set(0.62, 0.62, 2.11);
  car.add(body, cabin, tailLeft, tailRight);
  return car;
}
