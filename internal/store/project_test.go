package store

import (
	"context"
	"errors"
	"testing"
)

func TestKeySequencesAreIndependentPerProject(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s, "mai", "mai", "MAI")
	mustProject(t, s, "mini-kg", "mini-kg", "KG")

	a := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeTask, Title: "one"})
	b := mustCreate(t, s, NewTicket{ProjectSlug: "mini-kg", Type: TypeTask, Title: "two"})
	c := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeTask, Title: "three"})

	if a.Key != "MAI-1" || b.Key != "KG-1" || c.Key != "MAI-2" {
		t.Fatalf("want MAI-1/KG-1/MAI-2, got %s/%s/%s", a.Key, b.Key, c.Key)
	}

	// Deleting must not free a key for reuse, per project.
	if err := s.DeleteTicket(context.Background(), c.Key); err != nil {
		t.Fatal(err)
	}
	d := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeTask, Title: "four"})
	if d.Key != "MAI-3" {
		t.Fatalf("want MAI-3 after deleting MAI-2, got %s", d.Key)
	}
}

func TestPrefixDerivedAndValidated(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.CreateProject(ctx, Project{Name: "mini-kg"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Prefix != "MINIKG" || p.Slug != "mini-kg" {
		t.Fatalf("want MINIKG/mini-kg, got %s/%s", p.Prefix, p.Slug)
	}

	if _, err := s.CreateProject(ctx, Project{Name: "Other", Prefix: "x"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a one-character prefix should be rejected, got %v", err)
	}
	if _, err := s.CreateProject(ctx, Project{Name: "Other", Prefix: "TOO-LONG"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("an 8-character prefix should be rejected, got %v", err)
	}
	if _, err := s.CreateProject(ctx, Project{Name: "Another", Prefix: "MINIKG"}); !errors.Is(err, ErrConflict) {
		t.Errorf("a duplicate prefix should conflict, got %v", err)
	}
}

// 'all' addresses the cross-project scope in URLs, so no project may claim it.
func TestAllIsReservedAsAProjectSlug(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject(context.Background(),
		Project{Name: "All", Slug: "all", Prefix: "ALL"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid for the reserved slug, got %v", err)
	}
}

func TestBoardsAreScopedToTheirProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")
	mustProject(t, s, "mini-kg", "mini-kg", "KG")

	mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeTask, Title: "mai work", StatusSlug: "todo"})
	mustCreate(t, s, NewTicket{ProjectSlug: "mini-kg", Type: TypeTask, Title: "kg work", StatusSlug: "todo"})

	titles := func(v *BoardView) []string {
		out := []string{}
		for _, col := range v.Columns {
			for _, c := range col.Cards {
				out = append(out, c.Title)
			}
		}
		return out
	}

	mai, err := s.GetBoard(ctx, "mai", "all-work")
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(mai); len(got) != 1 || got[0] != "mai work" {
		t.Fatalf("the mai board should show only mai's ticket, got %v", got)
	}

	// The cross-project scope shows both, with each card naming its project.
	all, err := s.GetBoard(ctx, AllScope, "all-work")
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(all); len(got) != 2 {
		t.Fatalf("the all-projects board should show both tickets, got %v", got)
	}
	for _, col := range all.Columns {
		for _, c := range col.Cards {
			if c.ProjectSlug == "" {
				t.Errorf("card %s carries no project, so it cannot be labelled", c.Key)
			}
		}
	}
}

// Two projects must each be able to have a view called 'epics'.
func TestViewSlugsAreUniquePerScopeOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")
	mustProject(t, s, "mini-kg", "mini-kg", "KG")

	for _, scope := range []string{"mai", "mini-kg", AllScope} {
		if _, err := s.GetBoardBySlug(ctx, scope, "epics"); err != nil {
			t.Fatalf("%s should have its own 'epics' view: %v", scope, err)
		}
	}

	// Within one scope, though, the slug is still taken.
	mai, err := s.GetProjectBySlug(ctx, "mai")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateBoard(ctx, Board{
		Name: "Epics", Slug: "epics", ProjectID: &mai.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict for a duplicate slug in one project, got %v", err)
	}
}

func TestChildMustShareItsParentsProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")
	mustProject(t, s, "mini-kg", "mini-kg", "KG")

	epic := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeEpic, Title: "Epic"})

	if _, err := s.CreateTicket(ctx, NewTicket{
		ProjectSlug: "mini-kg", Type: TypeTask, Title: "wrong project", ParentKey: epic.Key,
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid creating a child in another project, got %v", err)
	}

	// Leaving the project out inherits the parent's.
	child := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "inherits", ParentKey: epic.Key})
	if child.ProjectSlug != "mai" {
		t.Fatalf("child should inherit mai, got %q", child.ProjectSlug)
	}

	// And a child cannot be moved out on its own.
	other := "mini-kg"
	if _, err := s.UpdateTicket(ctx, child.Key,
		TicketPatch{ProjectSlug: &other}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid moving a child alone, got %v", err)
	}
}

func TestMovingATicketMovesItsSubtree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")
	mustProject(t, s, "mini-kg", "mini-kg", "KG")

	epic := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeEpic, Title: "Epic"})
	task := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Task", ParentKey: epic.Key})
	sub := mustCreate(t, s, NewTicket{Type: TypeSubtask, Title: "Sub", ParentKey: task.Key})

	target := "mini-kg"
	if _, err := s.UpdateTicket(ctx, epic.Key, TicketPatch{ProjectSlug: &target}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{epic.Key, task.Key, sub.Key} {
		got, err := s.GetTicket(ctx, k)
		if err != nil {
			t.Fatal(err)
		}
		if got.ProjectSlug != "mini-kg" {
			t.Errorf("%s should have moved with its ancestor, still in %q", k, got.ProjectSlug)
		}
	}

	// Keys keep their original prefix: rewriting them would break every
	// reference already written down.
	if got, _ := s.GetTicket(ctx, "MAI-1"); got == nil {
		t.Error("moving a ticket must not change its key")
	}
}

func TestDeleteProjectCascades(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")
	mustProject(t, s, "keep", "keep", "KEEP")

	epic := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeEpic, Title: "Epic"})
	task := mustCreate(t, s, NewTicket{Type: TypeTask, Title: "Task", ParentKey: epic.Key})
	if _, err := s.CreateComment(ctx, task.Key, "note"); err != nil {
		t.Fatal(err)
	}
	survivor := mustCreate(t, s, NewTicket{ProjectSlug: "keep", Type: TypeTask, Title: "Survives"})

	n, err := s.DeleteProject(ctx, "mai")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("want a reported count of 2 deleted tickets, got %d", n)
	}
	for _, k := range []string{epic.Key, task.Key} {
		if _, err := s.GetTicket(ctx, k); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s should be gone with its project, got %v", k, err)
		}
	}
	if _, err := s.GetTicket(ctx, survivor.Key); err != nil {
		t.Errorf("another project's ticket must survive: %v", err)
	}

	// Its views and comments went too.
	var comments, boards int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM comment`).Scan(&comments); err != nil {
		t.Fatal(err)
	}
	if comments != 0 {
		t.Errorf("comments should have cascaded, %d remain", comments)
	}
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM board WHERE project_id IS NOT NULL`).Scan(&boards); err != nil {
		t.Fatal(err)
	}
	if boards != len(defaultViews) {
		t.Errorf("only the surviving project's %d views should remain, got %d",
			len(defaultViews), boards)
	}
}

func TestTicketNeedsARealProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	for _, slug := range []string{"", AllScope} {
		if _, err := s.CreateTicket(ctx, NewTicket{
			ProjectSlug: slug, Type: TypeTask, Title: "nowhere",
		}); !errors.Is(err, ErrInvalid) {
			t.Errorf("project %q should be rejected, got %v", slug, err)
		}
	}
	if _, err := s.CreateTicket(ctx, NewTicket{
		ProjectSlug: "nope", Type: TypeTask, Title: "nowhere",
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown project should be a not-found, got %v", err)
	}
}
