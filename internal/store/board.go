package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

const boardColumns = `b.id, b.name, b.slug, b.project_id, COALESCE(pr.slug, '` + AllScope + `'),
	b.filter_type, b.filter_label_mode, b.position, b.created_at`

func scanBoard(sc interface{ Scan(...any) error }) (Board, error) {
	var b Board
	var projectID sql.NullInt64
	if err := sc.Scan(&b.ID, &b.Name, &b.Slug, &projectID, &b.ProjectSlug,
		&b.FilterType, &b.FilterLabelMode, &b.Position, &b.CreatedAt); err != nil {
		return b, err
	}
	if projectID.Valid {
		v := projectID.Int64
		b.ProjectID = &v
	}
	b.FilterLabels = []Label{}
	return b, nil
}

// ListBoards returns the views in one scope: a project slug, or AllScope for
// the cross-project views.
func (s *Store) ListBoards(ctx context.Context, scope string) ([]Board, error) {
	projectID, err := s.resolveScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	where := "WHERE b.project_id IS NULL"
	args := []any{}
	if projectID != nil {
		where, args = "WHERE b.project_id = ?", []any{*projectID}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+boardColumns+`
		FROM board b LEFT JOIN project pr ON pr.id = b.project_id
		`+where+` ORDER BY b.position, b.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Board{}
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ls, err := s.boardFilterLabels(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].FilterLabels = ls
	}
	return out, nil
}

// GetBoardBySlug looks a view up within a scope. Board slugs are only unique
// per scope, so the scope is not optional.
func (s *Store) GetBoardBySlug(ctx context.Context, scope, slug string) (*Board, error) {
	projectID, err := s.resolveScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	where := "WHERE b.slug = ? AND b.project_id IS NULL"
	args := []any{slug}
	if projectID != nil {
		where, args = "WHERE b.slug = ? AND b.project_id = ?", []any{slug, *projectID}
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT `+boardColumns+`
		FROM board b LEFT JOIN project pr ON pr.id = b.project_id `+where, args...)
	b, err := scanBoard(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: view %s in %s", ErrNotFound, slug, scope)
	}
	if err != nil {
		return nil, err
	}
	ls, err := s.boardFilterLabels(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	b.FilterLabels = ls
	return &b, nil
}

func (s *Store) boardFilterLabels(ctx context.Context, boardID int64) ([]Label, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.name, l.color FROM board_label bl
		JOIN label l ON l.id = bl.label_id WHERE bl.board_id = ? ORDER BY l.name`, boardID)
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

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Store) CreateBoard(ctx context.Context, in Board) (*Board, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, fmt.Errorf("%w: board name is required", ErrInvalid)
	}
	if in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if in.Slug == "" {
		return nil, fmt.Errorf("%w: board name must contain a letter or digit", ErrInvalid)
	}
	if in.FilterType == "" {
		in.FilterType = "any"
	}
	if in.FilterLabelMode == "" {
		in.FilterLabelMode = "any"
	}
	if in.Position == 0 {
		// Position is per scope, so a new view in one project doesn't inherit
		// an ordering number from an unrelated project's views.
		var err error
		if in.ProjectID == nil {
			err = s.db.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(position),0)+1 FROM board WHERE project_id IS NULL`).Scan(&in.Position)
		} else {
			err = s.db.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(position),0)+1 FROM board WHERE project_id = ?`,
				*in.ProjectID).Scan(&in.Position)
		}
		if err != nil {
			return nil, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO board (name, slug, project_id, filter_type, filter_label_mode, position, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		strings.TrimSpace(in.Name), in.Slug, in.ProjectID,
		in.FilterType, in.FilterLabelMode, in.Position, now())
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: a view called %q already exists here", ErrConflict, in.Slug)
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	for _, l := range in.FilterLabels {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO board_label (board_id, label_id) VALUES (?,?)`, id, l.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getBoardByID(ctx, id)
}

func (s *Store) getBoardByID(ctx context.Context, id int64) (*Board, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+boardColumns+`
		FROM board b LEFT JOIN project pr ON pr.id = b.project_id WHERE b.id = ?`, id)
	b, err := scanBoard(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: view %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	ls, err := s.boardFilterLabels(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	b.FilterLabels = ls
	return &b, nil
}

// BoardPatch is a partial board update; nil fields are left alone.
type BoardPatch struct {
	Name            *string
	FilterType      *string
	FilterLabelMode *string
	FilterLabelIDs  *[]int64
}

func (s *Store) UpdateBoard(ctx context.Context, scope, slug string, p BoardPatch) (*Board, error) {
	b, err := s.GetBoardBySlug(ctx, scope, slug)
	if err != nil {
		return nil, err
	}
	sets, args := []string{}, []any{}
	if p.Name != nil {
		if strings.TrimSpace(*p.Name) == "" {
			return nil, fmt.Errorf("%w: board name cannot be empty", ErrInvalid)
		}
		sets, args = append(sets, "name = ?"), append(args, strings.TrimSpace(*p.Name))
	}
	if p.FilterType != nil {
		sets, args = append(sets, "filter_type = ?"), append(args, *p.FilterType)
	}
	if p.FilterLabelMode != nil {
		sets, args = append(sets, "filter_label_mode = ?"), append(args, *p.FilterLabelMode)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if len(sets) > 0 {
		args = append(args, b.ID)
		if _, err := tx.ExecContext(ctx,
			"UPDATE board SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			return nil, err
		}
	}
	if p.FilterLabelIDs != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM board_label WHERE board_id = ?`, b.ID); err != nil {
			return nil, err
		}
		for _, id := range *p.FilterLabelIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO board_label (board_id, label_id) VALUES (?,?)`, b.ID, id); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getBoardByID(ctx, b.ID)
}

func (s *Store) DeleteBoard(ctx context.Context, scope, slug string) error {
	b, err := s.GetBoardBySlug(ctx, scope, slug)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM board WHERE id = ?`, b.ID)
	return err
}

// BoardColumns returns the statuses this board shows, in column order.
func (s *Store) BoardColumns(ctx context.Context, boardID int64) ([]Status, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.id, st.name, st.slug, st.color, st.position, st.is_done
		FROM board_status bs JOIN status st ON st.id = bs.status_id
		WHERE bs.board_id = ? ORDER BY bs.position, st.position`, boardID)
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

func (s *Store) SetBoardColumns(ctx context.Context, boardID int64, statusIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM board_status WHERE board_id = ?`, boardID); err != nil {
		return err
	}
	for i, id := range statusIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO board_status (board_id, status_id, position) VALUES (?,?,?)`,
			boardID, id, i+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetBoard assembles a board into columns of cards.
//
// This is the only place the nesting rule lives, so the web UI and the CLI can
// never disagree about it: a sub-task renders inside its parent's card while it
// shares the parent's status, and becomes a card of its own the moment it
// doesn't.
func (s *Store) GetBoard(ctx context.Context, scope, slug string) (*BoardView, error) {
	b, err := s.GetBoardBySlug(ctx, scope, slug)
	if err != nil {
		return nil, err
	}
	statuses, err := s.BoardColumns(ctx, b.ID)
	if err != nil {
		return nil, err
	}

	labelIDs := make([]int64, 0, len(b.FilterLabels))
	for _, l := range b.FilterLabels {
		labelIDs = append(labelIDs, l.ID)
	}
	// b.ProjectID nil is the cross-project scope, which the filter reads as
	// "every project".
	matched, err := s.ListTickets(ctx, TicketFilter{
		ProjectID: b.ProjectID,
		Type:      b.FilterType,
		LabelIDs:  labelIDs,
		LabelMode: b.FilterLabelMode,
	})
	if err != nil {
		return nil, err
	}

	// The universe is what the filter matched plus the sub-tasks of those
	// tickets, so sub-tasks ride along even when they don't match the filter
	// themselves.
	universe := map[int64]*Ticket{}
	for i := range matched {
		universe[matched[i].ID] = &matched[i]
	}
	parentIDs := make([]int64, 0, len(matched))
	for _, t := range matched {
		parentIDs = append(parentIDs, t.ID)
	}
	subs, err := s.subtasksOf(ctx, parentIDs)
	if err != nil {
		return nil, err
	}
	for i := range subs {
		if _, ok := universe[subs[i].ID]; !ok {
			universe[subs[i].ID] = &subs[i]
		}
	}

	isColumn := map[int64]bool{}
	for _, st := range statuses {
		isColumn[st.ID] = true
	}

	// nested(t): t hides inside its parent's card.
	nested := func(t *Ticket) (*Ticket, bool) {
		if t.Type != TypeSubtask || t.ParentID == nil {
			return nil, false
		}
		parent, ok := universe[*t.ParentID]
		if !ok || !isColumn[parent.StatusID] {
			return nil, false
		}
		if t.StatusID != parent.StatusID {
			return nil, false
		}
		return parent, true
	}

	positions, err := s.cardPositions(ctx, b.ID)
	if err != nil {
		return nil, err
	}

	cardsByStatus := map[int64][]*Card{}
	cardByTicket := map[int64]*Card{}
	nestedUnder := map[int64][]Ticket{}

	// Ordering by id keeps assembly deterministic before card_order is applied.
	ids := make([]int64, 0, len(universe))
	for id := range universe {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		t := universe[id]
		if parent, ok := nested(t); ok {
			nestedUnder[parent.ID] = append(nestedUnder[parent.ID], *t)
			continue
		}
		if !isColumn[t.StatusID] {
			continue // this board doesn't show that status
		}
		pos, ok := positions[t.ID]
		if !ok {
			pos = unorderedPosition
		}
		c := &Card{Ticket: *t, Position: pos, Subtasks: []Ticket{}}
		cardsByStatus[t.StatusID] = append(cardsByStatus[t.StatusID], c)
		cardByTicket[t.ID] = c
	}
	for parentID, kids := range nestedUnder {
		if c, ok := cardByTicket[parentID]; ok {
			c.Subtasks = append(c.Subtasks, kids...)
		}
	}

	view := &BoardView{Board: *b, Columns: make([]Column, 0, len(statuses))}
	for _, st := range statuses {
		cards := cardsByStatus[st.ID]
		sort.SliceStable(cards, func(i, j int) bool {
			if cards[i].Position != cards[j].Position {
				return cards[i].Position < cards[j].Position
			}
			return cards[i].ID < cards[j].ID
		})
		col := Column{Status: st, Cards: make([]Card, 0, len(cards))}
		for _, c := range cards {
			sort.Slice(c.Subtasks, func(i, j int) bool { return c.Subtasks[i].ID < c.Subtasks[j].ID })
			col.Cards = append(col.Cards, *c)
		}
		view.Columns = append(view.Columns, col)
	}
	return view, nil
}

// unorderedPosition sorts cards that have never been dragged to the end of their
// column, rather than letting them disappear or jump to the top.
const unorderedPosition = 1e9

func (s *Store) subtasksOf(ctx context.Context, parentIDs []int64) ([]Ticket, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(parentIDs)), ",")
	args := make([]any, 0, len(parentIDs))
	for _, id := range parentIDs {
		args = append(args, id)
	}
	return s.queryTickets(ctx,
		fmt.Sprintf(`WHERE t.type = 'subtask' AND t.parent_id IN (%s)`, ph), args, "ORDER BY t.id")
}

func (s *Store) cardPositions(ctx context.Context, boardID int64) (map[int64]float64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ticket_id, position FROM card_order WHERE board_id = ?`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]float64{}
	for rows.Next() {
		var id int64
		var p float64
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// MoveCard sets a ticket's status and its position within the target column of a
// board. afterKey is the card it should land below; empty means the top.
//
// The whole target column is renumbered rather than fiddling with midpoints:
// columns on a personal board are small, and integer positions with no drift are
// far easier to reason about than fractional ones.
func (s *Store) MoveCard(ctx context.Context, scope, boardSlug, ticketKey, statusSlug, afterKey string) (*BoardView, error) {
	b, err := s.GetBoardBySlug(ctx, scope, boardSlug)
	if err != nil {
		return nil, err
	}
	t, err := s.GetTicket(ctx, ticketKey)
	if err != nil {
		return nil, err
	}
	if statusSlug != "" && statusSlug != t.Status.Slug {
		if _, err := s.UpdateTicket(ctx, ticketKey, TicketPatch{StatusSlug: &statusSlug}); err != nil {
			return nil, err
		}
		t, err = s.GetTicket(ctx, ticketKey)
		if err != nil {
			return nil, err
		}
	}

	// Re-assemble so the column reflects the new status, then reorder within it.
	view, err := s.GetBoard(ctx, scope, boardSlug)
	if err != nil {
		return nil, err
	}
	var column *Column
	for i := range view.Columns {
		if view.Columns[i].Status.ID == t.StatusID {
			column = &view.Columns[i]
			break
		}
	}
	if column == nil {
		// The ticket's status isn't a column here (or it nested into a parent
		// card). Status was still applied; there is nothing to order.
		return view, nil
	}

	order := make([]int64, 0, len(column.Cards))
	for _, c := range column.Cards {
		if c.ID != t.ID {
			order = append(order, c.ID)
		}
	}
	insertAt := 0
	if afterKey != "" {
		for i, id := range order {
			if c := findCard(column.Cards, id); c != nil && c.Key == afterKey {
				insertAt = i + 1
				break
			}
		}
	}
	if insertAt > len(order) {
		insertAt = len(order)
	}
	order = append(order[:insertAt], append([]int64{t.ID}, order[insertAt:]...)...)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i, id := range order {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO card_order (board_id, ticket_id, position) VALUES (?,?,?)
			 ON CONFLICT(board_id, ticket_id) DO UPDATE SET position = excluded.position`,
			b.ID, id, float64(i+1)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetBoard(ctx, scope, boardSlug)
}

func findCard(cards []Card, id int64) *Card {
	for i := range cards {
		if cards[i].ID == id {
			return &cards[i]
		}
	}
	return nil
}
