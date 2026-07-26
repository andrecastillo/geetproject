package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

// AllScope addresses the cross-project scope in place of a project slug. It is
// a reserved word: no project may take it, since /p/all is how the UI and the
// API name that scope.
const AllScope = "all"

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.slug, p.prefix, p.color, p.position, p.created_at,
		       (SELECT COUNT(*) FROM ticket t WHERE t.project_id = p.id)
		FROM project p ORDER BY p.position, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Prefix, &p.Color,
			&p.Position, &p.CreatedAt, &p.TicketCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (*Project, error) {
	var p Project
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.slug, p.prefix, p.color, p.position, p.created_at,
		       (SELECT COUNT(*) FROM ticket t WHERE t.project_id = p.id)
		FROM project p WHERE p.slug = ?`, slug).
		Scan(&p.ID, &p.Name, &p.Slug, &p.Prefix, &p.Color, &p.Position, &p.CreatedAt, &p.TicketCount)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, slug)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// resolveScope turns a scope name into a project id, or nil for AllScope. Every
// board and board-scoped query goes through it, so "all" is understood in
// exactly one place.
func (s *Store) resolveScope(ctx context.Context, scope string) (*int64, error) {
	if scope == "" || scope == AllScope {
		return nil, nil
	}
	p, err := s.GetProjectBySlug(ctx, scope)
	if err != nil {
		return nil, err
	}
	return &p.ID, nil
}

// derivePrefix builds a key prefix from a project name: "mini-kg" -> "MINIKG",
// "mai" -> "MAI". Callers can always override it.
func derivePrefix(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
		}
		if b.Len() >= 6 {
			break
		}
	}
	out := b.String()
	if len(out) < 2 {
		out += "PRJ"[:2-len(out)]
	}
	return out
}

func validPrefix(p string) error {
	if len(p) < 2 || len(p) > 6 {
		return fmt.Errorf("%w: prefix must be 2-6 characters, got %q", ErrInvalid, p)
	}
	for _, r := range p {
		if !unicode.IsUpper(r) && !unicode.IsDigit(r) {
			return fmt.Errorf("%w: prefix must be uppercase letters and digits, got %q", ErrInvalid, p)
		}
	}
	return nil
}

func (s *Store) CreateProject(ctx context.Context, in Project) (*Project, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, fmt.Errorf("%w: project name is required", ErrInvalid)
	}
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if in.Slug == "" {
		return nil, fmt.Errorf("%w: project name must contain a letter or digit", ErrInvalid)
	}
	if in.Slug == AllScope {
		return nil, fmt.Errorf("%w: %q is reserved for the cross-project view", ErrInvalid, AllScope)
	}
	if in.Prefix == "" {
		in.Prefix = derivePrefix(in.Name)
	}
	in.Prefix = strings.ToUpper(strings.TrimSpace(in.Prefix))
	if err := validPrefix(in.Prefix); err != nil {
		return nil, err
	}
	if in.Position == 0 {
		if err := s.db.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(position),0)+1 FROM project`).Scan(&in.Position); err != nil {
			return nil, err
		}
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO project (name, slug, prefix, color, position, ticket_seq, created_at)
		VALUES (?,?,?,?,?,0,?)`,
		in.Name, in.Slug, in.Prefix, in.Color, in.Position, now())
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a project with that name or prefix already exists", ErrConflict)
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	// A project with no views renders as a blank page, so give it the same
	// starting pair every scope gets.
	if err := s.seedDefaultViews(ctx, &id); err != nil {
		return nil, fmt.Errorf("seed views for %s: %w", in.Slug, err)
	}
	return s.GetProjectBySlug(ctx, in.Slug)
}

// ProjectPatch is a partial update; nil fields are left alone.
type ProjectPatch struct {
	Name     *string
	Prefix   *string
	Color    *string
	Position *int
}

func (s *Store) UpdateProject(ctx context.Context, slug string, p ProjectPatch) (*Project, error) {
	cur, err := s.GetProjectBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	sets, args := []string{}, []any{}
	if p.Name != nil {
		if strings.TrimSpace(*p.Name) == "" {
			return nil, fmt.Errorf("%w: project name cannot be empty", ErrInvalid)
		}
		sets, args = append(sets, "name = ?"), append(args, strings.TrimSpace(*p.Name))
	}
	if p.Prefix != nil {
		next := strings.ToUpper(strings.TrimSpace(*p.Prefix))
		if err := validPrefix(next); err != nil {
			return nil, err
		}
		// Existing keys keep the old prefix on purpose. Rewriting them would
		// break every reference already written into a description or comment,
		// which is the one thing ticket keys exist to survive.
		sets, args = append(sets, "prefix = ?"), append(args, next)
	}
	if p.Color != nil {
		sets, args = append(sets, "color = ?"), append(args, *p.Color)
	}
	if p.Position != nil {
		sets, args = append(sets, "position = ?"), append(args, *p.Position)
	}
	if len(sets) > 0 {
		args = append(args, cur.ID)
		if _, err := s.db.ExecContext(ctx,
			"UPDATE project SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			if isUniqueViolation(err) {
				return nil, fmt.Errorf("%w: that name or prefix is already taken", ErrConflict)
			}
			return nil, err
		}
	}
	return s.GetProjectBySlug(ctx, slug)
}

// DeleteProject removes a project and everything filed under it. It returns how
// many tickets went with it so callers can report what was destroyed.
func (s *Store) DeleteProject(ctx context.Context, slug string) (int, error) {
	p, err := s.GetProjectBySlug(ctx, slug)
	if err != nil {
		return 0, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM project WHERE id = ?`, p.ID); err != nil {
		return 0, err
	}
	return p.TicketCount, nil
}
