import type { Texture } from "pixi.js";
import charactersUrl from "./assets/characters.png";
import floorsUrl from "./assets/floors.png";
import furnitureUrl from "./assets/furniture.png";
import wallsUrl from "./assets/walls.png";
import { FLOOR_FRAMES, FURNITURE_FRAMES } from "./assets/frames";

export type FurnitureSprite = keyof typeof FURNITURE_FRAMES;
export type FloorSprite = keyof typeof FLOOR_FRAMES;

/** Sheet rows: 0 faces the camera, 1 faces away, 2 faces right (mirrored for left). */
export type CharacterRow = 0 | 1 | 2;

export const CHARACTER_SHEETS = 12;
export const CHARACTER_FRAME_W = 16;
export const CHARACTER_FRAME_H = 32;
const CHARACTER_FRAMES_PER_ROW = 8;
const CHARACTER_SHEET_W = CHARACTER_FRAME_W * CHARACTER_FRAMES_PER_ROW;
const CHARACTER_SHEET_H = CHARACTER_FRAME_H * 3;

export interface OfficeTextures {
  furniture: Record<FurnitureSprite, Texture>;
  floors: Record<FloorSprite, Texture>;
  /** Indexed by the N=1, E=2, S=4, W=8 neighbour mask. Each piece is 16 x 32. */
  walls: Texture[];
  /** `characters[sheet][blink ? 1 : 0][row][frame]`. */
  characters: Texture[][][][];
}

function assetUrl(asset: string | { src: string }): string {
  return typeof asset === "string" ? asset : asset.src;
}

let cached: OfficeTextures | null = null;
let pending: Promise<OfficeTextures> | null = null;

/**
 * Loads the four atlases once per page and slices them into frames. Every
 * texture uses nearest-neighbour sampling so the 16 px art stays crisp at any
 * zoom.
 */
export function loadOfficeTextures(pixi: typeof import("pixi.js")): Promise<OfficeTextures> {
  if (cached) return Promise.resolve(cached);
  if (pending) return pending;

  const load = (asset: string | { src: string }) =>
    pixi.Assets.load<Texture>({
      src: assetUrl(asset),
      parser: "texture",
      data: { scaleMode: "nearest" },
    });

  pending = Promise.all([
    load(furnitureUrl),
    load(floorsUrl),
    load(wallsUrl),
    load(charactersUrl),
  ])
    .then(([furnitureBase, floorBase, wallBase, characterBase]) => {
      for (const base of [furnitureBase, floorBase, wallBase, characterBase]) {
        base.source.scaleMode = "nearest";
      }
      const cut = (base: Texture, x: number, y: number, w: number, h: number) =>
        new pixi.Texture({ source: base.source, frame: new pixi.Rectangle(x, y, w, h) });

      const furniture = Object.fromEntries(
        Object.entries(FURNITURE_FRAMES).map(([name, [x, y, w, h]]) => [
          name,
          cut(furnitureBase, x, y, w, h),
        ]),
      ) as Record<FurnitureSprite, Texture>;

      const floors = Object.fromEntries(
        Object.entries(FLOOR_FRAMES).map(([name, [x, y, w, h]]) => [
          name,
          cut(floorBase, x, y, w, h),
        ]),
      ) as Record<FloorSprite, Texture>;

      const walls = Array.from({ length: 16 }, (_, mask) =>
        cut(wallBase, (mask % 4) * 16, Math.floor(mask / 4) * 32, 16, 32),
      );

      const characters = Array.from({ length: CHARACTER_SHEETS }, (_, sheet) =>
        [0, 1].map((blink) =>
          [0, 1, 2].map((row) =>
            Array.from({ length: CHARACTER_FRAMES_PER_ROW }, (_, frame) =>
              cut(
                characterBase,
                sheet * CHARACTER_SHEET_W + frame * CHARACTER_FRAME_W,
                blink * CHARACTER_SHEET_H + row * CHARACTER_FRAME_H,
                CHARACTER_FRAME_W,
                CHARACTER_FRAME_H,
              ),
            ),
          ),
        ),
      );

      cached = { furniture, floors, walls, characters };
      return cached;
    })
    .catch((error: unknown) => {
      pending = null;
      throw error;
    });
  return pending;
}

/** The loaded atlases. Callers run after `loadOfficeTextures` resolves. */
export function officeTextures(): OfficeTextures {
  if (!cached) throw new Error("Office textures are used before loadOfficeTextures resolved");
  return cached;
}
