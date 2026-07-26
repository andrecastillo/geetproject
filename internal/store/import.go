package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ImportNode is one ticket in a batch, with its children nested beneath it.
// Type, Status and Labels are all optional: type is inferred from the depth,
// status falls back to the first one, and labels are created if they are new.
type ImportNode struct {
	Type        string       `json:"type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Status      string       `json:"status"`
	Labels      []string     `json:"labels"`
	Children    []ImportNode `json:"children"`
}

// ImportResult reports one created ticket and how deep it sat in the batch, so
// callers can print the tree back in shape.
type ImportResult struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Depth int    `json:"depth"`
}

// ImportError collects every problem found in one pass, each pointed at the
// offending node, rather than failing on the first and hiding the rest.
type ImportError struct {
	Problems []string
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("%d problem(s) with the batch:\n  %s",
		len(e.Problems), strings.Join(e.Problems, "\n  "))
}

// Is lets callers match with errors.Is(err, ErrInvalid) like every other
// validation failure in the store.
func (e *ImportError) Is(target error) bool { return target == ErrInvalid }

// inferType picks a type for a node that didn't state one, from where it sits.
// A top-level node with children is an epic; without children it is a plain
// task, since a lone epic containing nothing is rarely what anyone meant.
func inferType(parentType string, hasChildren bool) string {
	switch parentType {
	case TypeEpic:
		return TypeTask
	case TypeTask:
		return TypeSubtask
	default:
		if hasChildren {
			return TypeEpic
		}
		return TypeTask
	}
}

// Import creates a whole tree of tickets in one transaction. With dryRun it
// validates and reports what it would create, then rolls the transaction back
// so nothing is written - which means the keys it reports are the ones it would
// have assigned, not ones already taken.
//
// The batch is all-or-nothing: a failure anywhere leaves the database
// untouched, so a retry cannot duplicate the part that already succeeded.
func (s *Store) Import(ctx context.Context, projectSlug string, nodes []ImportNode, dryRun bool) ([]ImportResult, error) {
	if projectSlug == "" || projectSlug == AllScope {
		return nil, fmt.Errorf("%w: an import needs a project to file tickets under", ErrInvalid)
	}
	project, err := s.GetProjectBySlug(ctx, projectSlug)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: the batch contains no tickets", ErrInvalid)
	}

	statuses, err := s.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	statusBySlug := map[string]int64{}
	for _, st := range statuses {
		statusBySlug[st.Slug] = st.ID
	}
	if len(statuses) == 0 {
		return nil, errors.New("no statuses configured")
	}
	defaultStatus := statuses[0].ID

	// Pass 1: validate the whole tree without writing anything, so a bad batch
	// comes back as one list of problems instead of a partial import.
	ve := &ImportError{}
	var validate func(ns []ImportNode, parentType, path string)
	validate = func(ns []ImportNode, parentType, path string) {
		for i, n := range ns {
			at := fmt.Sprintf("%s[%d]", path, i)
			if strings.TrimSpace(n.Title) == "" {
				ve.Problems = append(ve.Problems, at+": title is required")
			}
			t := n.Type
			if t == "" {
				t = inferType(parentType, len(n.Children) > 0)
			}
			if t != TypeEpic && t != TypeTask && t != TypeSubtask {
				ve.Problems = append(ve.Problems,
					fmt.Sprintf("%s: unknown type %q (want epic, task or subtask)", at, n.Type))
				continue
			}
			// Reuse the same hierarchy rule the single-ticket path enforces, by
			// describing the parent as a bare Ticket of the right type.
			var parent *Ticket
			if parentType != "" {
				parent = &Ticket{Type: parentType}
			}
			if err := validateParent(t, parent); err != nil {
				ve.Problems = append(ve.Problems,
					fmt.Sprintf("%s (%q): %s", at, n.Title, strings.TrimPrefix(err.Error(), "invalid: ")))
				continue
			}
			if n.Status != "" {
				if _, ok := statusBySlug[n.Status]; !ok {
					ve.Problems = append(ve.Problems,
						fmt.Sprintf("%s: unknown status %q", at, n.Status))
				}
			}
			for _, l := range n.Labels {
				if strings.TrimSpace(l) == "" {
					ve.Problems = append(ve.Problems, at+": a label name cannot be blank")
				}
			}
			if len(n.Children) > 0 {
				validate(n.Children, t, at+".children")
			}
		}
	}
	validate(nodes, "", "tickets")
	if len(ve.Problems) > 0 {
		return nil, ve
	}

	// Pass 2: apply. One transaction for the whole batch.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	labelIDs, err := resolveLabelsTx(ctx, tx, nodes)
	if err != nil {
		return nil, err
	}

	out := []ImportResult{}
	var apply func(ns []ImportNode, parentType string, parentID *int64, depth int) error
	apply = func(ns []ImportNode, parentType string, parentID *int64, depth int) error {
		for _, n := range ns {
			t := n.Type
			if t == "" {
				t = inferType(parentType, len(n.Children) > 0)
			}
			statusID := defaultStatus
			if n.Status != "" {
				statusID = statusBySlug[n.Status]
			}

			key, err := nextTicketKey(ctx, tx, project.ID)
			if err != nil {
				return err
			}
			ts := now()
			var pid any
			if parentID != nil {
				pid = *parentID
			}
			res, err := tx.ExecContext(ctx, `
				INSERT INTO ticket
				  (key, project_id, type, title, description, status_id, parent_id, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				key, project.ID, t, strings.TrimSpace(n.Title), n.Description, statusID, pid, ts, ts)
			if err != nil {
				return err
			}
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			ids := make([]int64, 0, len(n.Labels))
			for _, name := range n.Labels {
				ids = append(ids, labelIDs[strings.ToLower(strings.TrimSpace(name))])
			}
			if err := setTicketLabels(ctx, tx, id, ids); err != nil {
				return err
			}

			out = append(out, ImportResult{Key: key, Type: t, Title: strings.TrimSpace(n.Title), Depth: depth})
			if len(n.Children) > 0 {
				if err := apply(n.Children, t, &id, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := apply(nodes, "", nil, 0); err != nil {
		return nil, err
	}

	if dryRun {
		// Rolling back also returns the key counter to where it was, so a real
		// import afterwards produces exactly the keys reported here.
		return out, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return out, nil
}

// resolveLabelsTx maps every label name in the batch to an id, creating the ones
// that don't exist yet. Inside the transaction, so a dry run invents nothing.
func resolveLabelsTx(ctx context.Context, tx *sql.Tx, nodes []ImportNode) (map[string]int64, error) {
	wanted := map[string]string{} // lower-cased -> as written
	var walk func(ns []ImportNode)
	walk = func(ns []ImportNode) {
		for _, n := range ns {
			for _, l := range n.Labels {
				name := strings.TrimSpace(l)
				if name != "" {
					wanted[strings.ToLower(name)] = name
				}
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	if len(wanted) == 0 {
		return map[string]int64{}, nil
	}

	out := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM label`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		out[strings.ToLower(name)] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for lower, name := range wanted {
		if _, ok := out[lower]; ok {
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO label (name, color) VALUES (?, '')`, name)
		if err != nil {
			return nil, fmt.Errorf("create label %q: %w", name, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		out[lower] = id
	}
	return out, nil
}
