import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";

vi.mock("../workbench", () => ({
  WorkbenchPage: () => <p>conversation surface</p>,
}));
vi.mock("./home-tasks-page", () => ({
  HomeTasksPage: () => <p>tasks surface</p>,
}));
vi.mock("../office", () => ({
  OfficePage: () => <p>office surface</p>,
}));
vi.mock("../layout/account-menu", () => ({
  AccountMenu: () => <button type="button">account menu</button>,
}));

import { HomePage } from "./home-page";

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/home",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

describe("HomePage", () => {
  it("opens on the conversation and switches to the caller's tasks", async () => {
    renderWithI18n(
      <NavigationProvider value={adapter}>
        <HomePage />
      </NavigationProvider>,
    );

    expect(screen.getByText("conversation surface")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "account menu" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "My tasks" }));

    expect(screen.getByText("tasks surface")).toBeInTheDocument();
    expect(screen.queryByText("conversation surface")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Office" }));

    expect(screen.getByText("office surface")).toBeInTheDocument();
    expect(screen.queryByText("tasks surface")).not.toBeInTheDocument();
  });
});
