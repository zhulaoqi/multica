"use client";

import { useEffect, useRef } from "react";

/**
 * WebGL backdrop for the sign-in screens: a container terminal at first
 * light. Procedural dawn sky, dark harbor water, stacks of containers,
 * gantry cranes, sodium lamps cutting through drifting rain, and a film
 * grain + vignette composite. three.js is imported lazily so the login
 * bundle stays small and nothing runs during SSR; if WebGL is not
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
        const [THREE, scene] = await Promise.all([
          import("three"),
          import("./login-backdrop-scene"),
        ]);
        if (disposed) return;
        cleanup = scene.startPortScene(THREE, canvas);
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
