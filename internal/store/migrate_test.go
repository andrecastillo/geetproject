package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// schemaV1 is the schema as it shipped before projects existed. Kept verbatim
// so the migration can be tested against a database that really looks like an
// old one, rather than against an approximation of it.
const schemaV1 = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE status (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, slug TEXT UNIQUE NOT NULL,
  color TEXT NOT NULL DEFAULT '', position INTEGER NOT NULL,
  is_done INTEGER NOT NULL DEFAULT 0);
CREATE TABLE ticket (
  id INTEGER PRIMARY KEY, key TEXT UNIQUE NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('epic','task','subtask')),
  title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
  status_id INTEGER NOT NULL REFERENCES status(id),
  parent_id INTEGER REFERENCES ticket(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE board (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, slug TEXT UNIQUE NOT NULL,
  filter_type TEXT NOT NULL DEFAULT 'any'
    CHECK (filter_type IN ('epic','task','subtask','any')),
  filter_label_mode TEXT NOT NULL DEFAULT 'any'
    CHECK (filter_label_mode IN ('any','all')),
  position INTEGER NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE board_label (
  board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES label(id) ON DELETE CASCADE,
  PRIMARY KEY (board_id, label_id));
CREATE TABLE board_status (
  board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
  status_id INTEGER NOT NULL REFERENCES status(id) ON DELETE CASCADE,
  position INTEGER NOT NULL, PRIMARY KEY (board_id, status_id));
CREATE TABLE card_order (
  board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
  ticket_id INTEGER NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
  position REAL NOT NULL, PRIMARY KEY (board_id, ticket_id));
CREATE TABLE label (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL,
  color TEXT NOT NULL DEFAULT '');
CREATE TABLE ticket_label (
  ticket_id INTEGER NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES label(id) ON DELETE CASCADE,
  PRIMARY KEY (ticket_id, label_id));
CREATE TABLE comment (
  id INTEGER PRIMARY KEY,
  ticket_id INTEGER NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
  body TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
PRAGMA user_version = 1;
`

// writeV1DB builds a database that looks exactly like a pre-projects install
// with real content in it.
func writeV1DB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schemaV1); err != nil {
		t.Fatalf("v1 schema: %v", err)
	}
	for i, s := range defaultStatuses {
		if _, err := db.Exec(
			`INSERT INTO status (id, name, slug, color, position, is_done) VALUES (?,?,?,?,?,?)`,
			i+1, s.Name, s.Slug, s.Color, s.Position, boolToInt(s.IsDone)); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range []struct {
		id                     int
		name, slug, filterType string
		pos                    int
	}{
		{1, "Epics", "epics", "epic", 1},
		{2, "All Tasks", "tasks", "task", 2},
	} {
		if _, err := db.Exec(
			`INSERT INTO board (id, name, slug, filter_type, filter_label_mode, position, created_at)
			 VALUES (?,?,?,?,'any',?,'2026-07-26T00:00:00Z')`,
			b.id, b.name, b.slug, b.filterType, b.pos); err != nil {
			t.Fatal(err)
		}
		for s := 1; s <= 4; s++ {
			if _, err := db.Exec(
				`INSERT INTO board_status (board_id, status_id, position) VALUES (?,?,?)`,
				b.id, s, s); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO label (id, name, color) VALUES (1,'infra','#f00')`); err != nil {
		t.Fatal(err)
	}
	// An epic, a task under it, a sub-task under that, a label and a comment.
	rows := []struct {
		id           int
		key, ttype   string
		title        string
		statusID     int
		parent       any
		descriptions string
	}{
		{1, "T-1", "epic", "Move mini-kg into mai", 2, nil, "see T-2 for the git decision"},
		{2, "T-2", "task", "Decide git history strategy", 2, 1, ""},
		{3, "T-3", "subtask", "Compare subtree vs submodule", 4, 2, ""},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			`INSERT INTO ticket (id, key, type, title, description, status_id, parent_id, created_at, updated_at)
			 VALUES (?,?,?,?,?,?,?, '2026-07-26T00:00:00Z','2026-07-26T00:00:00Z')`,
			r.id, r.key, r.ttype, r.title, r.descriptions, r.statusID, r.parent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO ticket_label (ticket_id, label_id) VALUES (2,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO comment (ticket_id, body, created_at, updated_at)
		 VALUES (2,'leaning subtree','2026-07-26T00:00:00Z','2026-07-26T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('ticket_seq','3')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO card_order (board_id, ticket_id, position) VALUES (2,2,1.0)`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	writeV1DB(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open a v1 database: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// Everything landed in a single default project.
	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Slug != "general" {
		t.Fatalf("want one 'general' project, got %+v", projects)
	}

	// Tickets were re-keyed, keeping their numbers so references still resolve.
	for _, want := range []struct{ key, title string }{
		{"GEN-1", "Move mini-kg into mai"},
		{"GEN-2", "Decide git history strategy"},
		{"GEN-3", "Compare subtree vs submodule"},
	} {
		got, err := s.GetTicket(ctx, want.key)
		if err != nil {
			t.Fatalf("ticket %s missing after migration: %v", want.key, err)
		}
		if got.Title != want.title {
			t.Errorf("%s: want title %q, got %q", want.key, want.title, got.Title)
		}
		if got.ProjectSlug != "general" {
			t.Errorf("%s: want project general, got %q", want.key, got.ProjectSlug)
		}
	}

	// The hierarchy, labels and comments came through intact.
	kids, err := s.Children(ctx, "GEN-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0].Key != "GEN-2" {
		t.Fatalf("want GEN-2 as the child of GEN-1, got %+v", kids)
	}
	tk, err := s.GetTicket(ctx, "GEN-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Labels) != 1 || tk.Labels[0].Name != "infra" {
		t.Errorf("labels lost in migration: %+v", tk.Labels)
	}
	comments, err := s.ListComments(ctx, "GEN-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Body != "leaning subtree" {
		t.Errorf("comments lost in migration: %+v", comments)
	}

	// The old global boards survive as cross-project views, columns and all.
	globals, err := s.ListBoards(ctx, AllScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(globals) != 2 {
		t.Fatalf("want the 2 old boards as global views, got %+v", globals)
	}
	view, err := s.GetBoard(ctx, AllScope, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Columns) != 4 {
		t.Fatalf("want 4 columns preserved, got %d", len(view.Columns))
	}

	// The counter continues from the highest migrated key rather than restarting.
	next, err := s.CreateTicket(ctx, NewTicket{
		ProjectSlug: "general", Type: TypeTask, Title: "after migration"})
	if err != nil {
		t.Fatal(err)
	}
	if next.Key != "GEN-4" {
		t.Fatalf("want GEN-4 after migrating 3 tickets, got %s", next.Key)
	}

	// And the database is structurally sound after the table rebuilds.
	var orphans int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("foreign key check found %d violations after migration", orphans)
	}
}

// Reopening must be a no-op: the migration has to be safe to run against a
// database it already migrated.
func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	writeV1DB(t, path)

	for i := range 3 {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		projects, err := s.ListProjects(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(projects) != 1 {
			t.Fatalf("open #%d: want 1 project, got %d", i+1, len(projects))
		}
		var tickets int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM ticket`).Scan(&tickets); err != nil {
			t.Fatal(err)
		}
		if tickets != 3 {
			t.Fatalf("open #%d: want 3 tickets, got %d", i+1, tickets)
		}
		s.Close()
	}
}

// A fresh database must not invent a default project - it starts empty, with
// only the cross-project views seeded.
func TestFreshDatabaseHasNoProjects(t *testing.T) {
	s := newTestStore(t)
	projects, err := s.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("a fresh database should have no projects, got %+v", projects)
	}
	globals, err := s.ListBoards(context.Background(), AllScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(globals) == 0 {
		t.Fatal("a fresh database should seed the cross-project views")
	}
	fmt.Fprint(nopWriter{}, globals)
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
