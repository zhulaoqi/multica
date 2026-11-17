/**
 * 易研 mark — YY. Vector copy of ICON_MARK_PATH in
 * packages/ui/lib/multica-brand.ts (mobile does not depend on @multica/ui) —
 * keep the two in sync.
 *
 * react-native-svg does not resolve CSS `currentColor`, so callers must pass
 * `color` explicitly. For theme-aware usage, pair with `useColorScheme` +
 * `THEME` token from `@/lib/theme`.
 */
import Svg, { Path } from "react-native-svg";
import { useLocale } from "@/lib/i18n/use-locale";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

/** Tile grid the mark is drawn in; the stems run past its bottom edge. */
const VIEW_BOX = { x: 0, y: 0, w: 100, h: 100 };

const MARK_PATH = "M16 22L30 52L44 22M30 52V112M56 22L70 52L84 22M70 52V112";
const MARK_STROKE = 12;

interface MulticaLogoProps {
  /** Rendered height in points; the mark is square. */
  size?: number;
  color?: string;
}

export function MulticaLogo({ size = 24, color }: MulticaLogoProps) {
  const { isDarkColorScheme } = useColorScheme();
  const locale = useLocale();
  const resolvedColor =
    color ?? (isDarkColorScheme ? THEME.dark.foreground : THEME.light.foreground);
  const label = locale.toLowerCase().startsWith("zh") ? "易研" : "yiyan";

  return (
    <Svg
      width={size}
      height={size}
      viewBox={`${VIEW_BOX.x} ${VIEW_BOX.y} ${VIEW_BOX.w} ${VIEW_BOX.h}`}
      accessibilityLabel={label}
    >
      <Path
        d={MARK_PATH}
        fill="none"
        stroke={resolvedColor}
        strokeWidth={MARK_STROKE}
      />
    </Svg>
  );
}
