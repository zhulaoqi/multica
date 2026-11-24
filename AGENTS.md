# Repository Instructions

Multica is a task management platform where people and agents collaborate on issues. These instructions apply to all coding agents working in this repository.

## 语言

所有对用户可见的交流都使用简体中文，包括解释、进度、报错、提问和总结。代码、标识符、提交说明和代码注释仍用英文。引用终端或接口原文时保留原文，再用中文说明含义。

## 双仓库

这个检出同时对应两个远程。日常开发使用 `release`。提交并推送后，GitHub 和 GitLab 的 `release` 都要有同一批提交。

| 远程 | 地址 |
| --- | --- |
| `origin` | `git@github.com:zhulaoqi/multica.git` |
| `gitlab` | `git@gitlab.yeahmobi.com:yeahcloudadrd/multica.git` |

- 缺少 `gitlab` 远程时执行 `git remote add gitlab git@gitlab.yeahmobi.com:yeahcloudadrd/multica.git`。不要改写已有的 `origin`。
- 新提交放在 `release`。用户要求提交并推送时，先推 `origin release`，再推 `gitlab release`。只提交不推送时，两个远程都不要推。
- GitLab 的 `main` 受保护。不要向 `main` 或 `master` 强制推送。
- 两个远程的 `release` 出现分叉时，先说明哪一边超前，再问用户怎么处理。不要自行覆盖另一边。

## Scope and Reading Order

- Before changing `apps/mobile/`, also read [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md), even if your tool does not load nested instructions automatically. Platform-specific sections below apply only to the named platform.
- For naming, translations, or Chinese UI/docs copy, read [conventions.mdx](apps/docs/content/docs/developers/conventions.mdx) and [conventions.zh.mdx](apps/docs/content/docs/developers/conventions.zh.mdx).
- Maintain shared rules here and mobile-specific rules in the mobile file. `CLAUDE.md` files only import them. Update instructions in the same change that alters the referenced workflow or boundary; do not add incident timelines, dependency version lists, or duplicate rules.

## Sharing Rules and Package Boundaries

| Location | Responsibility and constraints |
| --- | --- |
| `server/` | Go backend; Chi, sqlc, WebSocket |
| `packages/core/` | Headless logic, API client, Query hooks, shared Zustand stores. No UI libraries, `react-dom`, `localStorage`, or `process.env`; use `StorageAdapter` for persistence. |
| `packages/ui/` | UI primitives and shared styles. No business logic or `@multica/core` imports. |
| `packages/views/` | Shared web/desktop pages and business components. No store definitions, `next/*`, or `react-router-dom`; use `NavigationAdapter`, `useNavigation()`, and `<AppLink>`. |
| `apps/web/` | Next.js routes/layouts and web-only UI. Framework APIs stay here; shared navigation adapters live in `apps/web/platform/`. |
| `apps/desktop/` | Electron and desktop-only UI/state. Application navigation goes through `apps/desktop/src/renderer/src/platform/`. |
| `apps/mobile/` | Independent Expo/React Native client: owns UI, state, hooks, providers, i18n, build, and release. Shares core types and pure utilities, including platform-independent schemas. |
| `apps/docs/` | Fumadocs documentation site |

- Dependency direction is `views -> core + ui`; core and ui remain independent. Shared packages export raw TypeScript compiled by consuming apps.
- Extract logic used by both web and desktop into the appropriate shared package. Keep framework/Electron APIs in the app layer; inject platform-specific UI through props/slots.
- Wire shared features into both web routes and the desktop router or overlay. Reuse existing guards/providers such as `DashboardGuard` in `packages/views/layout/`.
- Each workspace declares its directly imported external dependencies. Use `catalog:` for shared dependencies; mobile pins Expo/React Native dependencies in its own manifest.

## Development and Verification

Use `Makefile`, workspace `package.json` files, and `pnpm-workspace.yaml` for current commands and versions. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and worktree operations.

- Use the checkout's managed environment: `make up`, `make status`, `make down`. `make down` preserves data; `make destroy` removes the environment and its data.
- Worktrees share PostgreSQL but have isolated databases/ports. Use the environment scripts and `.env.worktree`; do not copy the main checkout's `.env` or manually create a database through an assumed PostgreSQL instance.
- Regenerate sqlc with `make sqlc` after SQL changes.
- Run the narrowest useful checks while iterating, then broaden when risk warrants it. Report what actually ran and any skipped checks.

Run these from the repository root:

| Scope | Checks |
| --- | --- |
| Frontend excluding mobile | `pnpm typecheck`, `pnpm lint`, `pnpm test` |
| Go backend | `make test` |
| End-to-end | `pnpm exec playwright test` |
| Combined web/backend verification | `make check` |
| Mobile | Commands in [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md#verification) |

Docs-only changes can use link/reference checks and `git diff --check`; state that code tests were not run.

## State Rules

- TanStack Query owns API/server data. Zustand owns client state such as filters, drafts, modals, and tab layout; persist only durable preferences/drafts/layout, not server data or ephemeral UI state.
- Web/desktop shared stores live in `packages/core/`. Desktop platform stores remain in desktop; mobile stores remain in mobile. Do not define stores in `packages/views/`.
- On web/desktop, workspace identity is route-driven; platform mirrors exist only for request headers, storage namespaces, and reconnects. React Context is for platform plumbing, not a second server-state store.
- Among stores, only auth/workspace stores may call `api.*` directly; other server interactions belong in queries/mutations.
- Workspace-scoped query keys include `wsId`; account-level keys remain account-scoped. Hooks needing workspace context accept `wsId` unless guaranteed to run under its provider.
- Zustand selectors return stable references; use shallow comparison for allocated objects/arrays.
- WebSocket events patch or invalidate Query caches, not server payloads in Zustand. Clearing client-owned pointers is allowed with one responder and a self-initiated guard when this client can cause the event.
- Optimistic field patches require a predictable result, rare failure, trivial rollback, and staying on the current screen. Snapshot before patching, roll back on failure, and invalidate uncertain projections on settle.
- Create/delete/leave and confirmation flows await the server before navigation or cleanup; do not optimistically delete entities. Exceptions: workspace leave (known debt under Desktop Rules) and mobile inbox mark-read (mobile instructions).
- Message sends use visible pending state and retry on failure.

## API Compatibility

Installed desktop clients may talk to newer backends. Preserve response compatibility at the API boundary.

- UI-consumed JSON passes through a zod schema and `parseWithFallback`, not an `as T` cast. Web/desktop use `packages/core/api/schema.ts`; mobile uses its own request helpers.
- Provide defaults for optional fields and fallbacks for unknown server enums. Prefer explicit boolean checks; avoid tying critical affordances to a single backend flag when other contract signals are available.
- When adding/changing an endpoint, update its schema and malformed-response tests.

## Database and Migration Rules

- Schema changes are applied by a DBA, not by the API process. Container startup only connects to the database. Do not put `migrate up` back into `docker/agent-api/start.sh` or `docker/entrypoint.sh`.
- The baseline for an empty database is `deploy/dba/baseline/multica-schema.sql`. Run it once. Do not edit it for later changes, and do not run it again on a database that already has tables.
- Each later change gets a new file in `deploy/dba/changes/`, named `NNN_short_description.sql`, containing only that change. Hand the file to the DBA. Do not rely on startup to apply it.
- Do not add foreign keys, cascading deletes, or cascading updates. Validate relationships and clean up dependents in application code, using a transaction when the operation must be atomic.
- Every migration-created index, including indexes on new tables, uses `CREATE [UNIQUE] INDEX CONCURRENTLY`. Each concurrent index build gets its own single-statement migration file; the runner executes files outside an explicit transaction.
- Conditionally skipped migrations are still recorded in `schema_migrations`. Later DDL touching conditional objects must be idempotent (`IF EXISTS` / `IF NOT EXISTS`); document recovery if the missing object would break runtime behavior.

## Backend UUID Rules

In `server/internal/handler/`, distinguish UUID sources before using them in writes:

- UUID-or-human-readable resource params: resolve with loaders such as `loadIssueForUser`, `loadSkillForUser`, `loadAgentForUser`, or `requireDaemonRuntimeAccess`, then write using the resolved `entity.ID`.
- Pure UUID request input: `parseUUIDOrBadRequest(w, s, fieldName)`; return immediately when `ok=false`.
- Trusted sqlc/test-fixture round-trips: `parseUUID(s)`, which panics on invalid input.
- Outside handlers: `util.ParseUUID(s)` and check the error.

Workspace-scoped queries filter by `workspace_id`; membership gates access and `X-Workspace-ID` selects the workspace. Assignees are polymorphic: interpret `assignee_id` together with `assignee_type`.

## Workspace Department Scope

There is one organization workspace (`workspace.is_organization`, at most one, `workspace_organization_uidx`). It owns the only department tree (`org_unit`) and the company roster, both written by Feishu directory sync. Every other workspace is a project owned by one department (`workspace.org_unit_id`, an organization unit id; NULL for legacy flat workspaces).

- Every authorization check on an owned workspace resolves through `server/internal/workspaceaccess` (`Resolve`, `Allowed`, `IsManager`). Do not reintroduce `GetMemberByUserAndWorkspace` as an access check on those paths.
- Access = an explicit member row (wherever the person is placed, so a project manager can pull in colleagues from other departments) OR leading the owning department or one of its ancestors. Descendant departments inherit nothing. `IsManager` (owner/admin or owning-department lead) gates settings and member management; integrations, plugins, MCP, runtimes, share links and billing stay on the stored role. An ancestor lead without a member row gets `Access.Implicit()` and a synthetic `EffectiveMember` identity, never a seat or management rights.
- Unit ids are global: project `workspace.org_unit_id` and `member.org_unit_id` point into the organization tree, and access CTEs join units by id only. Projects have no tree of their own; their org-page endpoints return 409 `organization_managed`.
- A project `member` row means "joined the project" and owns seats, notifications, presence and rosters. Organization member rows come from sync and are the company directory: they write no seat intent, and the organization disables `@all`, member-wide notifications, invitations and share links. Project member candidates are read from the organization roster; `workspace_directory_member` is retired.
- Organization issues are visible by department (`workspaceaccess.ResolveIssueScope`): managers, creator, assignee or their agent's owner, subscribers, members of the issue's `org_unit_id` subtree, and leads of that unit or its ancestors. List, detail, search and realtime fan-out all apply it; hidden issues return 404.
- New member/organization queries must follow content access: explicit member rows without a subtree filter, plus ancestor leads through the recursive `ancestors` CTE. The owning department's lead always has a real member row and owns the project; a lead is removed only after transferring the department lead.
- Dispatch and reassign run only in the organization workspace (409 `dispatch_organization_only` elsewhere); root issues carry the target `org_unit_id` and sub-issues inherit it.
- Who may hand a department work lives in `org_unit_intake_source` and is edited on the organization's org page (`PUT /api/org/units/{id}/intake-sources`; projects answer 409 `organization_managed`). Its leads, and the leads of its ancestors, always may; each row adds one person or one department whose whole subtree may request. A department with no rows accepts only its leads' requests. Web, workbench routing and Feishu dispatch all apply this rule before the primary-agent binding check, so a hidden or unconfigured department answers 404 `org_dispatch_unit_not_found`.
- A project may tighten who hands it work with its own list in `workspace_intake_source`, edited in the project's settings (`PUT /api/project-intake/sources`). An empty list inherits the owning department's rule; a non-empty list is the only rule. The C-end composer only offers projects from `GET /api/org/request-projects`, and dispatch accepts an explicit `project_id`; an unauthorized or unknown project answers 404 `org_dispatch_unit_not_found` so projects cannot be enumerated.
- A dispatch routed to a project is created in that project's workspace and run by one of its own agents (`pickProjectExecutor`, preferring the owning department lead's primary twin), not in the department queue. Routing treats a named project as authoritative: exact project name beats exact department, containment prefers the longest match and gives ties to the project, and the models run only when no name matched: the decision model (`MULTICA_ROUTE_DECISION_MODEL`, `llm.Decider`) first, acted on at confidence ≥ 0.5, then the chat LLM. `dispatch_origin` stays in the organization workspace (`target_unit_id`, `target_project_id`, `target_squad_id` NULL); the first run is a leader task with no squad. No online project agent answers 409 `org_dispatch_project_agent_offline`. The requester follows it from `/home` through `GET /api/org/dispatches/{issueId}/trail` (root issue, child statuses, final agent answer, produced files — never internal comments) and controls it with `POST .../messages` and `POST .../status`; project dispatches are not reassignable. Requester attachments are rehomed into the project before creation.
- Existing workspaces are converted with `server/cmd/reset_project_members` and `server/cmd/consolidate_org`, never by deleting member rows or units in SQL.

## Digital Twin

A member's agent is their digital twin: it represents them when they dispatch work and receives the department's follow-ups.

- Agents are created and edited in the organization workspace. A project holds read-only projections (`agent.source_agent_id`) that `syncProjectionsForSource` refreshes from the source; their runtime maps to the project runtime on the same daemon and provider (or profile), else NULL. Write endpoints on a projection return 409 `agent_managed_by_organization`, and projects cannot create native user agents. Project managers add projections of members' organization agents with `POST /api/agents/projections`.
- Each member has at most one primary agent per workspace (`agent.is_primary`, unique partial index `agent_primary_twin_uidx`). Creating the first active owned agent promotes it; archiving or restoring the primary promotes the owner's next active agent through `EnsurePrimaryAgentForOwner`. `PUT /api/agents/{id}/primary` switches it. A projection's `is_primary` follows its source.
- Dispatching and submitting intake require a primary agent. Unbound members may browse and review but get `agent_binding_required` (403). Apply the gate after unit visibility checks so a hidden unit still returns 404.
- Assigning an issue (or a workflow step) to a member also requires their primary agent; `validateAssigneePair` returns 403 otherwise. The web/desktop shell reminds unbound members once per workspace per session (`AgentBindingReminder`).
- A primary twin is open to everyone with workspace access on collaboration paths, whatever its `permission_mode`: dispatch leader picks, assignment, @mentions, comment routing, squad delegation and reruns (backend `canCollaborateWithAgent` / `memberMayCollaborateWithAgent`, frontend `canAssignAgentToIssue`). It is also visible to them. Private chat, channel bots, autopilots, issue wakeups, squad wiring and quick actions keep `permission_mode` (`canInvokeAgent`, frontend `canChatWithAgent`), as do non-primary agents. An agent or system actor gets the opening only through a human originator.
- Joining a project (create, direct add, invitation, share link) projects the member's organization primary twin via `ensurePrimaryTwinInWorkspace`; org sync mass-adds do not. Skills are per workspace and do not travel with projections.
- Frontend resolves the twin with `findPrimaryTwin` / `ownedTwins` from `@multica/core/agents`; do not re-implement the primary-first ordering.

## Desktop Rules

- Workspace session routes are tab destinations. Pre-workspace one-shot flows (create workspace, accept invite) use `WindowOverlay` in `apps/desktop/src/renderer/src/stores/window-overlay-store.ts`, not new routes. Stale workspace tabs heal by dropping stale tab groups.
- Workspace route layouts own `setCurrentWorkspace(slug, uuid)` from `@multica/core/platform`; leaving workspace context calls `setCurrentWorkspace(null, null)`.
- Cross-workspace navigation uses the adapter's `switchWorkspace(slug, targetPath)` flow; do not bypass it with direct router navigation.
- Workspace leave clears/navigates first to avoid the `member:removed` race; this is known debt in `packages/views/settings/components/workspace-tab.tsx`, not a pattern for new flows.
- Full-window views outside the dashboard shell mount `<DragStrip />` from `@multica/views/platform` as the first flex child. Interactive controls in the top 48px need `WebkitAppRegion: "no-drag"`.

## UI Copy

- Descriptions are optional and omitted by default. Do not restate titles, labels, values, statuses, or button actions; add help only for a non-obvious choice, constraint, consequence, or next step. A description prop is not a requirement to write a paragraph.
- Before adding or reviewing copy, follow "UI descriptions" in [conventions.mdx](apps/docs/content/docs/developers/conventions.mdx), and review all supported translations, including mobile's independent copy.

## Web/Desktop UI Rules

- For Button and Dialog usage, read `packages/ui/docs/button.md` and `packages/ui/docs/dialog.md`.
- Prefer existing shadcn/Base UI primitives. Add components with `pnpm ui:add <component>`.
- For `pnpm ui:add @reui/<name>`, decline overwrite prompts. Keep `REUI_LICENSE_KEY` in the environment, never in repo files. Adapt vendored primitives into `packages/ui/components/ui/` and compositions into `packages/views/`.
- Import icons from `@multica/ui/icons`; no icon library is installed. If the icon you need is missing, draw it by following `.agents/skills/multica-icons/SKILL.md`. Do not substitute an inline SVG or an emoji. Rewrite icon imports in `ui:add` output before committing.
- Use shared semantic tokens in `packages/ui/styles/`. Typography uses the role-named `--text-*` scale in `packages/ui/styles/tokens.css`, not Tailwind's default size ramp.
- Selected states remain identifiable on hover. Handle overflow, long text, and scrolling deliberately; avoid unnecessary local state and dividers.

## Testing

- Tests live beside their implementation: shared logic in core, shared components in views, platform wiring in apps, E2E in `e2e/`, Go tests in server. Do not test shared behavior in app suites.
- Give each behavior one canonical test layer: helper tests own parsing/state matrices; component tests cover wiring, accessibility, happy paths, and named regressions. Prefer a failing regression test before behavioral fixes.
- DOM-free `.test.ts` files start with `// @vitest-environment node`; do not use it if it would silently switch the code under test to an SSR path.
- Every workspace with jsdom suites loads `@multica/vitest-setup/web-storage` in its Vitest setup; do not add per-package `localStorage` shims.
- Views tests must not mock `next/*` or `react-router-dom`. Mock stores with their Zustand callable shape plus `getState`; mock API calls at `@multica/core/api`.
- E2E setup/teardown uses `TestApiClient`.
- DB-backed Go tests use `server/internal/testutil` fixtures (`dbfx.Issue`, `dbfx.Task`, `dbfx.Insert`) and `testutil.Call(h, req).Want(status).JSON(&out)`. Keep product assertions and case-specific diagnostics in the test, not fixture helpers.
- Default tests must not resolve or execute user-installed agent CLIs; pass test-created fake or missing executable paths. New default agent commands go in `scripts/agent-cli-command-names.txt`.
- Only run real-agent smoke tests when explicitly authorized. Gate them behind `agentintegration` and check `MULTICA_RUN_REAL_AGENT_SMOKE=1` before executable lookup/account access. Run the specific test: `(cd server && MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent -run '<test-name>' -count=1 -v)`.

## Change and Delivery Rules

- Keep changes scoped; reuse existing patterns.
- Before adding complexity for a speculative or vanishingly unlikely race or security edge case, explain the concrete failure mode, likelihood, and complexity cost to the user and get their buy-in. Do not silently expand scope for theoretical robustness.
- Do not add internal compatibility shims, dual writes, fallback paths, or legacy adapters unless requested. This does not relax API response compatibility above.
- Use names, types, and tests to express behavior. Code comments are English and kept for non-obvious rationale and constraints; remove superseded plans and incident narratives when updating code. Preserve tool directives and mixed-version contracts.
- New global pre-workspace routes use a single word or `/{noun}/{verb}`, not hyphenated root names. Update `server/internal/handler/reserved_slugs.json`, run `pnpm generate:reserved-slugs`, and commit `packages/core/paths/reserved-slugs.ts` when changing reserved slugs.
- Use atomic conventional commits and the repository PR template. For releases, follow [.github/RELEASING.md](.github/RELEASING.md); default to a patch bump unless specified otherwise.
