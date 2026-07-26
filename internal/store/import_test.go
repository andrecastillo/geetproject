package store

import (
	"context"
	"errors"
	"testing"
)

func sampleBatch() []ImportNode {
	return []ImportNode{{
		Type: TypeEpic, Title: "Chat UI", Description: "Front the KG with a chat interface.",
		Labels: []string{"app"},
		Children: []ImportNode{{
			Title: "Flask /chat route", Status: "in-progress",
			Children: []ImportNode{
				{Title: "Stream tokens"},
				{Title: "Wire up the tool schema", Labels: []string{"app", "backend"}},
			},
		}},
	}}
}

func TestImportCreatesTheWholeTree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	got, err := s.Import(ctx, "mai", sampleBatch(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 created tickets, got %d: %+v", len(got), got)
	}

	want := []struct {
		key, ttype string
		depth      int
	}{
		{"MAI-1", TypeEpic, 0},
		{"MAI-2", TypeTask, 1},
		{"MAI-3", TypeSubtask, 2},
		{"MAI-4", TypeSubtask, 2},
	}
	for i, w := range want {
		if got[i].Key != w.key || got[i].Type != w.ttype || got[i].Depth != w.depth {
			t.Errorf("row %d: want %s/%s/depth %d, got %s/%s/depth %d",
				i, w.key, w.ttype, w.depth, got[i].Key, got[i].Type, got[i].Depth)
		}
	}

	// Types were inferred from depth where they were left out.
	task, err := s.GetTicket(ctx, "MAI-2")
	if err != nil {
		t.Fatal(err)
	}
	if task.Type != TypeTask || task.Status.Slug != "in-progress" || task.ParentKey != "MAI-1" {
		t.Errorf("inferred task is wrong: %+v", task)
	}

	// Descriptions and labels came through, and a new label was created.
	epic, err := s.GetTicket(ctx, "MAI-1")
	if err != nil {
		t.Fatal(err)
	}
	if epic.Description != "Front the KG with a chat interface." {
		t.Errorf("description lost: %q", epic.Description)
	}
	if len(epic.Labels) != 1 || epic.Labels[0].Name != "app" {
		t.Errorf("labels lost: %+v", epic.Labels)
	}
	sub, err := s.GetTicket(ctx, "MAI-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Labels) != 2 {
		t.Errorf("want both labels on MAI-4, got %+v", sub.Labels)
	}
}

// A dry run must report exactly what a real run would then do, and leave
// nothing behind - including no advance of the key counter.
func TestImportDryRunWritesNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	preview, err := s.Import(ctx, "mai", sampleBatch(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 4 {
		t.Fatalf("want a 4-ticket preview, got %d", len(preview))
	}

	tickets, err := s.ListTickets(ctx, TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 0 {
		t.Fatalf("a dry run must write nothing, found %d tickets", len(tickets))
	}
	labels, err := s.ListLabels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 0 {
		t.Fatalf("a dry run must not create labels, found %+v", labels)
	}

	// The real run produces the very keys the preview promised.
	real, err := s.Import(ctx, "mai", sampleBatch(), false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range preview {
		if preview[i].Key != real[i].Key {
			t.Errorf("row %d: preview said %s, real run made %s", i, preview[i].Key, real[i].Key)
		}
	}
}

func TestImportIsAllOrNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	// Seed one good ticket so we can prove the failed batch didn't disturb it.
	mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeTask, Title: "pre-existing"})

	// Legal at the top, illegal deeper down: a sub-task cannot hold children.
	bad := []ImportNode{{
		Type: TypeEpic, Title: "Fine so far",
		Children: []ImportNode{{
			Title: "Also fine",
			Children: []ImportNode{{
				Title: "Not fine", Children: []ImportNode{{Title: "too deep"}},
			}},
		}},
	}}
	if _, err := s.Import(ctx, "mai", bad, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid for an over-deep tree, got %v", err)
	}

	tickets, err := s.ListTickets(ctx, TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].Title != "pre-existing" {
		t.Fatalf("a rejected batch must leave nothing behind, found %+v", tickets)
	}

	// And the key counter did not move, so the next ticket is MAI-2.
	next := mustCreate(t, s, NewTicket{ProjectSlug: "mai", Type: TypeTask, Title: "after"})
	if next.Key != "MAI-2" {
		t.Errorf("a rejected batch burned keys: next was %s, want MAI-2", next.Key)
	}
}

func TestImportReportsEveryProblemAtOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	bad := []ImportNode{
		{Type: TypeEpic, Title: ""},
		{Type: "story", Title: "wrong type"},
		{Type: TypeTask, Title: "bad status", Status: "nope"},
	}
	_, err := s.Import(ctx, "mai", bad, false)
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("want an ImportError, got %v", err)
	}
	if len(ie.Problems) != 3 {
		t.Fatalf("want all 3 problems reported at once, got %d: %v", len(ie.Problems), ie.Problems)
	}
	// Each one names where it is, so a model can fix the right node.
	for _, p := range ie.Problems {
		if p[:8] != "tickets[" {
			t.Errorf("problem should point at a node, got %q", p)
		}
	}
	if !errors.Is(err, ErrInvalid) {
		t.Error("an ImportError should match ErrInvalid like other validation failures")
	}
}

func TestImportInfersTypesFromShape(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	// A flat list with no types and no children is a list of tasks, not epics.
	got, err := s.Import(ctx, "mai", []ImportNode{
		{Title: "one"}, {Title: "two"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.Type != TypeTask {
			t.Errorf("a childless top-level ticket should be a task, got %s", r.Type)
		}
	}

	// A top-level ticket with children is an epic.
	got, err = s.Import(ctx, "mai", []ImportNode{
		{Title: "parent", Children: []ImportNode{{Title: "child"}}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Type != TypeEpic || got[1].Type != TypeTask {
		t.Errorf("want epic then task, got %s then %s", got[0].Type, got[1].Type)
	}
}

func TestImportNeedsARealProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustProject(t, s, "mai", "mai", "MAI")

	for _, slug := range []string{"", AllScope} {
		if _, err := s.Import(ctx, slug, sampleBatch(), false); !errors.Is(err, ErrInvalid) {
			t.Errorf("project %q should be rejected, got %v", slug, err)
		}
	}
	if _, err := s.Import(ctx, "nope", sampleBatch(), false); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown project should be a not-found, got %v", err)
	}
	if _, err := s.Import(ctx, "mai", nil, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("an empty batch should be rejected, got %v", err)
	}
}
