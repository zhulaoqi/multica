package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestDispatchWork_PersonalRefusedWhenTwinOffline keeps a personal dispatch
// from creating work for a twin that cannot run: the requester is told to start
// their runtime instead of being queued behind it.
func TestDispatchWork_PersonalRefusedWhenTwinOffline(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "personal-offline-admin@example.com")
	insertIntakeDutyAgent(t, "personal-offline-twin", admin)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, _, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 离线分身",
		"client_request_id": testClientRequestID,
		"personal":          true,
	})
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", status, body)
	}
	if !strings.Contains(body, "org_dispatch_leader_offline") {
		t.Fatalf("body = %s, want org_dispatch_leader_offline", body)
	}
}

// TestDispatchWork_PersonalCreatesAndReusesPersonalSquad covers the personal
// entry: the first send creates the requester's private squad led by their
// twin, the second reuses it, and history still lists both as personal.
func TestDispatchWork_PersonalCreatesAndReusesPersonalSquad(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "personal-dispatch-admin@example.com")
	twin := insertRunnableDutyAgent(t, "personal-dispatch-twin", admin)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, res, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 整理下周的发布计划",
		"client_request_id": testClientRequestID,
		"personal":          true,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	if !res.Dispatch.Personal || res.Dispatch.SquadID == "" {
		t.Fatalf("dispatch = %+v, want personal squad", res.Dispatch)
	}
	if res.Dispatch.UnitID != "" {
		t.Fatalf("dispatch unit_id = %q, want empty on a personal dispatch", res.Dispatch.UnitID)
	}

	var name string
	var isPrivate bool
	var orgUnitValid bool
	var creatorID, leaderID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT name, is_private, org_unit_id IS NOT NULL, creator_id, leader_id
		FROM squad WHERE id = $1
	`, res.Dispatch.SquadID).Scan(&name, &isPrivate, &orgUnitValid, &creatorID, &leaderID); err != nil {
		t.Fatalf("load personal squad: %v", err)
	}
	if !isPrivate || orgUnitValid {
		t.Fatalf("squad is_private/org_unit = %v/%v, want true/false", isPrivate, orgUnitValid)
	}
	if creatorID != admin || leaderID != twin {
		t.Fatalf("squad creator/leader = %s/%s, want %s/%s", creatorID, leaderID, admin, twin)
	}
	if name == "" {
		t.Fatal("personal squad has no name")
	}

	var targetUnitValid bool
	var targetSquad string
	if err := testPool.QueryRow(context.Background(), `
		SELECT target_unit_id IS NOT NULL, target_squad_id::text
		FROM dispatch_origin WHERE issue_id = $1
	`, res.Issue.ID).Scan(&targetUnitValid, &targetSquad); err != nil {
		t.Fatalf("load personal dispatch origin: %v", err)
	}
	if targetUnitValid || targetSquad != res.Dispatch.SquadID {
		t.Fatalf("origin target unit/squad = %v/%s, want false/%s", targetUnitValid, targetSquad, res.Dispatch.SquadID)
	}

	status, again, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 再补一份",
		"client_request_id": testClientRequestID2,
		"personal":          true,
	})
	if status != http.StatusCreated {
		t.Fatalf("second status = %d, want 201: %s", status, body)
	}
	if again.Dispatch.SquadID != res.Dispatch.SquadID {
		t.Fatalf("second dispatch squad = %s, want reuse of %s", again.Dispatch.SquadID, res.Dispatch.SquadID)
	}

	status, items, body := listDispatches(t, admin, "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", status, body)
	}
	var personalItems int
	for _, item := range items {
		if item.Personal {
			personalItems++
			if item.SquadID != res.Dispatch.SquadID {
				t.Fatalf("history squad_id = %s, want %s", item.SquadID, res.Dispatch.SquadID)
			}
		}
	}
	if personalItems != 2 {
		t.Fatalf("personal history items = %d, want 2: %+v", personalItems, items)
	}
}

// TestDispatchWork_PersonalRetryIsIdempotent keeps the workbench retry path
// returning the same personal dispatch instead of creating a second one.
func TestDispatchWork_PersonalRetryIsIdempotent(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "personal-retry-admin@example.com")
	insertRunnableDutyAgent(t, "personal-retry-twin", admin)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, first, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 幂等派活",
		"client_request_id": testClientRequestID,
		"personal":          true,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", status, body)
	}
	status, retry, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 幂等派活",
		"client_request_id": testClientRequestID,
		"personal":          true,
	})
	if status != http.StatusOK {
		t.Fatalf("retry status = %d, want 200: %s", status, body)
	}
	if retry.Issue.ID != first.Issue.ID || !retry.Dispatch.Personal ||
		retry.Dispatch.SquadID != first.Dispatch.SquadID {
		t.Fatalf("retry = %+v, want the same personal dispatch as %+v", retry, first)
	}
}

// TestDispatchWork_PersonalRejectsSomeoneElsesSquad keeps a personal squad
// private even when another member knows its id.
func TestDispatchWork_PersonalRejectsSomeoneElsesSquad(t *testing.T) {
	startDispatchTest(t)
	owner := createOrgTestAdmin(t, "personal-owner@example.com")
	insertRunnableDutyAgent(t, "personal-owner-twin", owner)
	other := createPlainMember(t, "personal-other@multica.test")
	insertRunnableDutyAgent(t, "personal-other-twin", other)
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, first, body := postDispatchWork(t, owner, map[string]any{
		"text":              "dispatch-test 我的私事",
		"client_request_id": testClientRequestID,
		"personal":          true,
	})
	if status != http.StatusCreated {
		t.Fatalf("owner status = %d, want 201: %s", status, body)
	}

	status, _, body = postDispatchWork(t, other, map[string]any{
		"text":              "dispatch-test 偷看别人的小队",
		"client_request_id": testClientRequestID,
		"squad_id":          first.Dispatch.SquadID,
	})
	if status != http.StatusNotFound || !containsCode(body, "personal_squad_not_found") {
		t.Fatalf("other member status = %d, want 404 personal_squad_not_found: %s", status, body)
	}
}

// TestDispatchWork_PersonalAndDepartmentConflict refuses an ambiguous target
// instead of silently preferring one.
func TestDispatchWork_PersonalAndDepartmentConflict(t *testing.T) {
	startDispatchTest(t)
	admin := createOrgTestAdmin(t, "personal-conflict-admin@example.com")
	insertRunnableDutyAgent(t, "personal-conflict-twin", admin)
	unitID := createOrgUnitAs(t, admin, "冲突部", "")
	t.Cleanup(func() { clearDispatchFixtures(t) })

	status, _, body := postDispatchWork(t, admin, map[string]any{
		"text":              "dispatch-test 目标冲突",
		"client_request_id": testClientRequestID,
		"unit_id":           unitID,
		"personal":          true,
	})
	if status != http.StatusBadRequest || !containsCode(body, "org_dispatch_target_conflict") {
		t.Fatalf("status = %d, want 400 org_dispatch_target_conflict: %s", status, body)
	}
}
