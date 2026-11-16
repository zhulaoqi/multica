// Generated from the pixel-agents asset pack; see CREDITS.md.
// Each frame is [x, y, width, height] in its atlas.

export const FURNITURE_FRAMES = {
  bin: [144, 96, 16, 16],
  bookshelf: [112, 96, 32, 16],
  cactus: [144, 64, 16, 32],
  clock: [96, 64, 16, 32],
  coffee: [32, 96, 16, 16],
  coffeeTable: [0, 64, 32, 32],
  cushionedBench: [240, 64, 16, 16],
  cushionedChairBack: [0, 96, 16, 16],
  deskFront: [48, 0, 48, 32],
  doubleBookshelf: [64, 64, 32, 32],
  hangingPlant: [160, 64, 16, 32],
  largePainting: [176, 64, 32, 32],
  pcBack: [160, 0, 16, 32],
  pcOff: [96, 0, 16, 32],
  pcOn1: [112, 0, 16, 32],
  pcOn2: [128, 0, 16, 32],
  pcOn3: [144, 0, 16, 32],
  pcSide: [176, 0, 16, 32],
  plant: [112, 64, 16, 32],
  plant2: [128, 64, 16, 32],
  pot: [160, 96, 16, 16],
  smallPainting: [208, 64, 16, 32],
  smallPainting2: [224, 64, 16, 32],
  smallTableFront: [224, 0, 32, 32],
  sofaBack: [80, 96, 32, 16],
  sofaFront: [48, 96, 32, 16],
  tableFront: [0, 0, 48, 64],
  whiteboard: [32, 64, 32, 32],
  woodenBench: [16, 96, 16, 16],
  woodenChairBack: [208, 0, 16, 32],
  woodenChairFront: [192, 0, 16, 32],
} as const satisfies Record<string, readonly [number, number, number, number]>;

export const FLOOR_FRAMES = {
  office: [0, 0, 16, 16],
  lounge: [16, 0, 16, 16],
  kitchen: [32, 0, 16, 16],
  meeting: [48, 0, 16, 16],
  reception: [64, 0, 16, 16],
  server: [80, 0, 16, 16],
  print: [96, 0, 16, 16],
} as const satisfies Record<string, readonly [number, number, number, number]>;

/** Flat fill for the floor cell under each wall tile. */
export const WALL_FILL = 0x000000;
