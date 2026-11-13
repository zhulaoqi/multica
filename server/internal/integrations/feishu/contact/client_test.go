package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeFeishu struct {
	tokenCalls atomic.Int32
	// departments pages are served in order; page_token "p2" selects the second.
	departmentPages []map[string]any
	// usersByDept maps department_id to its pages.
	usersByDept map[string][]map[string]any
	// failCode, when non-zero, is returned for every contact call.
	failCode int
}

func (f *fakeFeishu) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}
		switch r.URL.Path {
		case tokenPath:
			f.tokenCalls.Add(1)
			writeJSON(map[string]any{"code": 0, "tenant_access_token": "t-token", "expire": 7200})
		case departmentsPath:
			if f.failCode != 0 {
				writeJSON(map[string]any{"code": f.failCode, "msg": "no permission"})
				return
			}
			if got := r.URL.Query().Get("fetch_child"); got != "true" {
				t.Errorf("fetch_child = %q, want true", got)
			}
			page := 0
			if r.URL.Query().Get("page_token") == "p2" {
				page = 1
			}
			if page >= len(f.departmentPages) {
				t.Fatalf("unexpected department page %d", page)
			}
			writeJSON(map[string]any{"code": 0, "data": f.departmentPages[page]})
		case departmentUsers:
			if f.failCode != 0 {
				writeJSON(map[string]any{"code": f.failCode, "msg": "no permission"})
				return
			}
			dept := r.URL.Query().Get("department_id")
			pages, ok := f.usersByDept[dept]
			if !ok {
				t.Fatalf("unexpected department %q", dept)
			}
			page := 0
			if r.URL.Query().Get("page_token") == "p2" {
				page = 1
			}
			writeJSON(map[string]any{"code": 0, "data": pages[page]})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	})
}

func newTestClient(t *testing.T, fake *fakeFeishu) *Client {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)
	return New(Config{AppID: "app", AppSecret: "secret", BaseURL: server.URL, PageInterval: time.Nanosecond})
}

func TestListDepartmentsPaginatesAndReusesToken(t *testing.T) {
	fake := &fakeFeishu{
		departmentPages: []map[string]any{
			{
				"items": []map[string]any{
					{"open_department_id": "od-root", "parent_department_id": "0", "name": "数据部", "leader_user_id": "ou-boss"},
				},
				"has_more": true, "page_token": "p2",
			},
			{
				"items": []map[string]any{
					{"open_department_id": "od-child", "parent_department_id": "od-root", "name": "支付组"},
				},
				"has_more": false,
			},
		},
		usersByDept: map[string][]map[string]any{
			"od-root": {{"items": []map[string]any{
				{"open_id": "ou-boss", "union_id": "on-boss", "name": "Boss", "email": "Boss@Example.com",
					"avatar": map[string]any{"avatar_72": "https://img/72.png"}},
			}, "has_more": false}},
		},
	}
	client := newTestClient(t, fake)

	departments, err := client.ListDepartments(context.Background())
	if err != nil {
		t.Fatalf("ListDepartments: %v", err)
	}
	if len(departments) != 2 {
		t.Fatalf("departments = %+v, want 2", departments)
	}
	if departments[0].ParentID != "" {
		t.Fatalf("root parent = %q, want empty", departments[0].ParentID)
	}
	if departments[1].ParentID != "od-root" || departments[1].Name != "支付组" {
		t.Fatalf("child = %+v", departments[1])
	}
	if departments[0].LeadOpenID != "ou-boss" {
		t.Fatalf("root lead = %q", departments[0].LeadOpenID)
	}

	users, err := client.ListDepartmentUsers(context.Background(), []string{"od-root"})
	if err != nil {
		t.Fatalf("ListDepartmentUsers: %v", err)
	}
	if len(users) != 1 || users[0].Email != "Boss@Example.com" || users[0].AvatarURL != "https://img/72.png" {
		t.Fatalf("users = %+v", users)
	}
	if got := fake.tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls = %d, want 1 cached token for the whole run", got)
	}
}

func TestListDepartmentUsersDeduplicatesByOpenID(t *testing.T) {
	fake := &fakeFeishu{
		usersByDept: map[string][]map[string]any{
			"od-a": {{"items": []map[string]any{
				{"open_id": "ou-1", "name": "Ada", "email": "ada@example.com"},
			}, "has_more": false}},
			"od-b": {{"items": []map[string]any{
				{"open_id": "ou-1", "name": "Ada", "email": "ada@example.com"},
				{"open_id": "ou-2", "name": "Bob", "email": ""},
			}, "has_more": false}},
		},
	}
	client := newTestClient(t, fake)

	users, err := client.ListDepartmentUsers(context.Background(), []string{"od-a", "od-b"})
	if err != nil {
		t.Fatalf("ListDepartmentUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %+v, want 2 distinct people", users)
	}
	if users[1].Email != "" {
		t.Fatalf("hidden email should stay empty, got %q", users[1].Email)
	}
	if users[0].DepartmentID != "od-a" {
		t.Fatalf("department = %q, want the first department the person appeared in", users[0].DepartmentID)
	}
}

func TestPermissionErrorNamesRequiredScopes(t *testing.T) {
	client := newTestClient(t, &fakeFeishu{failCode: 99991672})

	_, err := client.ListDepartments(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	for _, want := range []string{"99991672", "contact:department.base:readonly", "contact:user.email:readonly"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q should mention %q", message, want)
		}
	}
}

func TestMissingCredentials(t *testing.T) {
	client := New(Config{})
	if _, err := client.ListDepartments(context.Background()); err == nil {
		t.Fatal("expected ErrNotConfigured")
	} else if !strings.Contains(fmt.Sprint(err), "not configured") {
		t.Fatalf("error = %v", err)
	}
}
