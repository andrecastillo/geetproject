package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TicketPatch is a partial update. A nil field means "not supplied" and is left
// alone; this is what keeps PATCH from blanking fields the caller never
// mentioned. ParentKey pointing at "" clears the parent.
type TicketPatch struct {
	Title       *string
	Description *string
	Type        *string
	StatusSlug  *string
	ParentKey   *string
	ProjectSlug *string
	LabelIDs    *[]int64
}

// NewTicket is the input to CreateTicket.
type NewTicket struct {
	ProjectSlug string
	Type        string
	Title       string
	Description string
	StatusSlug  string
	ParentKey   string
	LabelIDs    []int64
}

// childTypeOf reports which ticket type may be parented by the given type.
func childTypeOf(parentType string) string {
	switch parentType {
	case TypeEpic:
		return TypeTask
	case TypeTask:
		return TypeSubtask
	default:
		return ""
	}
}

// validateParent enforces the three fixed levels: epics are roots, tasks may sit
// under an epic or stand alone, sub-tasks must have a task parent.
func validateParent(ticketType string, parent *Ticket) error {
	switch ticketType {
	case TypeEpic:
		if parent != nil {
			return fmt.Errorf("%w: an epic cannot have a parent", ErrInvalid)
		}
	case TypeTask:
		if parent != nil && parent.Type != TypeEpic {
			return fmt.Errorf("%w: a task's parent must be an epic, got %s", ErrInvalid, parent.Type)
		}
	case TypeSubtask:
		if parent == nil {
			return fmt.Errorf("%w: a sub-task must have a parent task", ErrInvalid)
		}
		if parent.Type != TypeTask {
			return fmt.Errorf("%w: a sub-task's parent must be a task, got %s", ErrInvalid, parent.Type)
		}
	default:
		return fmt.Errorf("%w: unknown ticket type %q", ErrInvalid, ticketType)
	}
	return nil
}

func (s *Store) CreateTicket(ctx context.Context, in NewTicket) (*Ticket, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	if in.Type == "" {
		in.Type = TypeTask
	}

	var parent *Ticket
	if in.ParentKey != "" {
		p, err := s.GetTicket(ctx, in.ParentKey)
		if err != nil {
			return nil, err
		}
		parent = p
	}
	if err := validateParent(in.Type, parent); err != nil {
		return nil, err
	}

	// A child always lives in its parent's project. Defaulting to it means the
	// caller can leave the project out entirely when adding a sub-task.
	projectSlug := in.ProjectSlug
	if parent != nil {
		if projectSlug != "" && projectSlug != parent.ProjectSlug {
			return nil, fmt.Errorf("%w: %s is in project %s, so its children must be too",
				ErrInvalid, parent.Key, parent.ProjectSlug)
		}
		projectSlug = parent.ProjectSlug
	}
	if projectSlug == "" || projectSlug == AllScope {
		return nil, fmt.Errorf("%w: a ticket needs a project", ErrInvalid)
	}
	project, err := s.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return nil, err
	}

	statusID, err := s.resolveStatus(ctx, in.StatusSlug)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	key, err := nextTicketKey(ctx, tx, project.ID)
	if err != nil {
		return nil, err
	}
	ts := now()
	var parentID any
	if parent != nil {
		parentID = parent.ID
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ticket (key, project_id, type, title, description, status_id, parent_id, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		key, project.ID, in.Type, strings.TrimSpace(in.Title), in.Description, statusID, parentID, ts, ts)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := setTicketLabels(ctx, tx, id, in.LabelIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetTicket(ctx, key)
}

// nextTicketKey allocates the next PREFIX-N from the project's counter. The
// counter only ever moves forward, so a deleted ticket's key is never handed to
// a different ticket later - which is what lets a "see MAI-2" written into a
// description stay meaningful. Keys are per project but not per type, so a
// ticket's key survives a change of type.
func nextTicketKey(ctx context.Context, tx *sql.Tx, projectID int64) (string, error) {
	if _, err := tx.ExecContext(ctx,
		`UPDATE project SET ticket_seq = ticket_seq + 1 WHERE id = ?`, projectID); err != nil {
		return "", err
	}
	var prefix string
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT prefix, ticket_seq FROM project WHERE id = ?`, projectID).Scan(&prefix, &n); err != nil {
		return "", err
	}
	return prefix + "-" + strconv.Itoa(n), nil
}

func (s *Store) resolveStatus(ctx context.Context, slug string) (int64, error) {
	if slug == "" {
		all, err := s.ListStatuses(ctx)
		if err != nil {
			return 0, err
		}
		if len(all) == 0 {
			return 0, errors.New("no statuses configured")
		}
		return all[0].ID, nil
	}
	st, err := s.statusBySlug(ctx, slug)
	if err != nil {
		return 0, err
	}
	return st.ID, nil
}

func (s *Store) GetTicket(ctx context.Context, key string) (*Ticket, error) {
	ts, err := s.queryTickets(ctx, `WHERE t.key = ?`, []any{key}, "")
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, fmt.Errorf("%w: ticket %s", ErrNotFound, key)
	}
	return &ts[0], nil
}

func (s *Store) UpdateTicket(ctx context.Context, key string, p TicketPatch) (*Ticket, error) {
	cur, err := s.GetTicket(ctx, key)
	if err != nil {
		return nil, err
	}

	newType := cur.Type
	if p.Type != nil {
		newType = *p.Type
	}

	// Resolve the parent we will end up with.
	var parent *Ticket
	if p.ParentKey != nil {
		if *p.ParentKey != "" {
			if *p.ParentKey == key {
				return nil, fmt.Errorf("%w: a ticket cannot be its own parent", ErrInvalid)
			}
			parent, err = s.GetTicket(ctx, *p.ParentKey)
			if err != nil {
				return nil, err
			}
		}
	} else if cur.ParentID != nil {
		parent, err = s.getTicketByID(ctx, *cur.ParentID)
		if err != nil {
			return nil, err
		}
	}
	if err := validateParent(newType, parent); err != nil {
		return nil, err
	}

	// Work out which project the ticket ends up in.
	targetProject := cur.ProjectID
	movingProject := false
	if p.ProjectSlug != nil && *p.ProjectSlug != "" && *p.ProjectSlug != cur.ProjectSlug {
		if *p.ProjectSlug == AllScope {
			return nil, fmt.Errorf("%w: %q is not a project", ErrInvalid, AllScope)
		}
		next, err := s.GetProjectBySlug(ctx, *p.ProjectSlug)
		if err != nil {
			return nil, err
		}
		targetProject = next.ID
		movingProject = true
	}
	// A ticket and its parent must share a project, so moving a child on its
	// own would strand it on neither project's board. Move the parent instead
	// and the whole subtree follows.
	if parent != nil && parent.ProjectID != targetProject {
		if movingProject {
			return nil, fmt.Errorf("%w: %s belongs to %s; move its parent %s to move it",
				ErrInvalid, key, cur.ProjectSlug, parent.Key)
		}
		return nil, fmt.Errorf("%w: %s is in a different project from %s",
			ErrInvalid, parent.Key, key)
	}

	// A type change would re-parent every child under a level that may not
	// accept them, so reject it while children exist rather than silently
	// leaving an illegal tree behind.
	if p.Type != nil && *p.Type != cur.Type {
		var kids int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM ticket WHERE parent_id = ?`, cur.ID).Scan(&kids); err != nil {
			return nil, err
		}
		if kids > 0 {
			return nil, fmt.Errorf("%w: cannot change type of %s while it has %d child ticket(s)",
				ErrInvalid, key, kids)
		}
	}

	sets := []string{}
	args := []any{}
	if p.Title != nil {
		if strings.TrimSpace(*p.Title) == "" {
			return nil, fmt.Errorf("%w: title cannot be empty", ErrInvalid)
		}
		sets, args = append(sets, "title = ?"), append(args, strings.TrimSpace(*p.Title))
	}
	if p.Description != nil {
		sets, args = append(sets, "description = ?"), append(args, *p.Description)
	}
	if p.Type != nil {
		sets, args = append(sets, "type = ?"), append(args, *p.Type)
	}
	if p.StatusSlug != nil {
		id, err := s.resolveStatus(ctx, *p.StatusSlug)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, "status_id = ?"), append(args, id)
	}
	if p.ParentKey != nil {
		if parent == nil {
			sets = append(sets, "parent_id = NULL")
		} else {
			sets, args = append(sets, "parent_id = ?"), append(args, parent.ID)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if movingProject {
		// Descendants move with their ancestor, or they would end up in a
		// project their parent is not in.
		if _, err := tx.ExecContext(ctx, `
			WITH RECURSIVE subtree(id) AS (
			  SELECT ? UNION ALL
			  SELECT t.id FROM ticket t JOIN subtree ON t.parent_id = subtree.id
			)
			UPDATE ticket SET project_id = ?, updated_at = ?
			WHERE id IN (SELECT id FROM subtree)`,
			cur.ID, targetProject, now()); err != nil {
			return nil, err
		}
	}

	if len(sets) > 0 {
		sets = append(sets, "updated_at = ?")
		args = append(args, now(), cur.ID)
		q := "UPDATE ticket SET " + strings.Join(sets, ", ") + " WHERE id = ?"
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return nil, err
		}
	}
	if p.LabelIDs != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM ticket_label WHERE ticket_id = ?`, cur.ID); err != nil {
			return nil, err
		}
		if err := setTicketLabels(ctx, tx, cur.ID, *p.LabelIDs); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE ticket SET updated_at = ? WHERE id = ?`, now(), cur.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetTicket(ctx, key)
}

// DeleteTicket removes a ticket; children and comments go with it via the
// schema's ON DELETE CASCADE.
func (s *Store) DeleteTicket(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ticket WHERE key = ?`, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: ticket %s", ErrNotFound, key)
	}
	return nil
}

// CountDescendants reports how many tickets would be removed alongside this one,
// so the UI can warn before a cascading delete.
func (s *Store) CountDescendants(ctx context.Context, key string) (int, error) {
	t, err := s.GetTicket(ctx, key)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.db.QueryRowContext(ctx, `
		WITH RECURSIVE d(id) AS (
			SELECT id FROM ticket WHERE parent_id = ?
			UNION ALL
			SELECT t.id FROM ticket t JOIN d ON t.parent_id = d.id
		) SELECT COUNT(*) FROM d`, t.ID).Scan(&n)
	return n, err
}

// Children returns the direct children of a ticket, ordered oldest first.
func (s *Store) Children(ctx context.Context, key string) ([]Ticket, error) {
	t, err := s.GetTicket(ctx, key)
	if err != nil {
		return nil, err
	}
	return s.queryTickets(ctx, `WHERE t.parent_id = ?`, []any{t.ID}, "ORDER BY t.id")
}

func (s *Store) getTicketByID(ctx context.Context, id int64) (*Ticket, error) {
	ts, err := s.queryTickets(ctx, `WHERE t.id = ?`, []any{id}, "")
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, fmt.Errorf("%w: ticket id %d", ErrNotFound, id)
	}
	return &ts[0], nil
}

func (s *Store) ListTickets(ctx context.Context, f TicketFilter) ([]Ticket, error) {
	where := []string{}
	args := []any{}

	// A nil ProjectID is the cross-project scope, so it adds no clause at all.
	if f.ProjectID != nil {
		where, args = append(where, "t.project_id = ?"), append(args, *f.ProjectID)
	}
	if f.Type != "" && f.Type != "any" {
		where, args = append(where, "t.type = ?"), append(args, f.Type)
	}
	if f.StatusID != 0 {
		where, args = append(where, "t.status_id = ?"), append(args, f.StatusID)
	}
	if f.ParentID != nil {
		where, args = append(where, "t.parent_id = ?"), append(args, *f.ParentID)
	}
	if f.HasParent != nil {
		if *f.HasParent {
			where = append(where, "t.parent_id IS NOT NULL")
		} else {
			where = append(where, "t.parent_id IS NULL")
		}
	}
	if f.Search != "" {
		where = append(where, "(t.title LIKE ? OR t.description LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like)
	}
	if len(f.LabelIDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.LabelIDs)), ",")
		sub := fmt.Sprintf(
			"SELECT COUNT(DISTINCT tl.label_id) FROM ticket_label tl WHERE tl.ticket_id = t.id AND tl.label_id IN (%s)", ph)
		for _, id := range f.LabelIDs {
			args = append(args, id)
		}
		if f.LabelMode == "all" {
			where = append(where, fmt.Sprintf("(%s) = %d", sub, len(f.LabelIDs)))
		} else {
			where = append(where, fmt.Sprintf("(%s) > 0", sub))
		}
	}

	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	return s.queryTickets(ctx, clause, args, "ORDER BY t.id")
}

// queryTickets is the single place ticket rows are read, so every caller gets
// the same joined status, parent breadcrumb and labels.
func (s *Store) queryTickets(ctx context.Context, where string, args []any, order string) ([]Ticket, error) {
	q := `
		SELECT t.id, t.key, t.type, t.title, t.description, t.status_id,
		       t.parent_id, COALESCE(p.key,''), COALESCE(p.title,''),
		       t.project_id, pr.slug, pr.name,
		       t.created_at, t.updated_at,
		       st.id, st.name, st.slug, st.color, st.position, st.is_done
		FROM ticket t
		JOIN status st ON st.id = t.status_id
		JOIN project pr ON pr.id = t.project_id
		LEFT JOIN ticket p ON p.id = t.parent_id
		` + where + " " + order

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Ticket{}
	ids := []int64{}
	for rows.Next() {
		var t Ticket
		var st Status
		var done int
		var parentID sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Key, &t.Type, &t.Title, &t.Description, &t.StatusID,
			&parentID, &t.ParentKey, &t.ParentTitle,
			&t.ProjectID, &t.ProjectSlug, &t.ProjectName,
			&t.CreatedAt, &t.UpdatedAt,
			&st.ID, &st.Name, &st.Slug, &st.Color, &st.Position, &done); err != nil {
			return nil, err
		}
		if parentID.Valid {
			v := parentID.Int64
			t.ParentID = &v
		}
		st.IsDone = done == 1
		t.Status = &st
		t.Labels = []Label{}
		out = append(out, t)
		ids = append(ids, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	byID, err := s.labelsForTickets(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if ls, ok := byID[out[i].ID]; ok {
			out[i].Labels = ls
		}
	}
	return out, nil
}
