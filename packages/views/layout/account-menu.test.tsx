import { cloneElement, type ReactElement, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { renderWithI18n } from "../test/i18n";
import { AccountMenu } from "./account-menu";

const state = vi.hoisted(() => ({
  workspaces: [] as {
    id: string;
    slug: string;
    is_organization?: boolean;
    viewer_can_manage?: boolean;
  }[],
}));

vi.mock("../navigation", () => ({
  AppLink: ({
    children,
    href,
    newTabTitle: _newTabTitle,
    ...props
  }: { children: ReactNode; href: string; newTabTitle?: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));

vi.mock("../common/use-language-switch", () => ({
  useLanguageSwitch: () => ({
    currentLocale: "en",
    options: [{ value: "en", label: "English" }],
    changeLanguage: vi.fn(),
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) =>
    selector({
      user: {
        name: "Ada",
        email: "ada@example.com",
        avatar_url: null,
        is_operator: false,
      },
    }),
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url?: string | null) => url ?? null,
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => <span />,
}));

vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: "system", setTheme: vi.fn() }),
}));

// Flatten the Base UI primitives so the menu content is always rendered; the
// only contract this suite needs is which anchors the menu offers.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const Pass = ({ children }: { children?: ReactNode }) => <>{children}</>;
  return {
    DropdownMenu: Pass,
    DropdownMenuContent: Pass,
    DropdownMenuGroup: Pass,
    DropdownMenuRadioGroup: Pass,
    DropdownMenuRadioItem: Pass,
    DropdownMenuSeparator: () => null,
    DropdownMenuSub: Pass,
    DropdownMenuSubContent: Pass,
    DropdownMenuSubTrigger: Pass,
    DropdownMenuLabel: Pass,
    DropdownMenuTrigger: Pass,
    DropdownMenuItem: ({
      children,
      render,
    }: {
      children: ReactNode;
      render?: ReactElement;
    }) => (render ? cloneElement(render, undefined, children) : <>{children}</>),
  };
});

function renderMenu(variant: "workspace" | "home") {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(workspaceListOptions().queryKey, state.workspaces as never);
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <AccountMenu variant={variant} />
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.workspaces = [];
  delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
});

afterEach(() => {
  delete (window as unknown as { desktopAPI?: unknown }).desktopAPI;
});

describe("AccountMenu", () => {
  it("offers the C-end from a web admin shell", () => {
    renderMenu("workspace");
    expect(screen.getByRole("link", { name: "Personal space" })).toHaveAttribute("href", "/home");
    expect(screen.queryByRole("link", { name: "Admin" })).toBeNull();
  });

  it("offers no C-end in the desktop shell, which has none", () => {
    (window as unknown as { desktopAPI?: unknown }).desktopAPI = {
      pickDirectory: () => undefined,
    };
    renderMenu("workspace");
    expect(screen.queryByRole("link", { name: "Personal space" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Admin" })).toBeNull();
  });

  it("offers the organization's admin side from the C-end", () => {
    state.workspaces = [
      { id: "org", slug: "org", is_organization: true, viewer_can_manage: true },
    ];
    renderMenu("home");
    expect(screen.getByRole("link", { name: "Admin" })).toHaveAttribute("href", "/org/org");
    expect(screen.queryByRole("link", { name: "Personal space" })).toBeNull();
  });

  it("hides the admin side from C-end users who manage nothing", () => {
    state.workspaces = [{ id: "org", slug: "org", is_organization: true, viewer_can_manage: false }];
    renderMenu("home");
    expect(screen.queryByRole("link", { name: "Admin" })).toBeNull();
  });
});
