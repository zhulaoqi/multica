package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// issueScopeFixture is an organization workspace with one department issue
// (sent to the 支付组 sub-team of 平台部) and one issue without a department,
// both in a dedicated project so list assertions ignore other rows.
type issueScopeFixture struct {
	projectID, deptIssue, flatIssue, needle string
	viewers                                 map[string]string
}

func newIssueScopeFixture(t *testing.T) issueScopeFixture {
	t.Helper()
	ctx := context.Background()
	viewer := func(name string) string {
		return createPermissionTestMember(t, fmt.Sprintf("issue-scope-%s@example.com", name))
	}
	f := issueScopeFixture{viewers: map[string]string{
		"member of the department": viewer("dept"),
		"member of the parent":     viewer("parent"),
		"outsider":                 viewer("outsider"),
		"parent lead":              viewer("lead"),
		"creator":                  viewer("creator"),
		"assignee":                 viewer("assignee"),
		"subscriber":               viewer("subscriber"),
		"workspace owner":          testUserID,
	}}
	v := f.viewers
	parent := teamRuntimeUnit(t, "issue-scope-parent", v["parent lead"])
	var dept, other string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name, parent_id, position) VALUES ($1, 'issue-scope-dept', $2, 0) RETURNING id
	`, testWorkspaceID, parent).Scan(&dept); err != nil {
		t.Fatalf("create department: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO org_unit (workspace_id, name, position) VALUES ($1, 'issue-scope-other', 0) RETURNING id
	`, testWorkspaceID).Scan(&other); err != nil {
		t.Fatalf("create other department: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM org_unit WHERE id = ANY($1::uuid[])`, []string{dept, other})
	})
	place := map[string]string{
		"member of the department": dept,
		"member of the parent":     parent,
		"outsider":                 other,
		"creator":                  other,
		"assignee":                 other,
		"subscriber":               other,
	}
	for name, unit := range place {
		dbfx.Exec(t, `UPDATE member SET org_unit_id = $3 WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, v[name], unit)
	}

	f.projectID = createChatProjectTestProject(t, testWorkspaceID, "issue-scope-project", "")
	f.needle = "issue-scope-needle"
	f.flatIssue = dbfx.Issue(t, f.needle+" flat", testutil.Cols{"project_id": f.projectID})
	f.deptIssue = dbfx.Issue(t, f.needle+" department", testutil.Cols{
		"project_id":      f.projectID,
		"parent_issue_id": f.flatIssue,
		"org_unit_id":     dept,
		"creator_id":      v["creator"],
		"assignee_type":   "member",
		"assignee_id":     v["assignee"],
	})
	dbfx.Exec(t, `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason) VALUES ($1, 'member', $2, 'manual')
	`, f.deptIssue, v["subscriber"])
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_subscriber WHERE issue_id = $1`, f.deptIssue)
	})
	return f
}

// visibleIssueIDs reads every surface that lists issues and fails when they
// disagree, returning the ids the viewer sees.
func (f issueScopeFixture) visibleIssueIDs(t *testing.T, userID string) map[string]bool {
	t.Helper()
	collect := func(ids []string) map[string]bool {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == f.deptIssue || id == f.flatIssue {
				seen[id] = true
			}
		}
		return seen
	}

	var list struct {
		Issues []IssueResponse `json:"issues"`
		Total  int64           `json:"total"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequestAs(userID, http.MethodGet, "/api/issues?project_id="+f.projectID, nil)).
		Want(http.StatusOK).JSON(&list)
	listed := collect(scopedIssueIDs(list.Issues))
	if int(list.Total) != len(list.Issues) {
		t.Fatalf("list total = %d, rows = %d", list.Total, len(list.Issues))
	}

	var search struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.SearchIssues, newRequestAs(userID, http.MethodGet, "/api/issues/search?q="+url.QueryEscape(f.needle), nil)).
		Want(http.StatusOK).JSON(&search)

	var rows issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows, newRequestAs(userID, http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
		Query: issueTableQuerySpec{
			Scope: issueTableScope{Kind: "project", ProjectID: f.projectID},
			Sort:  issueTableSortRequest{Field: "position", Direction: "asc"},
		},
		Group: issueTableGroupSpec{Kind: "none"},
		Page:  issueTablePageRequest{Limit: 50},
	})).Want(http.StatusOK).JSON(&rows)
	tableIDs := make([]string, len(rows.Rows))
	for i, row := range rows.Rows {
		tableIDs[i] = row.Issue.ID
	}

	var children struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListChildIssues, withURLParam(newRequestAs(userID, http.MethodGet, "/api/issues/"+f.flatIssue+"/children", nil), "id", f.flatIssue)).
		Want(http.StatusOK).JSON(&children)
	childSeen := collect(scopedIssueIDs(children.Issues))

	for surface, seen := range map[string]map[string]bool{
		"search": collect(scopedIssueIDs(search.Issues)),
		"table":  collect(tableIDs),
	} {
		if len(seen) != len(listed) || seen[f.deptIssue] != listed[f.deptIssue] {
			t.Fatalf("%s = %v, list = %v", surface, seen, listed)
		}
	}
	if childSeen[f.deptIssue] != listed[f.deptIssue] {
		t.Fatalf("children = %v, list = %v", childSeen, listed)
	}

	detail := testutil.Call(t, testHandler.GetIssue, withURLParam(newRequestAs(userID, http.MethodGet, "/api/issues/"+f.deptIssue, nil), "id", f.deptIssue))
	if listed[f.deptIssue] {
		detail.Want(http.StatusOK)
	} else {
		detail.Want(http.StatusNotFound)
	}
	return listed
}

func scopedIssueIDs(issues []IssueResponse) []string {
	ids := make([]string, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID
	}
	return ids
}

func TestOrganizationIssuesAreScopedByDepartment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	markOrganizationWorkspace(t)
	f := newIssueScopeFixture(t)

	cases := map[string]bool{
		"member of the department": true,
		"member of the parent":     false,
		"outsider":                 false,
		"parent lead":              true,
		"creator":                  true,
		"assignee":                 true,
		"subscriber":               true,
		"workspace owner":          true,
	}
	audience, err := testHandler.Queries.ListIssueAudience(context.Background(), parseUUID(f.deptIssue))
	if err != nil {
		t.Fatalf("list issue audience: %v", err)
	}
	inAudience := map[string]bool{}
	for _, user := range audience {
		inAudience[uuidToString(user)] = true
	}
	for name, wantDept := range cases {
		t.Run(name, func(t *testing.T) {
			if inAudience[f.viewers[name]] != wantDept {
				t.Fatalf("realtime audience = %v, want %v", inAudience[f.viewers[name]], wantDept)
			}
			seen := f.visibleIssueIDs(t, f.viewers[name])
			if !seen[f.flatIssue] {
				t.Fatalf("issue without a department must stay visible: %v", seen)
			}
			if seen[f.deptIssue] != wantDept {
				t.Fatalf("department issue visible = %v, want %v", seen[f.deptIssue], wantDept)
			}
		})
	}
}

func TestProjectIssuesIgnoreDepartmentScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newIssueScopeFixture(t)
	if seen := f.visibleIssueIDs(t, f.viewers["outsider"]); !seen[f.deptIssue] {
		t.Fatalf("project member must see every issue: %v", seen)
	}
}
