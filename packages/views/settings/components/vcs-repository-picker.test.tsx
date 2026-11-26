import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const query = vi.hoisted(() => ({
  data: {
    pages: [
      {
        repositories: [
          {
            id: 1,
            full_name: "acme/existing",
            description: "Already connected",
            clone_url: "https://gitlab.example.com/acme/existing.git",
            html_url: "https://gitlab.example.com/acme/existing",
            private: true,
            archived: false,
            default_branch: "main",
          },
          {
            id: 2,
            full_name: "acme/app",
            description: "Application",
            clone_url: "https://gitlab.example.com/acme/app.git",
            html_url: "https://gitlab.example.com/acme/app",
            private: false,
            archived: false,
            default_branch: "main",
          },
          {
            id: 3,
            full_name: "acme/archive",
            description: "",
            clone_url: "https://gitlab.example.com/acme/archive.git",
            html_url: "https://gitlab.example.com/acme/archive",
            private: false,
            archived: true,
            default_branch: "main",
          },
        ],
        next_page: 2,
      },
    ],
  },
  isPending: false,
  isError: false,
  hasNextPage: true,
  isFetchingNextPage: false,
  fetchNextPage: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useInfiniteQuery: () => query,
  infiniteQueryOptions: <T,>(options: T) => options,
}));

vi.mock("@multica/core/vcs/queries", () => ({
  vcsConnectionRepositoriesOptions: vi.fn(() => ({})),
}));

import { VCSRepositoryPicker } from "./vcs-repository-picker";

const connections = [
  {
    id: "vcs-1",
    provider: "gitlab" as const,
    instance_url: "https://gitlab.example.com",
    account_login: "bot",
  },
];

beforeEach(() => {
  vi.clearAllMocks();
});

describe("VCSRepositoryPicker", () => {
  it("disables existing and archived repositories, then imports the selection", async () => {
    const onImport = vi.fn().mockResolvedValue(true);
    const user = userEvent.setup();
    renderWithI18n(
      <VCSRepositoryPicker
        open
        workspaceId="ws-1"
        connections={connections}
        initialConnectionId="vcs-1"
        existingUrls={["git@gitlab.example.com:acme/existing.git"]}
        onOpenChange={vi.fn()}
        onImport={onImport}
      />,
    );

    expect(
      await screen.findByRole("heading", { name: "Choose GitLab repositories" }),
    ).toBeInTheDocument();
    const checkboxes = screen.getAllByRole("checkbox");
    expect(checkboxes[0]).toHaveAttribute("aria-disabled", "true");
    expect(checkboxes[2]).toHaveAttribute("aria-disabled", "true");

    await user.click(checkboxes[1]!);
    await user.click(screen.getByRole("button", { name: "Add repositories" }));

    expect(onImport).toHaveBeenCalledWith([
      {
        url: "https://gitlab.example.com/acme/app.git",
        description: "Application",
      },
    ]);
  });

  it("loads another page from GitLab", async () => {
    const user = userEvent.setup();
    renderWithI18n(
      <VCSRepositoryPicker
        open
        workspaceId="ws-1"
        connections={connections}
        initialConnectionId="vcs-1"
        existingUrls={[]}
        onOpenChange={vi.fn()}
        onImport={vi.fn()}
      />,
    );

    await user.click(await screen.findByRole("button", { name: "Load more" }));
    expect(query.fetchNextPage).toHaveBeenCalled();
  });
});
