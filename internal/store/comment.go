package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *Store) ListComments(ctx context.Context, ticketKey string) ([]Comment, error) {
	t, err := s.GetTicket(ctx, ticketKey)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ticket_id, body, created_at, updated_at
		FROM comment WHERE ticket_id = ? ORDER BY id`, t.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TicketID, &c.Body, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateComment(ctx context.Context, ticketKey, body string) (*Comment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: comment body is required", ErrInvalid)
	}
	t, err := s.GetTicket(ctx, ticketKey)
	if err != nil {
		return nil, err
	}
	ts := now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO comment (ticket_id, body, created_at, updated_at) VALUES (?,?,?,?)`,
		t.ID, body, ts, ts)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Comment{ID: id, TicketID: t.ID, Body: body, CreatedAt: ts, UpdatedAt: ts}, nil
}

func (s *Store) UpdateComment(ctx context.Context, id int64, body string) (*Comment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("%w: comment body is required", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE comment SET body = ?, updated_at = ? WHERE id = ?`, body, now(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("%w: comment %d", ErrNotFound, id)
	}
	var c Comment
	err = s.db.QueryRowContext(ctx,
		`SELECT id, ticket_id, body, created_at, updated_at FROM comment WHERE id = ?`, id).
		Scan(&c.ID, &c.TicketID, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: comment %d", ErrNotFound, id)
	}
	return &c, err
}

func (s *Store) DeleteComment(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM comment WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: comment %d", ErrNotFound, id)
	}
	return nil
}
