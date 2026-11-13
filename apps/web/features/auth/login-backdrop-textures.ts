import type * as THREE from "three";

type ThreeModule = typeof import("three");
type Texture = InstanceType<ThreeModule["Texture"]>;

/**
 * Procedural canvas textures for the sign-in backdrop — no image files over
 * the network. Glow sprites carry the bloom, streaks carry wet-ground light
 * reflections, mist is a soft blob and the noise tile drives water ripple.
 */

function makeCanvas(size: number): [HTMLCanvasElement, CanvasRenderingContext2D | null] {
  const canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  return [canvas, canvas.getContext("2d")];
}

export function createGlowTexture(THREE: ThreeModule): Texture {
  const [canvas, ctx] = makeCanvas(128);
  if (ctx) {
    const gradient = ctx.createRadialGradient(64, 64, 0, 64, 64, 64);
    gradient.addColorStop(0, "rgba(255, 220, 170, 1)");
    gradient.addColorStop(0.28, "rgba(255, 180, 100, 0.45)");
    gradient.addColorStop(1, "rgba(255, 150, 70, 0)");
    ctx.fillStyle = gradient;
    ctx.fillRect(0, 0, 128, 128);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/** Vertical streak used for wet-ground light reflections and the sun path. */
export function createStreakTexture(THREE: ThreeModule): Texture {
  const [canvas, ctx] = makeCanvas(128);
  if (ctx) {
    const vertical = ctx.createLinearGradient(0, 0, 0, 128);
    vertical.addColorStop(0, "rgba(255, 190, 120, 0.85)");
    vertical.addColorStop(0.6, "rgba(255, 170, 90, 0.28)");
    vertical.addColorStop(1, "rgba(255, 160, 80, 0)");
    ctx.fillStyle = vertical;
    // Break the streak into ragged segments so it reads as rippled water.
    for (let y = 0; y < 128; y += 4) {
      const inset = Math.abs(Math.sin(y * 0.7)) * 26 + Math.random() * 8;
      ctx.fillRect(inset, y, 128 - inset * 2, 3);
    }
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

export function createMistTexture(THREE: ThreeModule): Texture {
  const [canvas, ctx] = makeCanvas(128);
  if (ctx) {
    const gradient = ctx.createRadialGradient(64, 64, 0, 64, 64, 64);
    gradient.addColorStop(0, "rgba(210, 222, 235, 0.5)");
    gradient.addColorStop(0.55, "rgba(190, 205, 220, 0.18)");
    gradient.addColorStop(1, "rgba(180, 195, 212, 0)");
    ctx.fillStyle = gradient;
    ctx.fillRect(0, 0, 128, 128);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/** Tiling value noise for the water ripple. */
export function createNoiseTexture(THREE: ThreeModule): Texture {
  const [canvas, ctx] = makeCanvas(256);
  if (ctx) {
    const image = ctx.createImageData(256, 256);
    const lattice = 32;
    const grid: number[] = [];
    for (let i = 0; i < lattice * lattice; i += 1) grid.push(Math.random());
    const at = (x: number, y: number) =>
      grid[((y % lattice) + lattice) % lattice * lattice + ((x % lattice) + lattice) % lattice] ?? 0;
    for (let y = 0; y < 256; y += 1) {
      for (let x = 0; x < 256; x += 1) {
        const gx = (x / 256) * lattice;
        const gy = (y / 256) * lattice;
        const x0 = Math.floor(gx);
        const y0 = Math.floor(gy);
        const fx = gx - x0;
        const fy = gy - y0;
        const sx = fx * fx * (3 - 2 * fx);
        const sy = fy * fy * (3 - 2 * fy);
        const value =
          at(x0, y0) * (1 - sx) * (1 - sy) +
          at(x0 + 1, y0) * sx * (1 - sy) +
          at(x0, y0 + 1) * (1 - sx) * sy +
          at(x0 + 1, y0 + 1) * sx * sy;
        const v = Math.floor(value * 255);
        const index = (y * 256 + x) * 4;
        image.data.set([v, v, v, 255], index);
      }
    }
    ctx.putImageData(image, 0, 0);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.wrapS = THREE.RepeatWrapping;
  texture.wrapT = THREE.RepeatWrapping;
  return texture;
}

/**
 * Soft cone texture for the volumetric lamp beams: bright at the lamp, fading
 * to nothing at the ground and at the silhouette edges, so the cone reads as
 * light in the air instead of a translucent cloth.
 */
export function createBeamTexture(THREE: ThreeModule): Texture {
  const canvas = document.createElement("canvas");
  canvas.width = 64;
  canvas.height = 256;
  const context = canvas.getContext("2d");
  if (context) {
    const vertical = context.createLinearGradient(0, 0, 0, 256);
    vertical.addColorStop(0, "rgba(255, 214, 160, 0)");
    vertical.addColorStop(0.14, "rgba(255, 205, 145, 0.55)");
    vertical.addColorStop(0.5, "rgba(255, 185, 110, 0.16)");
    vertical.addColorStop(1, "rgba(255, 170, 90, 0)");
    context.fillStyle = vertical;
    context.fillRect(0, 0, 64, 256);
    // Feather the sides and the top so the quad has no visible rectangle.
    const horizontal = context.createLinearGradient(0, 0, 64, 0);
    horizontal.addColorStop(0, "rgba(0, 0, 0, 0)");
    horizontal.addColorStop(0.35, "rgba(0, 0, 0, 1)");
    horizontal.addColorStop(0.65, "rgba(0, 0, 0, 1)");
    horizontal.addColorStop(1, "rgba(0, 0, 0, 0)");
    context.globalCompositeOperation = "destination-in";
    context.fillStyle = horizontal;
    context.fillRect(0, 0, 64, 256);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  return texture;
}

/**
 * Container corrugation: soft vertical ribs plus darker top/bottom rails, so
 * the yard reads as painted steel boxes instead of flat slabs at distance.
 */
export function createContainerTexture(THREE: ThreeModule): Texture {
  const canvas = document.createElement("canvas");
  canvas.width = 64;
  canvas.height = 64;
  const context = canvas.getContext("2d");
  if (context) {
    context.fillStyle = "#ffffff";
    context.fillRect(0, 0, 64, 64);
    for (let x = 0; x < 64; x += 4) {
      context.fillStyle = "rgba(0, 0, 0, 0.14)";
      context.fillRect(x, 0, 1.4, 64);
    }
    context.fillStyle = "rgba(0, 0, 0, 0.45)";
    context.fillRect(0, 0, 64, 4);
    context.fillRect(0, 60, 64, 4);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.wrapS = THREE.RepeatWrapping;
  texture.wrapT = THREE.ClampToEdgeWrapping;
  texture.repeat.set(7, 1);
  return texture;
}
