/**
 * Brand geometry. The icon is YY for 易研: straight strokes of one constant
 * width, drawn larger than the tile so the stems are cropped by it. Units are
 * the 100×100 icon grid unless stated otherwise; do not tweak these numbers by
 * eye, they are the spec.
 */

/**
 * User-visible product branding. Technical identifiers — package names, the
 * CLI binary, environment variables, config directories, and URLs — keep their
 * historical names so existing installs and deployments keep working.
 */
export const BRAND = {
  full: "易研",
  short: "易研",
} as const;

const r3 = (n: number) => Math.round(n * 1000) / 1000;

// ---------- icon (F2): YY, stroke 12, legs bleed off the bottom ----------
/**
 * Stroke of the YY mark, in tile units. The mark is drawn as one stroked
 * centre line so the static and "think" forms share the same geometry.
 */
export const ICON_STROKE = 12;
export const ICON_TILE_RADIUS = 22.5;

/**
 * The two Ys, in 100x100 tile units. The stems run past the tile's bottom edge
 * so the mark reads as too big for its tile and gets cropped.
 */
export const ICON_MARK_PATH =
  "M16 22L30 52L44 22M30 52V112M56 22L70 52L84 22M70 52V112";

/**
 * Places the mark on the tile. The circle (avatar) form shrinks it slightly so
 * the arms clear the curve.
 */
export function iconMarkTransform(shape: "square" | "circle" = "square"): string | undefined {
  return shape === "circle" ? "translate(5 5) scale(0.9)" : undefined;
}

/**
 * The thin edge in the tile colour that keeps the tile outline intact on any
 * background: 2.6% of the width, never thinner than 1px.
 */
export function iconEdge(sizePx: number): number {
  return Math.max(2.6, 100 / sizePx);
}

// ---------- wordmark (G2): stroke 16, arch 62, x-height 76, ascender 30 ----------
export const WORDMARK_STROKE = 16;

export interface WordmarkLetter {
  d: string;
  dot?: { cx: number; cy: number; r: number };
}

function buildWordmark() {
  const t = WORDMARK_STROKE;
  const W = 62;
  const X = 76;
  const asc = 30;
  const gap = 12;
  const r = (W - t) / 2;
  const a = t / 2;
  const cy1 = a + r;
  const cy2 = X - a - r;
  const c45 = Math.SQRT1_2;
  const A = (x1: number, y1: number) => `A${r3(r)} ${r3(r)} 0 0 1 ${r3(x1)} ${r3(y1)}`;
  const Ar = (x1: number, y1: number) => `A${r3(r)} ${r3(r)} 0 0 0 ${r3(x1)} ${r3(y1)}`;
  const glyphs: Record<string, [number, (x: number) => string, ((x: number) => WordmarkLetter["dot"])?]> = {
    m: [4 * r + t, (x) => `M${r3(x + a)} ${X}V${r3(cy1)}${A(x + a + 2 * r, cy1)}V${X}M${r3(x + a + 2 * r)} ${r3(cy1)}${A(x + a + 4 * r, cy1)}V${X}`],
    u: [2 * r + t, (x) => `M${r3(x + a)} 0V${r3(cy2)}${Ar(x + a + 2 * r, cy2)}M${r3(x + a + 2 * r)} 0V${X}`],
    l: [t, (x) => `M${r3(x + a)} ${-asc}V${X}`],
    t: [2.5 * t, (x) => `M${r3(x + 0.75 * t + a)} ${r3(-asc * 0.66)}V${X}M${r3(x)} ${r3(a)}H${r3(x + 2.5 * t)}`],
    i: [t, (x) => `M${r3(x + a)} 0V${X}`, (x) => ({ cx: r3(x + a), cy: r3(-asc * 0.62), r: r3(t * 0.56) })],
    c: [(W / 2) * (1 + c45), (x) => {
      const cx = x + W / 2;
      return `M${r3(cx + r * c45)} ${r3(cy1 - r * c45)}${Ar(cx - r, cy1)}V${r3(cy2)}${Ar(cx + r * c45, cy2 + r * c45)}`;
    }],
    a: [W, (x) => `M${r3(x + a)} ${r3(cy1)}${A(x + a + 2 * r, cy1)}V${r3(cy2)}${A(x + a, cy2)}ZM${r3(x + a + 2 * r)} 0V${X}`],
  };
  // Optical kerning: the four pairs that read loose at the default spacing.
  const kern: Record<string, number> = { lt: 0.8, ti: 0.85, ic: 0.92, ca: 0.8 };
  const word = "multica";
  const letters: WordmarkLetter[] = [];
  let x = 0;
  for (let k = 0; k < word.length; k++) {
    const [w, d, dot] = glyphs[word[k]!]!;
    letters.push({ d: d(x), dot: dot?.(x) });
    x += w;
    if (k < word.length - 1) x += gap * (kern[word[k]! + word[k + 1]!] ?? 1);
  }
  const top = -asc - 0.5;
  return { letters, viewBox: `0 ${r3(top)} ${r3(x)} ${r3(X - top)}` };
}

export const WORDMARK = buildWordmark();
