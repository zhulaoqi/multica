# Agent Intake Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the phase-1 intake product: a colleague signs in with Feishu, writes a few sentences, and the server turns them into one ordered task on one module's agent, one branch, and one deploy, then reports progress until acceptance.

**Identity:** Feishu login stores name, email, avatar, open_id, and union_id on `user`. Only the signed-in person's own menu receives id and name. Email, avatar URL, and Feishu ids are not returned on intake lists or cards. Progress is stored on `intake_request`, not on issue comments. Whether the comment system stays is undecided and is out of phase 1.

**Architecture:** Keep the existing Go API, PostgreSQL, sqlc, daemon queue, and issue model. Add intake tables and a single in-process `server/pkg/llm` call that returns one module plus an ordered task list. The web client is a chat at `/{workspaceSlug}/intake`; requesters never see the executor. Management of modules, members, and the test environment stays on a separate mode for leads, owners, and admins.

**Tech Stack:** Go 1.26, Chi, pgx, sqlc, PostgreSQL 17, Redis 7.2, Next.js, TanStack Query, zod, existing Feishu interactive cards.

**Spec:** [docs/engineering/agent-intake/overview.md](../../engineering/agent-intake/overview.md) and [docs/engineering/agent-intake/phase-1/design.md](../../engineering/agent-intake/phase-1/design.md).

Work on the current branch of this checkout. Do not open a worktree. Push `origin` and `gitlab` only when the user asks for a push.

---

## File structure

| File | Responsibility |
| --- | --- |
| `server/migrations/614_intake_agent_identity.up.sql` | `agent.intake_can`, `agent.intake_cannot` |
| `server/migrations/615_intake_module.up.sql` | module table, no indexes |
| `server/migrations/616_intake_module_uidx.up.sql` | unique `(workspace_id, project_id, product, module)` |
| `server/migrations/617_intake_module_position_idx.up.sql` | list index |
| `server/migrations/618_intake_project_member.up.sql` | membership table |
| `server/migrations/619_intake_project_member_uidx.up.sql` | unique member |
| `server/migrations/620_intake_project_member_user_idx.up.sql` | projects for one user |
| `server/migrations/621_intake_request.up.sql` | one row per submitted utterance |
| `server/migrations/622_intake_request_uidx.up.sql` | unique issue and client request |
| `server/migrations/623_intake_request_list_idx.up.sql` | list index |
| `server/migrations/624_intake_request_scheduled_idx.up.sql` | future `start_at` index |
| `server/migrations/625_intake_blocker.up.sql` | blocker sentences |
| `server/migrations/626_intake_blocker_open_uidx.up.sql` | one open blocker per issue |
| `server/pkg/db/queries/intake.sql` | all intake SQL |
| `server/internal/service/intake_split.go` | model output to one module and ordered tasks |
| `server/internal/service/intake_submit.go` | create one issue and enqueue or wake |
| `server/internal/service/intake_decision.go` | continue, stop, accept, cancel, resume |
| `server/internal/service/intake_progress.go` | day and night progress claim |
| `server/internal/handler/intake.go` | HTTP handlers |
| `server/internal/integrations/lark/intake_card.go` | personal cards, no executor name for the requester |
| `packages/core/api/schema.ts` | zod schemas |
| `packages/core/api/intake.ts` | client methods |
| `packages/views/intake/intake-page.tsx` | chat |
| `packages/views/intake/intake-manage-page.tsx` | capability map, members, test URL |
| `apps/web/app/[workspaceSlug]/(dashboard)/intake/page.tsx` | route wiring |

Indexes that use `CONCURRENTLY` each live in their own migration file. No foreign keys.

## Chunk 1: Schema

### Task 1: Agent identity columns

**Files:**
- Create: `server/migrations/614_intake_agent_identity.up.sql`
- Create: `server/migrations/614_intake_agent_identity.down.sql`
- Test: `server/internal/handler/intake_agent_test.go`

- [ ] **Step 1: Write the migration**

```sql
ALTER TABLE agent ADD COLUMN IF NOT EXISTS intake_can TEXT;
ALTER TABLE agent ADD COLUMN IF NOT EXISTS intake_cannot TEXT;
```

Down migration drops both columns with `IF EXISTS`.

- [ ] **Step 2: Write a failing handler test**

`POST /api/intake/agents` with an empty `can` returns 400 and inserts no agent. A body `{name, can, cannot, runtime_mode}` creates an agent owned by the caller. `GET /api/intake/agents` returns only that caller's agents and includes online state.

- [ ] **Step 3: Run the test and confirm it fails**

Run: `cd server && go test ./internal/handler -run 'TestIntakeAgent' -count=1`
Expected: FAIL because the route does not exist.

- [ ] **Step 4: Implement the short register path**

Add `intake_can` and `intake_cannot` to the agent queries. `POST` and `PATCH` accept only `name`, `can`, `cannot`, and `runtime_mode`. Reject blank `can`. Do not write skills, model, or instructions from this route.

- [ ] **Step 5: Run the test and confirm it passes**

Run: `cd server && go test ./internal/handler -run 'TestIntakeAgent' -count=1`
Expected: PASS

### Task 2: Module, member, request, and blocker tables

**Files:**
- Create: `server/migrations/615_intake_module.up.sql` through `server/migrations/626_intake_blocker_open_uidx.up.sql`
- Modify: `server/pkg/db/queries/intake.sql`

- [ ] **Step 1: Create tables without foreign keys**

`intake_module` columns: `id`, `workspace_id`, `project_id`, `product`, `module`, `scope`, `agent_id`, `backup_agent_id`, `position`, `created_at`, `updated_at`.

`intake_project_member` columns: `id`, `workspace_id`, `project_id`, `user_id`, `created_at`.

`intake_request` columns: `id`, `workspace_id`, `issue_id`, `project_id`, `module_id`, `requester_id`, `client_request_id`, `source_body`, `start_at`, `last_user_notice_at`, `created_at`.

`intake_blocker` columns: `id`, `workspace_id`, `issue_id`, `task_id`, `agent_id`, `done_text`, `missing_text`, `consequence_text`, `state`, `decision_comment_id`, `decided_by`, `created_at`, `decided_at`. `state` is `open`, `continued`, or `stopped`.

- [ ] **Step 2: Put each index in its own file**

```sql
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_module_product_uidx
  ON intake_module (workspace_id, project_id, product, module);
```

```sql
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_request_client_uidx
  ON intake_request (workspace_id, requester_id, client_request_id);
```

```sql
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS intake_blocker_open_uidx
  ON intake_blocker (issue_id) WHERE state = 'open';
```

Also add the position, user, list, and scheduled partial indexes from the spec.

- [ ] **Step 3: Regenerate sqlc and compile**

Run: `make sqlc && cd server && go test ./pkg/db -count=1`
Expected: PASS

## Chunk 2: Assignment and one execution

### Task 3: Split one utterance into one module

**Files:**
- Create: `server/internal/service/intake_split.go`
- Test: `server/internal/service/intake_split_test.go`

- [ ] **Step 1: Write the failing parser tests**

The model reply is JSON: `{ "module_id": "uuid", "tasks": ["first", "second"] }`. Reject a missing module, an empty task, a module outside the caller's projects, and a module with no primary. A reply that names two modules keeps only the declared `module_id`; extra module ids do not create issues. A disabled or failed model call returns `intake_split_empty` and creates nothing.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `cd server && go test ./internal/service -run 'TestIntakeSplit' -count=1`
Expected: FAIL

- [ ] **Step 3: Implement the call**

Send only `module_id`, product, module, and scope for projects where the caller is a member or the lead, and the project has `lead_type = member`. Do not send agent names. Require `MULTICA_LLM_API_KEY` or `MULTICA_LLM_BASE_URL`; otherwise return `intake_split_empty` before any HTTP call. Record the new caller in the `server/pkg/llm` consumer comment and its guard test.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `cd server && go test ./internal/service -run 'TestIntakeSplit' ./pkg/llm -count=1`
Expected: PASS

### Task 4: Submit creates one issue, one branch, one deploy

**Files:**
- Create: `server/internal/service/intake_submit.go`
- Test: `server/internal/handler/intake_submit_test.go`

- [ ] **Step 1: Write the failing submit tests**

`POST /api/intake/requests` with `{body, client_request_id, start_at}`:

- No eligible project returns 422 `intake_project_lead_missing` and no issue.
- A split with two tasks creates one issue. The description is the ordered list plus the sentence `在同一个分支上按顺序做完，只部署一次。` `source_body` keeps the original text. Assignee is that module's primary.
- Repeating the same `client_request_id` returns the same issue and does not call the model again.
- Primary online enqueues one task even when the agent is at capacity.
- Primary offline and backup online assigns the backup, writes why, and enqueues one task.
- Both offline leaves the primary as assignee, does not enqueue, and records a maintenance notice.
- A future `start_at` creates one `issue_wakeup` (`kind=at`, `mode=once`) and does not enqueue.
- A third module's agent has no queue row.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `cd server && go test ./internal/handler -run 'TestIntakeSubmit' -count=1`
Expected: FAIL

- [ ] **Step 3: Implement submit**

Create the issue and `intake_request` in one transaction. Enqueue after commit. Append the module scope and the project test environment URL to the run context. Do not add a deploy pipeline.

- [ ] **Step 4: Run the test and confirm it passes**

Run: `cd server && go test ./internal/handler -run 'TestIntakeSubmit' -count=1`
Expected: PASS

### Task 5: Modules and members

**Files:**
- Modify: `server/internal/handler/intake.go`
- Test: `server/internal/handler/intake_module_test.go`

- [ ] **Step 1: Write failing tests for the capability map**

A lead can replace modules for their project. A member receives 403. An agent owner cannot attach their own agent. Primary and backup must differ, both need `intake_can`, and a duplicate product plus module returns 422. `PUT /api/intake/projects/{projectId}/members` replaces everyone except the lead. A non-member `GET /api/intake/requests` does not include that project's body.

- [ ] **Step 2: Implement and pass**

Run: `cd server && go test ./internal/handler -run 'TestIntakeModule|TestIntakeMember' -count=1`
Expected: PASS

## Chunk 3: Progress, recovery, and cards

### Task 6: Blocker, continue, stop, accept, cancel, resume

**Files:**
- Create: `server/internal/service/intake_decision.go`
- Test: `server/internal/handler/intake_decision_test.go`

- [ ] **Step 1: Write failing decision tests**

The assignee runtime can open one blocker with three non-empty sentences. Continue on a running task writes a supplement. Continue after the run ends enqueues the same agent without `forceFreshSession`. Stop cancels the running task and leaves the issue blocked. Accept requires `in_review` and no open blocker, then sets `done` without enqueue. Cancel before enqueue closes the wakeup. Resume rechecks primary, then backup, and does not enqueue while both are offline.

- [ ] **Step 2: Implement and pass**

Run: `cd server && go test ./internal/handler -run 'TestIntakeDecision' -count=1`
Expected: PASS

### Task 7: Retry inside the same module

**Files:**
- Modify: `server/internal/service/intake_decision.go`
- Test: `server/internal/service/intake_retry_test.go`

- [ ] **Step 1: Write failing retry tests**

`idle_watchdog`, timeout, runtime offline, queued expiry, and provider 5xx retry the same primary after 1 minute and again after 5 minutes. The next failure uses the backup only when it is online, with a new session. A second open blocker after an automatic continue switches to the backup only when online. Two exhausted passes set the row to `等待处理` with `执行没有进展`. An explicit stop is not restarted.

- [ ] **Step 2: Implement and pass**

Run: `cd server && go test ./internal/service -run 'TestIntakeRetry' -count=1`
Expected: PASS

### Task 8: Progress and offline notices

**Files:**
- Create: `server/internal/service/intake_progress.go`
- Test: `server/internal/service/intake_progress_test.go`

- [ ] **Step 1: Write failing cadence tests**

From the moment work may start, daytime claims a notice at 30 minutes and nighttime at 2 hours. The claim is a conditional update of `last_user_notice_at`. A failed send does not move the timestamp. Progress stops for `待验收`, `已完成`, `已经停住`, cancelled, an open blocker, and `执行没有进展`. One missed heartbeat does not alert. An online agent missing heartbeats for 150 seconds alerts the owner once per machine and the project lead once. The requester card says `还没开始` and contains no agent or machine name.

- [ ] **Step 2: Implement on the existing 30 second loop**

Run: `cd server && go test ./internal/service -run 'TestIntakeProgress|TestIntakeOffline' -count=1`
Expected: PASS

### Task 9: Personal Feishu cards

**Files:**
- Create: `server/internal/integrations/lark/intake_card.go`
- Test: `server/internal/integrations/lark/intake_card_test.go`

- [ ] **Step 1: Write a card snapshot test**

Requester cards use the existing interactive card shape and one URL button. They include the utterance, progress, blocker sentences, or result, and never an agent name or machine. Owner and lead maintenance cards may include both. No `open_id` skips the send and leaves the task running. No inbound message creates an intake request.

- [ ] **Step 2: Implement and pass**

Run: `cd server && go test ./internal/integrations/lark -run 'TestIntakeCard' -count=1`
Expected: PASS

## Chunk 4: Web chat and management

### Task 10: API schema and chat page

**Files:**
- Modify: `packages/core/api/schema.ts`
- Create: `packages/core/api/intake.ts`
- Create: `packages/views/intake/intake-page.tsx`
- Test: `packages/views/intake/intake-page.test.tsx`
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/intake/page.tsx`

- [ ] **Step 1: Write the failing page test**

The page opens in chat. The composer has a text area and one send button, with no project, module, or agent picker. A successful send shows `正在做` and `做到` / `还剩`, and the response agent name is not rendered. `intake_split_empty` leaves the text in the composer and shows `没对上，请再补一句`. An open blocker shows the three sentences, with 继续 as the primary button and 停止 as the secondary button.

- [ ] **Step 2: Implement the page**

Use TanStack Query keys `['intake', wsId, 'mine']`, `['intake', wsId]`, and `['intake-scheduled', wsId]`. Keep the draft in component state. Parse responses with zod and `parseWithFallback`. Requester payloads omit `agent_name`.

- [ ] **Step 3: Run the test**

Run: `pnpm --filter @multica/views test intake-page.test.tsx`
Expected: PASS

### Task 11: Management mode

**Files:**
- Create: `packages/views/intake/intake-manage-page.tsx`
- Test: `packages/views/intake/intake-manage-page.test.tsx`
- Create: `packages/views/intake/intake-bar.tsx`
- Create: `packages/views/intake/help-panel.tsx`

- [ ] **Step 1: Write failing management tests**

A member switch shows scheduled items, their own agents, and progress without an executor. A lead sees the capability map first and can change primary, backup, members, and the test environment URL. The logo opens help. The avatar menu shows the person and logout. Refresh returns to chat.

- [ ] **Step 2: Implement and pass**

Run: `pnpm --filter @multica/views test intake-manage-page.test.tsx intake-page.test.tsx`
Expected: PASS

### Task 12: Workspace entry

**Files:**
- Modify: the workspace home redirect for members
- Modify: `packages/core/paths/paths.ts`

- [ ] **Step 1: Send members to intake**

A workspace member opening the workspace home lands on `/{workspaceSlug}/intake`. The old issues board stays available by its existing URL. `intake` is not added as a reserved root slug.

- [ ] **Step 2: Run the route test that covers the redirect**

Run: `pnpm --filter @multica/web test workspace-home`
Expected: PASS, or add the focused test next to the existing home redirect if none exists.

## Chunk 5: Verification

- [ ] Run `make sqlc`
- [ ] Run `cd server && go test ./internal/handler ./internal/service ./internal/integrations/lark ./pkg/llm -count=1`
- [ ] Run `pnpm --filter @multica/views test intake-page.test.tsx intake-manage-page.test.tsx`
- [ ] Run `pnpm --filter @multica/core test schema` for the new malformed-response cases
- [ ] Do not run the full Playwright suite unless a route redirect test cannot be covered at unit level

Commit each chunk separately with a conventional message. The spec documents in `docs/engineering/agent-intake/` stay the behavior source. Update them only when a chunk changes a locked rule.
