import { cn } from "../../lib/utils";
import { BRAND } from "../../lib/multica-brand";

interface MulticaWordmarkProps extends React.ComponentProps<"span"> {
  /** Visible and accessible name. Defaults to the short product name. */
  title?: string;
}

/**
 * The product wordmark. The drawn Latin wordmark no longer spells the product
 * name, so the mark is set as type; size it with the `text-*` scale.
 */
export function MulticaWordmark({
  title = BRAND.short,
  className,
  ...props
}: MulticaWordmarkProps) {
  return (
    <span
      role="img"
      aria-label={title}
      className={cn(
        "inline-flex items-center font-semibold leading-none tracking-tight",
        className,
      )}
      {...props}
    >
      {title}
    </span>
  );
}
