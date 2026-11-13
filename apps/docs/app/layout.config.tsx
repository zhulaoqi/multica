import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { MulticaWordmark } from "@multica/ui/components/common/multica-wordmark";

// This fork serves the docs internally on the app origin, so the upstream
// GitHub and marketing-site links are deliberately absent.
export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="inline-flex items-center gap-2 font-semibold text-base">
        <MulticaWordmark className="h-[15px]" />
        Docs
      </span>
    ),
  },
};
