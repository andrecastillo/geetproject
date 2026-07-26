package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Sentinel errors the API layer maps onto status codes.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")
)

type Store struct {
	db *sql.DB
}

// Open connects to the SQLite file, applies the schema and seeds defaults.
//
// The pragmas go in the DSN rather than an Exec after connecting: they are
// per-connection settings, and database/sql keeps a pool, so an Exec would only
// configure whichever connection happened to serve it. foreign_keys in
// particular defaults to OFF in SQLite, which would silently turn every
// ON DELETE CASCADE in the schema into a no-op.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		url.PathEscape(path),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Single writer keeps a personal tracker free of lock contention entirely.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.seed(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for tests.
func (s *Store) DB() *sql.DB { return s.db }

const schemaVersion = 2

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Migrations key off what the tables actually look like rather than off
	// user_version alone, so re-running against an already-migrated database
	// is a no-op no matter how it got there.
	if err := s.migrateProjects(); err != nil {
		return fmt.Errorf("migrate to projects: %w", err)
	}
	// These depend on columns the migration above adds, so they cannot live in
	// schema.sql, which runs first.
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_ticket_project ON ticket(project_id)`,
		// Per-scope slug uniqueness. A plain UNIQUE(project_id, slug) would not
		// do: SQLite treats NULLs as distinct, so two cross-project views could
		// share a slug.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_board_scope_slug ON board(COALESCE(project_id, 0), slug)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}
	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	// Verify the pragma actually took, rather than trusting the DSN silently.
	var fk int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		return fmt.Errorf("read foreign_keys: %w", err)
	}
	if fk != 1 {
		return errors.New("foreign_keys pragma is off; cascading deletes would not work")
	}
	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrateProjects brings a pre-projects database forward: every ticket gains a
// required project, and boards gain an optional one (NULL being the
// cross-project scope).
//
// Both need a table rebuild rather than ALTER TABLE ADD COLUMN - ticket because
// SQLite cannot add a NOT NULL column without a default, and board because its
// slug carried a global UNIQUE constraint that has to go for two projects to
// each have a view called 'epics'.
func (s *Store) migrateProjects() error {
	ticketHas, err := s.hasColumn("ticket", "project_id")
	if err != nil {
		return err
	}
	boardHas, err := s.hasColumn("board", "project_id")
	if err != nil {
		return err
	}
	if ticketHas && boardHas {
		return nil
	}

	// PRAGMA is a no-op inside a transaction, so these toggle outside one.
	// legacy_alter_table stops RENAME from rewriting other tables' REFERENCES
	// clauses, which is what the documented rebuild procedure relies on.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`PRAGMA legacy_alter_table = ON`); err != nil {
		return err
	}
	defer func() {
		s.db.Exec(`PRAGMA legacy_alter_table = OFF`)
		s.db.Exec(`PRAGMA foreign_keys = ON`)
	}()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if !ticketHas {
		var existing int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM ticket`).Scan(&existing); err != nil {
			return err
		}
		// Only invent a home for tickets that actually exist. A database with
		// no tickets should come out the other side with no projects at all.
		var defaultProject int64
		if existing > 0 {
			res, err := tx.Exec(`
				INSERT INTO project (name, slug, prefix, color, position, ticket_seq, created_at)
				VALUES ('General', 'general', 'GEN', '#60a5fa', 1, 0, ?)`, now())
			if err != nil {
				return err
			}
			if defaultProject, err = res.LastInsertId(); err != nil {
				return err
			}
		}

		for _, stmt := range []string{
			`CREATE TABLE ticket_rebuild (
			   id          INTEGER PRIMARY KEY,
			   key         TEXT UNIQUE NOT NULL,
			   project_id  INTEGER NOT NULL REFERENCES project(id) ON DELETE CASCADE,
			   type        TEXT NOT NULL CHECK (type IN ('epic','task','subtask')),
			   title       TEXT NOT NULL,
			   description TEXT NOT NULL DEFAULT '',
			   status_id   INTEGER NOT NULL REFERENCES status(id),
			   parent_id   INTEGER REFERENCES ticket(id) ON DELETE CASCADE,
			   created_at  TEXT NOT NULL,
			   updated_at  TEXT NOT NULL)`,
		} {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`
			INSERT INTO ticket_rebuild
			  (id, key, project_id, type, title, description, status_id, parent_id, created_at, updated_at)
			SELECT id, key, ?, type, title, description, status_id, parent_id, created_at, updated_at
			FROM ticket`, defaultProject); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE ticket`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE ticket_rebuild RENAME TO ticket`); err != nil {
			return err
		}

		if existing > 0 {
			// Keep the number, change the prefix, so a 'see T-2' already
			// written into a description still points at the same ticket.
			if _, err := tx.Exec(
				`UPDATE ticket SET key = 'GEN-' || SUBSTR(key, 3) WHERE key LIKE 'T-%'`); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE project SET ticket_seq = (
				  SELECT COALESCE(MAX(CAST(SUBSTR(key, 5) AS INTEGER)), 0)
				  FROM ticket WHERE ticket.project_id = project.id
				) WHERE id = ?`, defaultProject); err != nil {
				return err
			}
		}
	}

	if !boardHas {
		if _, err := tx.Exec(`
			CREATE TABLE board_rebuild (
			  id                INTEGER PRIMARY KEY,
			  name              TEXT NOT NULL,
			  slug              TEXT NOT NULL,
			  project_id        INTEGER REFERENCES project(id) ON DELETE CASCADE,
			  filter_type       TEXT NOT NULL DEFAULT 'any'
			                    CHECK (filter_type IN ('epic','task','subtask','any')),
			  filter_label_mode TEXT NOT NULL DEFAULT 'any'
			                    CHECK (filter_label_mode IN ('any','all')),
			  position          INTEGER NOT NULL,
			  created_at        TEXT NOT NULL)`); err != nil {
			return err
		}
		// Boards were global before projects existed, so that is what they
		// honestly still are: the cross-project views.
		if _, err := tx.Exec(`
			INSERT INTO board_rebuild
			  (id, name, slug, project_id, filter_type, filter_label_mode, position, created_at)
			SELECT id, name, slug, NULL, filter_type, filter_label_mode, position, created_at
			FROM board`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DROP TABLE board`); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE board_rebuild RENAME TO board`); err != nil {
			return err
		}
	}

	// The global key counter is now per project.
	if _, err := tx.Exec(`DELETE FROM meta WHERE key = 'ticket_seq'`); err != nil {
		return err
	}
	return tx.Commit()
}

var defaultStatuses = []Status{
	{Name: "Backlog", Slug: "backlog", Color: "#94a3b8", Position: 1},
	{Name: "Todo", Slug: "todo", Color: "#60a5fa", Position: 2},
	{Name: "In Progress", Slug: "in-progress", Color: "#fbbf24", Position: 3},
	{Name: "Done", Slug: "done", Color: "#4ade80", Position: 4, IsDone: true},
}

// seed populates statuses and the cross-project views on a fresh database.
//
// It deliberately creates no projects: a brand new install starts empty and the
// sidebar prompts for the first one. Only a migrated database gets a project
// invented for it, because its tickets have to live somewhere.
func (s *Store) seed() error {
	ctx := context.Background()

	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM status").Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		for _, st := range defaultStatuses {
			if _, err := s.db.Exec(
				`INSERT INTO status (name, slug, color, position, is_done) VALUES (?,?,?,?,?)`,
				st.Name, st.Slug, st.Color, st.Position, boolToInt(st.IsDone),
			); err != nil {
				return fmt.Errorf("seed statuses: %w", err)
			}
		}
	}

	if err := s.db.QueryRow("SELECT COUNT(*) FROM board").Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if err := s.seedDefaultViews(ctx, nil); err != nil {
			return fmt.Errorf("seed cross-project views: %w", err)
		}
	}
	return nil
}

// seedDefaultViews gives a scope the two views every scope wants: everything,
// and epics only. projectID nil means the cross-project scope. Shared by seed()
// and CreateProject so a new project is usable the moment it exists.
func (s *Store) seedDefaultViews(ctx context.Context, projectID *int64) error {
	statuses, err := s.ListStatuses(ctx)
	if err != nil {
		return err
	}
	statusIDs := make([]int64, 0, len(statuses))
	for _, st := range statuses {
		statusIDs = append(statusIDs, st.ID)
	}

	for i, b := range []struct{ name, slug, ftype string }{
		{"All", "all-work", "any"},
		{"Epics", "epics", TypeEpic},
	} {
		created, err := s.CreateBoard(ctx, Board{
			Name: b.name, Slug: b.slug, FilterType: b.ftype,
			Position: i + 1, ProjectID: projectID,
		})
		if err != nil {
			return err
		}
		if err := s.SetBoardColumns(ctx, created.ID, statusIDs); err != nil {
			return err
		}
	}
	return nil
}

// ---- statuses ----

func (s *Store) ListStatuses(ctx context.Context) ([]Status, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, color, position, is_done FROM status ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Status{}
	for rows.Next() {
		var st Status
		var done int
		if err := rows.Scan(&st.ID, &st.Name, &st.Slug, &st.Color, &st.Position, &done); err != nil {
			return nil, err
		}
		st.IsDone = done == 1
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) statusBySlug(ctx context.Context, slug string) (*Status, error) {
	all, err := s.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Slug == slug {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("%w: status %q", ErrNotFound, slug)
}

// ---- helpers ----

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
