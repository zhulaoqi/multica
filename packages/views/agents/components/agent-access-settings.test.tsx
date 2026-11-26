// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, MemberWithUser } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import enIssues from "../../locales/en/issues.json";
import { AgentAccessSettings } from "./agent-access-settings";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../../navigation/app-link", () => ({
  AppLink: ({
    href,
    children,
  }: {
    href: string;
    children: React.ReactNode;
  }) => <a href={href}>{children}</a>,
}));

const resources = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

it("keeps an organization-managed projection read-only for its owner", () => {
  const agent = {
    id: "projection-1",
    owner_id: "user-1",
    source_agent_id: "source-1",
    permission_mode: "private",
    invocation_targets: [],
    visibility: "private",
    composio_toolkit_allowlist: [],
    is_primary: true,
  } as unknown as Agent;

  render(
    <I18nProvider locale="en" resources={resources}>
      <AgentAccessSettings
        agent={agent}
        members={[] as MemberWithUser[]}
        currentUserId="user-1"
        sourceHref="/org/agents/source-1"
        onUpdate={vi.fn()}
      />
    </I18nProvider>,
  );

  expect(screen.getByTestId("access-readonly")).toBeInTheDocument();
  expect(
    screen.getByText(
      "Managed by the organization. Change access there; updates sync to this project.",
    ),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "Open in organization" }),
  ).toHaveAttribute("href", "/org/agents/source-1");
  expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
});
