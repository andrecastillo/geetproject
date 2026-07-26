package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dv310p3r/geet/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	h := New(st, nil)
	// Tickets need somewhere to live, so every API test starts with one project.
	if rec := do(t, h, "POST", "/api/projects",
		map[string]any{"name": "Demo", "slug": "demo", "prefix": "DEMO"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("seed project: %d %s", rec.Code, rec.Body)
	}
	return h
}

// do issues a request and decodes the JSON body into out (when non-nil).
func do(t *testing.T, h http.Handler, method, path string, body any, out any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec
}

func TestHealth(t *testing.T) {
	h := newTestServer(t)
	if rec := do(t, h, "GET", "/healthz", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestTicketLifecycle(t *testing.T) {
	h := newTestServer(t)

	var epic store.Ticket
	rec := do(t, h, "POST", "/api/tickets",
		map[string]any{"project": "demo", "type": "epic", "title": "Epic", "status": "todo"}, &epic)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create epic: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	if epic.Key != "DEMO-1" {
		t.Fatalf("want key DEMO-1, got %s", epic.Key)
	}

	var task store.Ticket
	do(t, h, "POST", "/api/tickets",
		map[string]any{"type": "task", "title": "Task", "parent": epic.Key, "status": "todo"}, &task)

	var sub store.Ticket
	do(t, h, "POST", "/api/tickets",
		map[string]any{"type": "subtask", "title": "Sub", "parent": task.Key, "status": "todo"}, &sub)

	// Detail view arrives in one round trip: ticket + children + comments.
	do(t, h, "POST", "/api/tickets/"+task.Key+"/comments", map[string]any{"body": "a note"}, nil)
	var detail struct {
		store.Ticket
		Children []store.Ticket  `json:"children"`
		Comments []store.Comment `json:"comments"`
	}
	do(t, h, "GET", "/api/tickets/"+task.Key, nil, &detail)
	if len(detail.Children) != 1 || detail.Children[0].Key != sub.Key {
		t.Fatalf("want the sub-task listed as a child, got %+v", detail.Children)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "a note" {
		t.Fatalf("want 1 comment, got %+v", detail.Comments)
	}
	if detail.ParentKey != epic.Key {
		t.Errorf("want parent breadcrumb %s, got %q", epic.Key, detail.ParentKey)
	}

	// PATCH with only a title must not touch the description.
	do(t, h, "PATCH", "/api/tickets/"+task.Key, map[string]any{"description": "keep me"}, nil)
	var patched store.Ticket
	do(t, h, "PATCH", "/api/tickets/"+task.Key, map[string]any{"title": "Renamed"}, &patched)
	if patched.Title != "Renamed" || patched.Description != "keep me" {
		t.Fatalf("partial patch damaged the ticket: %+v", patched)
	}

	// Deleting the epic takes the whole subtree with it.
	if rec := do(t, h, "DELETE", "/api/tickets/"+epic.Key, nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: want 204, got %d", rec.Code)
	}
	for _, k := range []string{epic.Key, task.Key, sub.Key} {
		if rec := do(t, h, "GET", "/api/tickets/"+k, nil, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s should be gone, got %d", k, rec.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	h := newTestServer(t)

	if rec := do(t, h, "GET", "/api/tickets/DEMO-999", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing ticket: want 404, got %d", rec.Code)
	}
	// A sub-task with no parent violates the hierarchy.
	rec := do(t, h, "POST", "/api/tickets", map[string]any{"project": "demo", "type": "subtask", "title": "orphan"}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("orphan sub-task: want 400, got %d (%s)", rec.Code, rec.Body)
	}
	// Duplicate label.
	do(t, h, "POST", "/api/labels", map[string]any{"name": "dup"}, nil)
	if rec := do(t, h, "POST", "/api/labels", map[string]any{"name": "dup"}, nil); rec.Code != http.StatusConflict {
		t.Errorf("duplicate label: want 409, got %d", rec.Code)
	}
	// Unknown field is rejected rather than silently ignored.
	if rec := do(t, h, "POST", "/api/tickets", map[string]any{"titel": "typo"}, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: want 400, got %d", rec.Code)
	}
}

func TestBoardEndpointAssemblesColumns(t *testing.T) {
	h := newTestServer(t)

	var task store.Ticket
	do(t, h, "POST", "/api/tickets",
		map[string]any{"project": "demo", "type": "task", "title": "Login", "status": "todo"}, &task)
	var sub store.Ticket
	do(t, h, "POST", "/api/tickets",
		map[string]any{"type": "subtask", "title": "form", "parent": task.Key, "status": "todo"}, &sub)

	var view store.BoardView
	do(t, h, "GET", "/api/projects/demo/boards/all-work", nil, &view)
	col := columnBySlug(t, &view, "todo")
	if len(col.Cards) != 1 || len(col.Cards[0].Subtasks) != 1 {
		t.Fatalf("want the sub-task nested in its parent card, got %+v", col.Cards)
	}

	// Moving the sub-task to Done breaks it out onto its own card.
	rec := do(t, h, "POST", "/api/tickets/"+sub.Key+"/move",
		map[string]any{"project": "demo", "board": "all-work", "status": "done"}, &view)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: want 200, got %d (%s)", rec.Code, rec.Body)
	}
	done := columnBySlug(t, &view, "done")
	if len(done.Cards) != 1 || done.Cards[0].Key != sub.Key {
		t.Fatalf("want the sub-task as its own card in done, got %+v", done.Cards)
	}
	if done.Cards[0].ParentKey != task.Key {
		t.Errorf("want a parent breadcrumb on the broken-out card, got %q", done.Cards[0].ParentKey)
	}
	todo := columnBySlug(t, &view, "todo")
	if len(todo.Cards) != 1 || len(todo.Cards[0].Subtasks) != 0 {
		t.Errorf("parent card should no longer nest it, got %+v", todo.Cards[0].Subtasks)
	}
}

func columnBySlug(t *testing.T, view *store.BoardView, slug string) store.Column {
	t.Helper()
	for _, c := range view.Columns {
		if c.Status.Slug == slug {
			return c
		}
	}
	t.Fatalf("board has no %q column", slug)
	return store.Column{}
}

func TestImportEndpoint(t *testing.T) {
	h := newTestServer(t)

	batch := map[string]any{
		"tickets": []map[string]any{{
			"type": "epic", "title": "Chat UI", "labels": []string{"app"},
			"children": []map[string]any{{
				"title": "Flask route", "status": "in-progress",
				"children": []map[string]any{{"title": "Stream tokens"}},
			}},
		}},
	}

	// A dry run reports without writing.
	var preview struct {
		DryRun  bool `json:"dry_run"`
		Count   int  `json:"count"`
		Tickets []struct {
			Key, Type string
			Depth     int
		} `json:"tickets"`
	}
	rec := do(t, h, "POST", "/api/projects/demo/import?dry_run=1", batch, &preview)
	if rec.Code != http.StatusOK {
		t.Fatalf("dry run: want 200, got %d (%s)", rec.Code, rec.Body)
	}
	if !preview.DryRun || preview.Count != 3 {
		t.Fatalf("want a 3-ticket dry run, got %+v", preview)
	}
	var after []store.Ticket
	do(t, h, "GET", "/api/tickets", nil, &after)
	if len(after) != 0 {
		t.Fatalf("dry run wrote %d tickets", len(after))
	}

	// The real thing creates the tree.
	rec = do(t, h, "POST", "/api/projects/demo/import", batch, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: want 201, got %d (%s)", rec.Code, rec.Body)
	}
	do(t, h, "GET", "/api/tickets", nil, &after)
	if len(after) != 3 {
		t.Fatalf("want 3 tickets created, got %d", len(after))
	}

	// A bad batch is a 400 and writes nothing.
	rec = do(t, h, "POST", "/api/projects/demo/import",
		map[string]any{"tickets": []map[string]any{{"title": ""}}}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad batch: want 400, got %d (%s)", rec.Code, rec.Body)
	}
	do(t, h, "GET", "/api/tickets", nil, &after)
	if len(after) != 3 {
		t.Errorf("a rejected batch changed the ticket count to %d", len(after))
	}
}

// An /api path this build doesn't serve must be a JSON 404. If it falls through
// to the SPA the client gets 200 and a page of HTML, and a version mismatch
// looks like a JSON parse error instead of a missing endpoint.
func TestUnknownAPIPathIsJSON404(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, "GET", "/api/does-not-exist", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d (%s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("want a JSON content type, got %q", ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Errorf("want a JSON error body, got %q", rec.Body.String())
	}
}
