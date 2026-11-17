import { cn } from "../../lib/utils";
import { ICON_STROKE } from "../../lib/multica-brand";

const ARCH_STAGGER_S = 0.11;
const BLEED_HEIGHT = 140;
/** One Y of the mark: arms spread 14 either side and meet 30 below the top. */
const Y_SPREAD = 14;
const Y_JOIN = 30;
/** Centre-to-centre distance between neighbouring Ys, as in the icon. */
const Y_STEP = 40;
/** Room for the stroke and the mitred arm tips around the centre line. */
const Y_PAD = ICON_STROKE;

const BLEED_WIDTH = 2 * (Y_PAD + Y_SPREAD) + Y_STEP;

function yPath(cx: number, top: number, bottom: number): string {
  const join = top + Y_JOIN;
  return `M${cx - Y_SPREAD} ${top}L${cx} ${join}L${cx + Y_SPREAD} ${top}M${cx} ${join}V${bottom}`;
}

interface MulticaArcadeProps extends React.ComponentProps<"svg"> {
  /** Number of Ys; each one stands for a teammate. */
  count: number;
  /** Height of each Y in icon units (a Y is 28 wide). */
  legs?: number;
  /**
   * `center`: Ys rise from the middle outwards (backgrounds).
   * `join`: Ys rise left to right, and the last one rises after a beat
   * (someone just joined). Omit for a static row.
   */
  rise?: "center" | "join";
  /** Seconds before the first Y moves. */
  delay?: number;
  /** Classes for the last Y, e.g. to highlight the newest teammate. */
  lastClassName?: string;
}

/**
 * A row of Ys built from the icon's module — the graphic extension of the
 * mark. Decorative and low-contrast; colour follows `currentColor`.
 */
export function MulticaArcade({
  count,
  legs = 76,
  rise,
  delay = 0,
  lastClassName,
  className,
  ...props
}: MulticaArcadeProps) {
  const first = Y_PAD + Y_SPREAD;
  const width = Y_STEP * (count - 1) + 2 * first;
  const mid = (count - 1) / 2;
  const delayOf = (i: number) => {
    if (rise === "center") return delay + Math.abs(i - mid) * ARCH_STAGGER_S;
    if (i === count - 1) return delay + i * ARCH_STAGGER_S + 0.3;
    return delay + i * ARCH_STAGGER_S;
  };
  return (
    <svg
      viewBox={`0 0 ${width} ${legs}`}
      fill="none"
      stroke="currentColor"
      strokeWidth={ICON_STROKE}
      aria-hidden="true"
      className={cn("block", className)}
      {...props}
    >
      {Array.from({ length: count }, (_, i) => (
        <path
          key={i}
          d={yPath(first + Y_STEP * i, Y_PAD, legs)}
          className={cn(rise && "multica-arch-rise", i === count - 1 && lastClassName)}
          style={rise ? { animationDelay: `${delayOf(i)}s` } : undefined}
        />
      ))}
    </svg>
  );
}

interface MulticaBleedMarkProps extends React.ComponentProps<"svg"> {
  /** Rise into place once, after this many seconds. Omit for static. */
  riseDelay?: number;
}

/**
 * A large YY meant to bleed off the bottom and right of its container — the
 * icon's "too big to fit" idea as a surface graphic. Position it with
 * `className`; colour follows `currentColor`.
 */
export function MulticaBleedMark({ riseDelay, className, ...props }: MulticaBleedMarkProps) {
  return (
    <svg
      viewBox={`0 0 ${BLEED_WIDTH} ${BLEED_HEIGHT}`}
      fill="none"
      stroke="currentColor"
      strokeWidth={ICON_STROKE}
      aria-hidden="true"
      className={cn("block", className)}
      {...props}
    >
      <g
        className={riseDelay != null ? "multica-arch-rise" : undefined}
        style={riseDelay != null ? { animationDelay: `${riseDelay}s` } : undefined}
      >
        <path
          d={
            yPath(Y_PAD + Y_SPREAD, Y_PAD, BLEED_HEIGHT) +
            yPath(Y_PAD + Y_SPREAD + Y_STEP, Y_PAD, BLEED_HEIGHT)
          }
        />
      </g>
    </svg>
  );
}
