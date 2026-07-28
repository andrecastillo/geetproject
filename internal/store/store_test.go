package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCreate(t *testing.T, s *Store, in NewTicket) *Ticket {
	t.Helper()
	if in.ProjectSlug == "" && in.ParentKey == "" {
		in.ProjectSlug = "demo"
	}
	tk, err := s.CreateTicket(context.Background(), in)
	if err != nil {
		t.Fatalf("create %q: %v", in.Title, err)
	}
	return tk
}

// mustProject creates a project and returns it.
func mustProject(t *testing.T, s *Store, name, slug, prefix string) *Project {
	t.Helper()
	p, err := s.CreateProject(context.Background(), Project{Name: name, Slug: slug, Prefix: prefix})
	if err != nil {
		t.Fatalf("create project %q: %v", name, err)
	}
	return p
}

// newSeededStore is a store with one project, "demo" (prefix DEMO), which is
// what mustCreate files tickets under by default.
func newSeededStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	mustProject(t, s, "Demo", "demo", "DEMO")
	return s
}

func TestSeedCreatesStatusesAndBoards(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	st, err := s.ListStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 4 {
		t.Fatalf("want 4 seeded statuses, got %d", len(st))
	}
	if st[0].Slug != "backlog" || !st[3].IsDone {
		t.Fatalf("unexpected seeded statuses: %+v", st)
	}

	boards, err := s.ListBoards(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 3 {
		t.Fatalf("want 3 seeded boards, got %d", len(boards))
	}
	for i, want := range []string{"all-work", "epics", "tasks"} {
		if boards[i].Slug != want {
			t.Errorf("tab %d: want %s, got %s", i, want, boards[i].Slug)
		}
	}
	view, err := s.GetBoard(ctx, "demo", "epics")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Columns) != 4 {
		t.Fatalf("want 4 columns on seeded board, got %d", len(view.Columns))
	}
}

func TestHierarchyRules(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	epic := mustCreate(t, s, NewTicket{Type: TypeEpic, Title: "Epic"})
	task := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Task", ParentKey: epic.Key})
	mustCreate(t, s, NewTicket{Type: TypeSubtask, Title: "Sub", ParentKey: task.Key})

	// A standalone task is legal.
	mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Loose task"})

	for _, tc := range []struct {
		name string
		in   NewTicket
	}{
		{"epic with a parent", NewTicket{Type: TypeEpic, Title: "x", ParentKey: epic.Key}},
		{"task under a task", NewTicket{Type: TypeTask, Title: "x", ParentKey: task.Key}},
		{"subtask under an epic", NewTicket{Type: TypeSubtask, Title: "x", ParentKey: epic.Key}},
		{"subtask with no parent", NewTicket{Type: TypeSubtask, Title: "x"}},
	} {
		if _, err := s.CreateTicket(ctx, tc.in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", tc.name, err)
		}
	}
}

func TestCannotChangeTypeWithChildren(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()
	epic := mustCreate(t, s, NewTicket{Type: TypeEpic, Title: "Epic"})
	mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Task", ParentKey: epic.Key})

	newType := TypeTask
	if _, err := s.UpdateTicket(ctx, epic.Key, TicketPatch{Type: &newType}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid changing type with children, got %v", err)
	}
}

// A PATCH that mentions only the title must not disturb anything else. This is
// the failure mode that destroys real work.
func TestPartialUpdateLeavesOtherFieldsAlone(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	label, err := s.CreateLabel(ctx, "backend", "#f00")
	if err != nil {
		t.Fatal(err)
	}
	tk := mustCreate(t, s, NewTicket{
		Type: TypeTask, Title: "Original", Description: "# notes\nimportant",
		StatusSlug: "in-progress", LabelIDs: []int64{label.ID},
	})

	title := "Renamed"
	got, err := s.UpdateTicket(ctx, tk.Key, TicketPatch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Renamed" {
		t.Errorf("title not applied: %q", got.Title)
	}
	if got.Description != "# notes\nimportant" {
		t.Errorf("description was destroyed: %q", got.Description)
	}
	if got.Status.Slug != "in-progress" {
		t.Errorf("status was reset: %q", got.Status.Slug)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "backend" {
		t.Errorf("labels were dropped: %+v", got.Labels)
	}
}

func TestDeleteCascadesToChildrenAndComments(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	epic := mustCreate(t, s, NewTicket{Type: TypeEpic, Title: "Epic"})
	task := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Task", ParentKey: epic.Key})
	sub := mustCreate(t, s, NewTicket{Type: TypeSubtask, Title: "Sub", ParentKey: task.Key})
	if _, err := s.CreateComment(ctx, task.Key, "a note"); err != nil {
		t.Fatal(err)
	}

	n, err := s.CountDescendants(ctx, epic.Key)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 descendants, got %d", n)
	}

	if err := s.DeleteTicket(ctx, epic.Key); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{epic.Key, task.Key, sub.Key} {
		if _, err := s.GetTicket(ctx, k); !errors.Is(err, ErrNotFound) {
			t.Errorf("ticket %s should be gone, got %v", k, err)
		}
	}
	var comments int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM comment`).Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != 0 {
		t.Errorf("comments should have cascaded away, %d remain", comments)
	}
}

// The board's defining behavior: nest while status matches, break out when it
// doesn't, re-nest when it matches again.
func TestSubtaskNestsAndBreaksOut(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	task := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Login", StatusSlug: "todo"})
	sub := mustCreate(t, s, NewTicket{
		Type: TypeSubtask, Title: "form", ParentKey: task.Key, StatusSlug: "todo"})

	find := func(view *BoardView, slug string) *Column {
		for i := range view.Columns {
			if view.Columns[i].Status.Slug == slug {
				return &view.Columns[i]
			}
		}
		t.Fatalf("no %s column", slug)
		return nil
	}

	view, err := s.GetBoard(ctx, "demo", "all-work")
	if err != nil {
		t.Fatal(err)
	}
	todo := find(view, "todo")
	if len(todo.Cards) != 1 {
		t.Fatalf("want 1 card while statuses match, got %d", len(todo.Cards))
	}
	if len(todo.Cards[0].Subtasks) != 1 || todo.Cards[0].Subtasks[0].Key != sub.Key {
		t.Fatalf("sub-task should be nested, got %+v", todo.Cards[0].Subtasks)
	}

	// Move the sub-task on: it must leave the parent card and become its own.
	done := "done"
	if _, err := s.UpdateTicket(ctx, sub.Key, TicketPatch{StatusSlug: &done}); err != nil {
		t.Fatal(err)
	}
	view, err = s.GetBoard(ctx, "demo", "all-work")
	if err != nil {
		t.Fatal(err)
	}
	todo, doneCol := find(view, "todo"), find(view, "done")
	if len(todo.Cards) != 1 || len(todo.Cards[0].Subtasks) != 0 {
		t.Fatalf("parent card should have no nested sub-tasks now, got %+v", todo.Cards[0].Subtasks)
	}
	if len(doneCol.Cards) != 1 || doneCol.Cards[0].Key != sub.Key {
		t.Fatalf("sub-task should be its own card in done, got %+v", doneCol.Cards)
	}
	if doneCol.Cards[0].ParentKey != task.Key {
		t.Errorf("broken-out card needs a parent breadcrumb, got %q", doneCol.Cards[0].ParentKey)
	}

	// And back again.
	todoSlug := "todo"
	if _, err := s.UpdateTicket(ctx, sub.Key, TicketPatch{StatusSlug: &todoSlug}); err != nil {
		t.Fatal(err)
	}
	view, err = s.GetBoard(ctx, "demo", "all-work")
	if err != nil {
		t.Fatal(err)
	}
	todo, doneCol = find(view, "todo"), find(view, "done")
	if len(doneCol.Cards) != 0 {
		t.Errorf("done column should be empty again, got %d cards", len(doneCol.Cards))
	}
	if len(todo.Cards) != 1 || len(todo.Cards[0].Subtasks) != 1 {
		t.Errorf("sub-task should have re-nested, got %+v", todo.Cards)
	}
}

// The epics board filters to epics, so child tasks must not appear as cards on it.
func TestEpicBoardShowsOnlyEpics(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	epic := mustCreate(t, s, NewTicket{Type: TypeEpic, Title: "Epic", StatusSlug: "todo"})
	mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Child", ParentKey: epic.Key, StatusSlug: "todo"})

	view, err := s.GetBoard(ctx, "demo", "epics")
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, c := range view.Columns {
		total += len(c.Cards)
	}
	if total != 1 {
		t.Fatalf("epics board should show exactly the 1 epic, got %d cards", total)
	}

	// ...but the epic's children are still reachable for the detail view.
	kids, err := s.Children(ctx, epic.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].Title != "Child" {
		t.Fatalf("want the child listed under the epic, got %+v", kids)
	}
}

func TestMoveCardOrdersWithinColumn(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	a := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "A", StatusSlug: "todo"})
	b := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "B", StatusSlug: "todo"})
	c := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "C", StatusSlug: "todo"})

	keysIn := func(view *BoardView, slug string) []string {
		for _, col := range view.Columns {
			if col.Status.Slug != slug {
				continue
			}
			out := make([]string, 0, len(col.Cards))
			for _, card := range col.Cards {
				out = append(out, card.Key)
			}
			return out
		}
		return nil
	}

	// Move C to the top of todo.
	view, err := s.MoveCard(ctx, "demo", "all-work", c.Key, "todo", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := keysIn(view, "todo"); got[0] != c.Key {
		t.Fatalf("want %s first, got %v", c.Key, got)
	}

	// Move A below B.
	view, err = s.MoveCard(ctx, "demo", "all-work", a.Key, "todo", b.Key)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{c.Key, b.Key, a.Key}
	got := keysIn(view, "todo")
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want order %v, got %v", want, got)
		}
	}

	// Moving across columns applies the new status and survives a reload.
	if _, err := s.MoveCard(ctx, "demo", "all-work", b.Key, "in-progress", ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.GetBoard(ctx, "demo", "all-work")
	if err != nil {
		t.Fatal(err)
	}
	if got := keysIn(reloaded, "in-progress"); len(got) != 1 || got[0] != b.Key {
		t.Fatalf("want %s in in-progress, got %v", b.Key, got)
	}
	if got := keysIn(reloaded, "todo"); len(got) != 2 {
		t.Fatalf("todo should have 2 cards left, got %v", got)
	}
}

func TestLabelFilteredBoard(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()

	app, err := s.CreateLabel(ctx, "app", "#00f")
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, NewTicket{Type: TypeTask, Title: "In app", StatusSlug: "todo", LabelIDs: []int64{app.ID}})
	mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Elsewhere", StatusSlug: "todo"})

	demo, err := s.GetProjectBySlug(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.CreateBoard(ctx, Board{Name: "App Layer", FilterType: TypeTask,
		ProjectID: &demo.ID, FilterLabels: []Label{*app}})
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := s.ListStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, st := range statuses {
		ids = append(ids, st.ID)
	}
	if err := s.SetBoardColumns(ctx, board.ID, ids); err != nil {
		t.Fatal(err)
	}

	view, err := s.GetBoard(ctx, "demo", "app-layer")
	if err != nil {
		t.Fatal(err)
	}
	total, title := 0, ""
	for _, col := range view.Columns {
		for _, card := range col.Cards {
			total++
			title = card.Title
		}
	}
	if total != 1 || title != "In app" {
		t.Fatalf("label filter should leave exactly the labelled ticket, got %d (%q)", total, title)
	}
}

func TestDuplicateLabelRejected(t *testing.T) {
	s := newSeededStore(t)
	ctx := context.Background()
	if _, err := s.CreateLabel(ctx, "dup", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLabel(ctx, "dup", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestKeysAreSequential(t *testing.T) {
	s := newSeededStore(t)
	a := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "one"})
	b := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "two"})
	if a.Key != "DEMO-1" || b.Key != "DEMO-2" {
		t.Fatalf("want DEMO-1/DEMO-2, got %s/%s", a.Key, b.Key)
	}
	if err := s.DeleteTicket(context.Background(), b.Key); err != nil {
		t.Fatal(err)
	}
	// Keys must not be reused after a delete.
	c := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "three"})
	if c.Key != "DEMO-3" {
		t.Fatalf("want DEMO-3 after deleting DEMO-2, got %s", c.Key)
	}
}
