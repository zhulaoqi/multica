import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LoginBackdrop } from "./login-backdrop";

describe("LoginBackdrop", () => {
  it("mounts a canvas and survives an environment without WebGL", async () => {
    render(<LoginBackdrop />);
    const backdrop = screen.getByTestId("login-backdrop");
    const canvas = backdrop.querySelector("canvas");
    expect(canvas).not.toBeNull();

    // The scene loads through a dynamic import; jsdom has no WebGL context, so
    // the backdrop must quietly keep the poster image underneath instead of
    // throwing.
    await waitFor(() => expect(canvas?.isConnected).toBe(true));
    expect(canvas).toHaveAttribute("aria-hidden", "true");
  });
});
