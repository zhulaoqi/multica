import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { CliInstallCommand, cliInstallCommands } from "./cli-install-command";

const COMMANDS = cliInstallCommands(window.location.origin);

const LABELS = {
  group: "Choose your platform",
  macosLinux: "macOS / Linux",
  windows: "Windows",
};

function renderSwitch() {
  return render(
    <CliInstallCommand labels={LABELS}>
      {(command) => <code>{command}</code>}
    </CliInstallCommand>,
  );
}

describe("cliInstallCommands", () => {
  it("installs from the deployment origin, not a public code host", () => {
    expect(cliInstallCommands("https://multica.internal")).toEqual({
      macosLinux:
        "curl -fsSL https://multica.internal/install/install.sh | bash",
      windows: "irm https://multica.internal/install/install.ps1 | iex",
    });
  });

  it("tolerates a trailing slash on the origin", () => {
    expect(cliInstallCommands("https://multica.internal/").macosLinux).toBe(
      "curl -fsSL https://multica.internal/install/install.sh | bash",
    );
  });
});

describe("CliInstallCommand", () => {
  it("defaults to the macOS / Linux installer", () => {
    renderSwitch();

    expect(
      screen.getByRole("tablist", { name: LABELS.group }),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "macOS / Linux" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(
      screen.getByText(COMMANDS.macosLinux),
    ).toBeInTheDocument();
    expect(screen.queryByText(COMMANDS.windows)).toBeNull();
  });

  it("hands the Windows command to the row when that tab is picked", async () => {
    const user = userEvent.setup();
    renderSwitch();

    await user.click(screen.getByRole("tab", { name: "Windows" }));

    expect(screen.getByRole("tab", { name: "Windows" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText(COMMANDS.windows)).toBeInTheDocument();
    expect(screen.queryByText(COMMANDS.macosLinux)).toBeNull();
  });

  // A real tablist: arrow keys move between platforms without a click.
  it("switches platform with the arrow keys", async () => {
    const user = userEvent.setup();
    renderSwitch();

    await user.click(screen.getByRole("tab", { name: "macOS / Linux" }));
    await user.keyboard("{ArrowRight}");

    expect(screen.getByText(COMMANDS.windows)).toBeInTheDocument();
  });
});
