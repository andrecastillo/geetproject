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

const schemaVersion = 1

func (s *Store) migrate() error {
	var have int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&have); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Future schema changes append migration steps here, keyed off `have`.
	if have < schemaVersion {
		if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return fmt.Errorf("set user_version: %w", err)
		}
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

var defaultStatuses = []Status{
	{Name: "Backlog", Slug: "backlog", Color: "#94a3b8", Position: 1},
	{Name: "Todo", Slug: "todo", Color: "#60a5fa", Position: 2},
	{Name: "In Progress", Slug: "in-progress", Color: "#fbbf24", Position: 3},
	{Name: "Done", Slug: "done", Color: "#4ade80", Position: 4, IsDone: true},
}

// seed populates statuses and two starter boards on a fresh database so the UI
// is never an empty screen with no way forward.
func (s *Store) seed() error {
	ctx := context.Background()

	// Bring the key counter up to whatever is already in the table, so a
	// database created before the counter existed can't re-issue live keys.
	if err := s.initTicketSeq(); err != nil {
		return err
	}

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
		for i, b := range []struct{ name, slug, ftype string }{
			{"Epics", "epics", TypeEpic},
			{"All Tasks", "tasks", TypeTask},
		} {
			created, err := s.CreateBoard(ctx, Board{
				Name: b.name, Slug: b.slug, FilterType: b.ftype, Position: i + 1,
			})
			if err != nil {
				return fmt.Errorf("seed boards: %w", err)
			}
			// Every status is a column by default.
			all, err := s.ListStatuses(ctx)
			if err != nil {
				return err
			}
			ids := make([]int64, 0, len(all))
			for _, st := range all {
				ids = append(ids, st.ID)
			}
			if err := s.SetBoardColumns(ctx, created.ID, ids); err != nil {
				return fmt.Errorf("seed board columns: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) initTicketSeq() error {
	var have int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM meta WHERE key = 'ticket_seq'`).Scan(&have)
	if err != nil {
		return err
	}
	if have > 0 {
		return nil
	}
	rows, err := s.db.Query(`SELECT key FROM ticket`)
	if err != nil {
		return err
	}
	defer rows.Close()
	max := 0
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return err
		}
		if n, ok := parseTicketKey(k); ok && n > max {
			max = n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO meta (key, value) VALUES ('ticket_seq', ?)`, fmt.Sprint(max))
	return err
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
