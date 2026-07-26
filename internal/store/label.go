package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func setTicketLabels(ctx context.Context, ex execer, ticketID int64, labelIDs []int64) error {
	seen := map[int64]bool{}
	for _, id := range labelIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := ex.ExecContext(ctx,
			`INSERT OR IGNORE INTO ticket_label (ticket_id, label_id) VALUES (?,?)`,
			ticketID, id); err != nil {
			return fmt.Errorf("attach label %d: %w", id, err)
		}
	}
	return nil
}

func (s *Store) labelsForTickets(ctx context.Context, ticketIDs []int64) (map[int64][]Label, error) {
	if len(ticketIDs) == 0 {
		return map[int64][]Label{}, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ticketIDs)), ",")
	args := make([]any, 0, len(ticketIDs))
	for _, id := range ticketIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT tl.ticket_id, l.id, l.name, l.color
		FROM ticket_label tl JOIN label l ON l.id = tl.label_id
		WHERE tl.ticket_id IN (%s)
		ORDER BY l.name`, ph), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]Label{}
	for rows.Next() {
		var tid int64
		var l Label
		if err := rows.Scan(&tid, &l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out[tid] = append(out[tid], l)
	}
	return out, rows.Err()
}

func (s *Store) ListLabels(ctx context.Context) ([]Label, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, color FROM label ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) CreateLabel(ctx context.Context, name, color string) (*Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: label name is required", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO label (name, color) VALUES (?,?)`, name, color)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: label %q already exists", ErrConflict, name)
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Label{ID: id, Name: name, Color: color}, nil
}

func (s *Store) UpdateLabel(ctx context.Context, id int64, name, color *string) (*Label, error) {
	sets, args := []string{}, []any{}
	if name != nil {
		if strings.TrimSpace(*name) == "" {
			return nil, fmt.Errorf("%w: label name cannot be empty", ErrInvalid)
		}
		sets, args = append(sets, "name = ?"), append(args, strings.TrimSpace(*name))
	}
	if color != nil {
		sets, args = append(sets, "color = ?"), append(args, *color)
	}
	if len(sets) > 0 {
		args = append(args, id)
		if _, err := s.db.ExecContext(ctx,
			"UPDATE label SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			if isUniqueViolation(err) {
				return nil, fmt.Errorf("%w: label name already exists", ErrConflict)
			}
			return nil, err
		}
	}
	var l Label
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, color FROM label WHERE id = ?`, id).Scan(&l.ID, &l.Name, &l.Color)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: label %d", ErrNotFound, id)
	}
	return &l, err
}

func (s *Store) DeleteLabel(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM label WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: label %d", ErrNotFound, id)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
