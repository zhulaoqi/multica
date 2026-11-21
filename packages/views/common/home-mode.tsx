"use client";

import { createContext, useContext } from "react";
import { paths } from "@multica/core/paths";

/**
 * True while rendering the C-end (`/home`). The shared workbench also renders
 * inside the admin shell, where its links point at workspace routes; on the
 * C-end those routes belong to the admin side, so components use this flag to
 * keep navigation (and any other admin-only affordance) inside `/home`.
 */
const HomeModeContext = createContext(false);

export function HomeModeProvider({ children }: { children: React.ReactNode }) {
  return <HomeModeContext.Provider value>{children}</HomeModeContext.Provider>;
}

export function useHomeMode(): boolean {
  return useContext(HomeModeContext);
}

/** A C-end dispatch thread, or the C-end itself for a fresh composition. */
export function cendDispatchPath(issueId?: string): string {
  return issueId ? `${paths.home()}?dispatch=${issueId}` : paths.home();
}
