package store

// Ticket types. The hierarchy is exactly three fixed levels, which is what keeps
// the tree from cycling and makes the "epics only" board a plain type filter.
const (
	TypeEpic    = "epic"
	TypeTask    = "task"
	TypeSubtask = "subtask"
)

// Project is the top-level container. Every ticket belongs to exactly one.
type Project struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Prefix      string `json:"prefix"`
	Color       string `json:"color"`
	Position    int    `json:"position"`
	CreatedAt   string `json:"created_at"`
	TicketCount int    `json:"ticket_count"`
}

type Status struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Color    string `json:"color"`
	Position int    `json:"position"`
	IsDone   bool   `json:"is_done"`
}

type Label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Ticket struct {
	ID          int64   `json:"id"`
	Key         string  `json:"key"`
	ProjectID   int64   `json:"project_id"`
	ProjectSlug string  `json:"project_slug"`
	ProjectName string  `json:"project_name"`
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	StatusID    int64   `json:"status_id"`
	Status      *Status `json:"status,omitempty"`
	ParentID    *int64  `json:"parent_id,omitempty"`
	ParentKey   string  `json:"parent_key,omitempty"`
	ParentTitle string  `json:"parent_title,omitempty"`
	Labels      []Label `json:"labels"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type Comment struct {
	ID        int64  `json:"id"`
	TicketID  int64  `json:"ticket_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Board struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	// ProjectID nil means the cross-project scope; ProjectSlug is then "all".
	ProjectID       *int64  `json:"project_id"`
	ProjectSlug     string  `json:"project_slug"`
	FilterType      string  `json:"filter_type"`
	FilterLabelMode string  `json:"filter_label_mode"`
	FilterLabels    []Label `json:"filter_labels"`
	Position        int     `json:"position"`
	CreatedAt       string  `json:"created_at"`
}

// Card is a ticket as it appears on a board. Subtasks holds the sub-tasks that
// currently share the card's status and therefore render nested inside it; a
// sub-task whose status differs gets a Card of its own instead.
type Card struct {
	Ticket
	Position float64  `json:"position"`
	Subtasks []Ticket `json:"subtasks"`
}

type Column struct {
	Status Status `json:"status"`
	Cards  []Card `json:"cards"`
}

// BoardView is a board plus its fully assembled columns.
type BoardView struct {
	Board
	Columns []Column `json:"columns"`
}

// TicketFilter drives both the list endpoint and board assembly.
type TicketFilter struct {
	// ProjectID nil means every project - the cross-project scope.
	ProjectID *int64
	Type      string // epic|task|subtask|any|"" (empty means any)
	StatusID  int64
	LabelIDs  []int64
	LabelMode string // "any" or "all"
	ParentID  *int64
	HasParent *bool
	Search    string
}
