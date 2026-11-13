"use client";

import { useEffect, useRef } from "react";

/**
 * WebGL backdrop for the sign-in screens: a night-time container port,
 * referencing the low-poly 3D scene the team liked (fog, steel stacks,
 * amber signal lights, a gantry crane). three.js is imported lazily so the
 * login bundle stays small and nothing runs during SSR; if WebGL is not
 * available the poster image behind the canvas stays visible.
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
        cleanup = startPortScene(THREE, canvas);
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

const INK = 0x070a10;
const STEEL = 0x8d9aa3;
const SIGNAL = 0xffb347;
const RUST = 0xb4562a;
const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";
const LAMP_LEFT = 900;
const LAMP_RIGHT = 640;

function startPortScene(THREE: ThreeModule, canvas: HTMLCanvasElement): () => void {
  const renderer = new THREE.WebGLRenderer({
    canvas,
    antialias: true,
    powerPreference: "high-performance",
  });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = 1.25;

  const scene = new THREE.Scene();
  scene.background = new THREE.Color(INK);
  scene.fog = new THREE.FogExp2(INK, 0.018);

  const camera = new THREE.PerspectiveCamera(44, 1, 0.1, 200);

  scene.add(new THREE.HemisphereLight(0x9fb4c8, 0x2a1b12, 1.5));
  const moon = new THREE.DirectionalLight(0xb9cde0, 2);
  moon.position.set(8, 16, 10);
  const warmFill = new THREE.DirectionalLight(0xff9a4a, 1.6);
  warmFill.position.set(-18, 9, 6);
  scene.add(moon, warmFill);

  const lampLeft = new THREE.PointLight(SIGNAL, LAMP_LEFT, 90, 1.5);
  lampLeft.position.set(-13, 7, 2);
  const lampRight = new THREE.PointLight(SIGNAL, LAMP_RIGHT, 80, 1.6);
  lampRight.position.set(15, 8, -8);
  scene.add(lampLeft, lampRight);
  const glowTexture = createGlowTexture(THREE);

  const ground = new THREE.Mesh(
    new THREE.PlaneGeometry(260, 260),
    new THREE.MeshStandardMaterial({ color: 0x0a0f14, roughness: 0.7, metalness: 0.4 }),
  );
  ground.rotation.x = -Math.PI / 2;
  scene.add(ground);

  const grid = new THREE.GridHelper(260, 104, 0x233039, 0x18222a);
  grid.position.y = 0.02;
  scene.add(grid);

  const containerGeometry = new THREE.BoxGeometry(6, 2.6, 2.6);
  const palette = [0x27333c, 0x2f3d47, RUST, 0x2f5d55, 0xb9722d, 0x8c8577];
  const stacks = new THREE.Group();
  let seed = 7;
  const random = () => {
    seed = (seed * 16807) % 2147483647;
    return seed / 2147483647;
  };
  for (let row = 0; row < 26; row += 1) {
    const side = random() < 0.5 ? -1 : 1;
    const x = side * (8 + random() * 22);
    const z = -26 + random() * 24;
    const height = 1 + Math.floor(random() * 3);
    for (let level = 0; level < height; level += 1) {
      const box = new THREE.Mesh(
        containerGeometry,
        new THREE.MeshStandardMaterial({
          color: palette[Math.floor(random() * palette.length)] ?? 0x1c242b,
          roughness: 0.75,
          metalness: 0.35,
        }),
      );
      box.position.set(x, 1.3 + level * 2.6, z);
      box.rotation.y = random() < 0.5 ? 0 : Math.PI / 2;
      stacks.add(box);
    }
  }

  const signalMaterial = new THREE.MeshStandardMaterial({
    color: SIGNAL,
    emissive: SIGNAL,
    emissiveIntensity: 2.4,
    roughness: 0.4,
  });
  for (const [x, z] of [
    [-9, 4],
    [10, -2],
    [-17, -12],
    [20, -16],
  ] as const) {
    const mast = new THREE.Mesh(new THREE.CylinderGeometry(0.09, 0.09, 5.4, 6), signalMaterial);
    mast.position.set(x, 2.7, z);
    const lamp = new THREE.Mesh(new THREE.SphereGeometry(0.22, 12, 10), signalMaterial);
    lamp.position.set(x, 5.5, z);
    stacks.add(mast, lamp);
    stacks.add(addGlow(THREE, glowTexture, [x, 5.5, z], 2.6));
    stacks.add(addReflection(THREE, glowTexture, [x, z], 2.4));
  }
  scene.add(stacks);

  scene.add(buildCrane(THREE, glowTexture));

  const dustGeometry = new THREE.BufferGeometry();
  const dustPositions = new Float32Array(420 * 3);
  for (let i = 0; i < 420; i += 1) {
    dustPositions[i * 3] = (Math.random() - 0.5) * 60;
    dustPositions[i * 3 + 1] = Math.random() * 14;
    dustPositions[i * 3 + 2] = -30 + Math.random() * 34;
  }
  dustGeometry.setAttribute("position", new THREE.BufferAttribute(dustPositions, 3));
  const dust = new THREE.Points(
    dustGeometry,
    new THREE.PointsMaterial({
      color: 0xffcf8a,
      size: 0.07,
      transparent: true,
      opacity: 0.55,
      depthWrite: false,
    }),
  );
  scene.add(dust);

  const reduceMotion = window.matchMedia?.(REDUCED_MOTION_QUERY).matches ?? false;
  const pointer = { x: 0, y: 0 };
  const onPointerMove = (event: PointerEvent) => {
    pointer.x = (event.clientX / window.innerWidth) * 2 - 1;
    pointer.y = (event.clientY / window.innerHeight) * 2 - 1;
  };
  window.addEventListener("pointermove", onPointerMove, { passive: true });

  const placeCamera = (elapsed: number) => {
    camera.position.set(
      Math.sin(elapsed * 0.045) * 1.3 + pointer.x * 0.9,
      4.4 + Math.sin(elapsed * 0.07) * 0.22 - pointer.y * 0.45,
      16,
    );
    camera.lookAt(0, 3.6, -8);
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
  const clock = new THREE.Clock();
  const tick = () => {
    if (document.hidden) {
      frame = requestAnimationFrame(tick);
      return;
    }
    const elapsed = clock.getElapsedTime();
    placeCamera(elapsed);
    dust.position.y = (elapsed * 0.12) % 2;
    lampLeft.intensity = LAMP_LEFT + Math.sin(elapsed * 0.9) * 90;
    lampRight.intensity = LAMP_RIGHT + Math.sin(elapsed * 0.7 + 1.7) * 70;
    renderer.render(scene, camera);
    frame = requestAnimationFrame(tick);
  };
  if (!reduceMotion) frame = requestAnimationFrame(tick);

  return () => {
    cancelAnimationFrame(frame);
    resizeObserver.disconnect();
    window.removeEventListener("pointermove", onPointerMove);
    scene.traverse((object) => {
      const mesh = object as InstanceType<ThreeModule["Mesh"]>;
      mesh.geometry?.dispose();
      const material = mesh.material;
      if (Array.isArray(material)) material.forEach((entry) => entry.dispose());
      else material?.dispose();
    });
    renderer.dispose();
  };
}

/** Radial amber sprite used for lamp halos and wet-ground reflections. */
function createGlowTexture(THREE: ThreeModule): InstanceType<ThreeModule["Texture"]> {
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
  texture: InstanceType<ThreeModule["Texture"]>,
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
  texture: InstanceType<ThreeModule["Texture"]>,
  position: readonly [number, number],
  width: number,
): InstanceType<ThreeModule["Mesh"]> {
  const mesh = new THREE.Mesh(
    new THREE.PlaneGeometry(width, width * 3.2),
    new THREE.MeshBasicMaterial({
      map: texture,
      blending: THREE.AdditiveBlending,
      transparent: true,
      depthWrite: false,
      opacity: 0.16,
    }),
  );
  mesh.rotation.x = -Math.PI / 2;
  mesh.position.set(position[0], 0.03, position[1] + width * 1.2);
  return mesh;
}

/** Gantry crane silhouette on the right, matching the port's signage look. */
function buildCrane(
  THREE: ThreeModule,
  glowTexture: InstanceType<ThreeModule["Texture"]>,
): InstanceType<ThreeModule["Group"]> {
  const crane = new THREE.Group();
  const steel = new THREE.MeshStandardMaterial({ color: 0x2a343c, roughness: 0.7, metalness: 0.6 });
  const legGeometry = new THREE.BoxGeometry(0.7, 14, 0.7);
  for (const [x, z] of [
    [14, -14],
    [14, -6],
    [24, -14],
    [24, -6],
  ] as const) {
    const leg = new THREE.Mesh(legGeometry, steel);
    leg.position.set(x, 7, z);
    crane.add(leg);
  }
  const beam = new THREE.Mesh(new THREE.BoxGeometry(13, 1, 1), steel);
  beam.position.set(19, 14, -10);
  const trolley = new THREE.Mesh(new THREE.BoxGeometry(1.4, 0.8, 1.4), steel);
  trolley.position.set(19, 13.4, -10);
  const cable = new THREE.Mesh(new THREE.BoxGeometry(0.08, 5, 0.08), steel);
  cable.position.set(19, 10.8, -10);
  const beaconMaterial = new THREE.MeshStandardMaterial({
    color: SIGNAL,
    emissive: SIGNAL,
    emissiveIntensity: 3,
  });
  const beacon = new THREE.Mesh(new THREE.SphereGeometry(0.3, 12, 10), beaconMaterial);
  beacon.position.set(25.4, 14.8, -10);
  crane.add(beam, trolley, cable, beacon);
  crane.add(addGlow(THREE, glowTexture, [25.4, 14.8, -10], 3.4));
  return crane;
}
