// Package cli implements geetproject's command line client. It talks to a running
// server over HTTP rather than opening the database directly, so the CLI and the
// web UI always agree - including about board assembly, which only the server
// knows how to do.
package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/andrecastillo/geetproject/internal/store"
)

const Usage = `Usage: geetproject <command> [flags]

Server:
  serve                          run the web UI and API

Projects:
  projects                       list projects with ticket counts
  project   new NAME [--prefix MAI] [--color '#60a5fa']
  project   rm SLUG [--force]    deletes its tickets too

Tickets:
  ls        [--project P] [--type T] [--status S] [--label L] [--parent KEY] [--search Q]
  new       "title" [--project P] [--type task|epic|subtask] [--parent KEY]
                    [--status S] [--label NAME]... [--desc-file FILE]
  show      KEY
  edit      KEY [--title T] [--status S] [--desc-file FILE] [--parent KEY]
                [--project P] [--type T]
  rm        KEY [--force]
  comment   KEY "body"

Batch (for scripts and agents):
  import    [--project P] [--dry-run]   create a tree of tickets from JSON on stdin
  agent-guide                           print the full interface for an LLM

Views:
  boards    [--project P]        views in a scope ('all' for cross-project)
  board     SCOPE VIEW           a view's columns and cards, e.g. geetproject board mai epics

Common flags:
  --server URL    geetproject server (default $GEETPROJECT_URL, else http://localhost:8080)
  --project P     project slug (default $GEETPROJECT_PROJECT); 'all' means every project
  --json          raw JSON instead of formatted output
`

type client struct {
	base string
	http *http.Client
}

func (c *client) do(method, path string, body any, out any) error {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, buf)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach geetproject at %s: %w", c.base, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("%s %s: %s", method, path, res.Status)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Run dispatches a CLI command. It returns flag.ErrHelp when the caller should
// print usage.
func Run(args []string) error {
	if len(args) == 0 {
		return flag.ErrHelp
	}
	cmd, rest := args[0], args[1:]

	// Every subcommand shares --server and --json, so pull them off first.
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("server", envOr("GEETPROJECT_URL", "http://localhost:8080"), "geetproject server URL")
	asJSON := fs.Bool("json", false, "raw JSON output")

	var (
		project    = fs.String("project", os.Getenv("GEETPROJECT_PROJECT"), "project slug")
		ticketType = fs.String("type", "", "ticket type: epic, task or subtask")
		status     = fs.String("status", "", "status slug")
		parent     = fs.String("parent", "", "parent ticket key")
		search     = fs.String("search", "", "match title or description")
		title      = fs.String("title", "", "new title")
		descFile   = fs.String("desc-file", "", "read the description from a file ('-' for stdin)")
		prefix     = fs.String("prefix", "", "ticket key prefix for a new project")
		color      = fs.String("color", "", "project colour, e.g. '#60a5fa'")
		force      = fs.Bool("force", false, "skip the confirmation prompt")
		dryRun     = fs.Bool("dry-run", false, "validate and report, but write nothing")
		labels     multiFlag
	)
	fs.Var(&labels, "label", "label name (repeatable)")

	// Positional arguments may appear before or after flags; Go's flag package
	// stops at the first non-flag, so collect them in one pass.
	var positional []string
	remaining := rest
	for len(remaining) > 0 {
		if err := fs.Parse(remaining); err != nil {
			return err
		}
		remaining = fs.Args()
		if len(remaining) == 0 {
			break
		}
		positional = append(positional, remaining[0])
		remaining = remaining[1:]
	}

	c := &client{base: strings.TrimRight(*server, "/"), http: &http.Client{Timeout: 15 * time.Second}}
	arg := func(i int) string {
		if i < len(positional) {
			return positional[i]
		}
		return ""
	}

	switch cmd {
	case "projects":
		return c.projects(*asJSON)
	case "project":
		switch arg(0) {
		case "new":
			if arg(1) == "" {
				return fmt.Errorf(`project new needs a name: geetproject project new "mai"`)
			}
			return c.createProject(arg(1), *prefix, *color, *asJSON)
		case "rm":
			if arg(1) == "" {
				return fmt.Errorf("project rm needs a slug")
			}
			return c.removeProject(arg(1), *force)
		default:
			return fmt.Errorf("project takes 'new' or 'rm'")
		}
	case "import":
		return c.importBatch(*project, *dryRun, *asJSON)
	case "agent-guide":
		fmt.Print(AgentGuide)
		return nil
	case "ls":
		return c.list(*project, *ticketType, *status, *parent, *search, labels, *asJSON)
	case "new":
		if arg(0) == "" {
			return fmt.Errorf(`new needs a title: geetproject new "Fix the thing"`)
		}
		return c.create(arg(0), *project, *ticketType, *parent, *status, *descFile, labels, *asJSON)
	case "show":
		if arg(0) == "" {
			return fmt.Errorf("show needs a ticket key, e.g. geetproject show T-12")
		}
		return c.show(arg(0), *asJSON)
	case "edit":
		if arg(0) == "" {
			return fmt.Errorf("edit needs a ticket key")
		}
		return c.edit(arg(0), *title, *status, *ticketType, *parent, *project, *descFile, fs, *asJSON)
	case "rm":
		if arg(0) == "" {
			return fmt.Errorf("rm needs a ticket key")
		}
		return c.remove(arg(0), *force)
	case "comment":
		if arg(0) == "" || arg(1) == "" {
			return fmt.Errorf(`comment needs a key and a body: geetproject comment T-12 "note"`)
		}
		return c.comment(arg(0), arg(1), *asJSON)
	case "boards":
		return c.boards(scopeOr(*project), *asJSON)
	case "board":
		if arg(0) == "" {
			return fmt.Errorf("board needs a scope and a view, e.g. geetproject board mai epics")
		}
		// `geetproject board mai epics`, or `geetproject board epics` with --project set.
		if arg(1) == "" {
			return c.board(scopeOr(*project), arg(0), *asJSON)
		}
		return c.board(arg(0), arg(1), *asJSON)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// scopeOr defaults an unset --project to the cross-project scope.
func scopeOr(project string) string {
	if project == "" {
		return "all"
	}
	return project
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	// Accept the pre-rename GEET_* names, loudly. Without this, anything still
	// passing GEET_DB to a binary that only reads GEETPROJECT_DB would fall back
	// to the default path and quietly start an empty database - which looks
	// exactly like data loss rather than a misconfiguration.
	if suffix, ok := strings.CutPrefix(key, "GEETPROJECT_"); ok {
		legacy := "GEET_" + suffix
		if v := os.Getenv(legacy); v != "" {
			fmt.Fprintf(os.Stderr, "geetproject: %s is deprecated, use %s\n", legacy, key)
			return v
		}
	}
	return fallback
}

func dumpJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// labelIDs resolves label names to ids, creating any that don't exist yet so
// `--label backend` works without a separate setup step.
func (c *client) labelIDs(names []string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	var existing []store.Label
	if err := c.do("GET", "/api/labels", nil, &existing); err != nil {
		return nil, err
	}
	byName := map[string]int64{}
	for _, l := range existing {
		byName[strings.ToLower(l.Name)] = l.ID
	}
	out := make([]int64, 0, len(names))
	for _, n := range names {
		if id, ok := byName[strings.ToLower(n)]; ok {
			out = append(out, id)
			continue
		}
		var created store.Label
		if err := c.do("POST", "/api/labels", map[string]any{"name": n}, &created); err != nil {
			return nil, err
		}
		out = append(out, created.ID)
	}
	return out, nil
}

func readDescription(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func (c *client) list(project, ticketType, status, parent, search string, labels multiFlag, asJSON bool) error {
	q := []string{}
	if project != "" {
		q = append(q, "project="+project)
	}
	if ticketType != "" {
		q = append(q, "type="+ticketType)
	}
	if status != "" {
		q = append(q, "status="+status)
	}
	if parent != "" {
		q = append(q, "parent="+parent)
	}
	if search != "" {
		q = append(q, "q="+search)
	}
	if len(labels) > 0 {
		ids, err := c.labelIDs(labels)
		if err != nil {
			return err
		}
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, fmt.Sprint(id))
		}
		q = append(q, "label_ids="+strings.Join(parts, ","))
	}
	path := "/api/tickets"
	if len(q) > 0 {
		path += "?" + strings.Join(q, "&")
	}

	var tickets []store.Ticket
	if err := c.do("GET", path, nil, &tickets); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(tickets)
	}
	if len(tickets) == 0 {
		fmt.Println("No tickets.")
		return nil
	}
	for _, t := range tickets {
		fmt.Printf("%-9s %-9s %-8s %-13s %s\n",
			t.Key, t.ProjectSlug, t.Type, statusName(t), t.Title)
	}
	return nil
}

func statusName(t store.Ticket) string {
	if t.Status == nil {
		return ""
	}
	return t.Status.Name
}

func (c *client) create(title, project, ticketType, parent, status, descFile string, labels multiFlag, asJSON bool) error {
	desc, err := readDescription(descFile)
	if err != nil {
		return err
	}
	ids, err := c.labelIDs(labels)
	if err != nil {
		return err
	}
	if ticketType == "" {
		ticketType = "task"
	}
	body := map[string]any{
		"type": ticketType, "title": title, "description": desc,
		"status": status, "parent": parent, "label_ids": ids,
	}
	// A child inherits its parent's project, so only send one when standing alone.
	if parent == "" {
		if project == "" {
			return fmt.Errorf("a ticket needs a project: pass --project, or set GEETPROJECT_PROJECT")
		}
		body["project"] = project
	}
	var t store.Ticket
	if err := c.do("POST", "/api/tickets", body, &t); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(t)
	}
	fmt.Printf("%s  %s\n", t.Key, t.Title)
	return nil
}

type detail struct {
	store.Ticket
	Children        []store.Ticket  `json:"children"`
	Comments        []store.Comment `json:"comments"`
	DescendantCount int             `json:"descendant_count"`
}

func (c *client) show(key string, asJSON bool) error {
	var d detail
	if err := c.do("GET", "/api/tickets/"+key, nil, &d); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(d)
	}
	fmt.Printf("%s  %s\n", d.Key, d.Title)
	fmt.Printf("%s · %s · %s", d.ProjectSlug, d.Type, statusName(d.Ticket))
	if d.ParentKey != "" {
		fmt.Printf(" · in %s %s", d.ParentKey, d.ParentTitle)
	}
	if len(d.Labels) > 0 {
		names := make([]string, 0, len(d.Labels))
		for _, l := range d.Labels {
			names = append(names, l.Name)
		}
		fmt.Printf(" · %s", strings.Join(names, ", "))
	}
	fmt.Println()
	if strings.TrimSpace(d.Description) != "" {
		fmt.Printf("\n%s\n", strings.TrimRight(d.Description, "\n"))
	}
	if len(d.Children) > 0 {
		fmt.Printf("\nChildren (%d):\n", len(d.Children))
		for _, ch := range d.Children {
			fmt.Printf("  %-6s %-13s %s\n", ch.Key, statusName(ch), ch.Title)
		}
	}
	if len(d.Comments) > 0 {
		fmt.Printf("\nComments (%d):\n", len(d.Comments))
		for _, cm := range d.Comments {
			fmt.Printf("  [%s] %s\n", cm.CreatedAt, strings.ReplaceAll(cm.Body, "\n", "\n  "))
		}
	}
	return nil
}

func (c *client) edit(key, title, status, ticketType, parent, project, descFile string, fs *flag.FlagSet, asJSON bool) error {
	// Only send what was actually passed. Sending everything would blank the
	// fields the user left out.
	patch := map[string]any{}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if set["title"] {
		patch["title"] = title
	}
	if set["status"] {
		patch["status"] = status
	}
	if set["type"] {
		patch["type"] = ticketType
	}
	if set["parent"] {
		patch["parent"] = parent
	}
	if set["project"] {
		patch["project"] = project
	}
	if set["desc-file"] {
		desc, err := readDescription(descFile)
		if err != nil {
			return err
		}
		patch["description"] = desc
	}
	if len(patch) == 0 {
		return fmt.Errorf("nothing to change; pass --title, --status, --type, --parent, --project or --desc-file")
	}

	var t store.Ticket
	if err := c.do("PATCH", "/api/tickets/"+key, patch, &t); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(t)
	}
	fmt.Printf("%s  %s  (%s)\n", t.Key, t.Title, statusName(t))
	return nil
}

func (c *client) remove(key string, force bool) error {
	var d detail
	if err := c.do("GET", "/api/tickets/"+key, nil, &d); err != nil {
		return err
	}
	if !force {
		extra := ""
		if d.DescendantCount > 0 {
			extra = fmt.Sprintf(" and %d child ticket(s)", d.DescendantCount)
		}
		fmt.Printf("Delete %s %q%s? [y/N] ", d.Key, d.Title, extra)
		var answer string
		fmt.Scanln(&answer)
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	if err := c.do("DELETE", "/api/tickets/"+key, nil, nil); err != nil {
		return err
	}
	fmt.Printf("Deleted %s\n", key)
	return nil
}

func (c *client) comment(key, body string, asJSON bool) error {
	var cm store.Comment
	if err := c.do("POST", "/api/tickets/"+key+"/comments", map[string]any{"body": body}, &cm); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(cm)
	}
	fmt.Printf("Commented on %s\n", key)
	return nil
}

func (c *client) boards(scope string, asJSON bool) error {
	var bs []store.Board
	if err := c.do("GET", "/api/projects/"+scope+"/boards", nil, &bs); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(bs)
	}
	for _, b := range bs {
		scope := b.FilterType
		if scope == "any" {
			scope = "all types"
		} else {
			scope += "s only"
		}
		if len(b.FilterLabels) > 0 {
			names := make([]string, 0, len(b.FilterLabels))
			for _, l := range b.FilterLabels {
				names = append(names, l.Name)
			}
			scope += ", labelled " + strings.Join(names, "/")
		}
		fmt.Printf("%-14s %-16s %s\n", b.Slug, b.Name, scope)
	}
	return nil
}

func (c *client) board(scope, slug string, asJSON bool) error {
	var v store.BoardView
	if err := c.do("GET", "/api/projects/"+scope+"/boards/"+slug, nil, &v); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(v)
	}
	fmt.Printf("%s\n", v.Name)
	for _, col := range v.Columns {
		fmt.Printf("\n%s (%d)\n", col.Status.Name, len(col.Cards))
		for _, card := range col.Cards {
			prefix := ""
			if card.Type == store.TypeSubtask && card.ParentKey != "" {
				prefix = fmt.Sprintf("(in %s) ", card.ParentKey)
			}
			fmt.Printf("  %-6s %s%s\n", card.Key, prefix, card.Title)
			for _, st := range card.Subtasks {
				fmt.Printf("      %-6s %s\n", st.Key, st.Title)
			}
		}
	}
	return nil
}

// ---- projects ----

func (c *client) projects(asJSON bool) error {
	var ps []store.Project
	if err := c.do("GET", "/api/projects", nil, &ps); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(ps)
	}
	if len(ps) == 0 {
		fmt.Println("No projects yet. Create one with: geetproject project new \"mai\"")
		return nil
	}
	for _, p := range ps {
		fmt.Printf("%-14s %-8s %4d ticket(s)  %s\n", p.Slug, p.Prefix, p.TicketCount, p.Name)
	}
	return nil
}

func (c *client) createProject(name, prefix, color string, asJSON bool) error {
	body := map[string]any{"name": name}
	if prefix != "" {
		body["prefix"] = prefix
	}
	if color != "" {
		body["color"] = color
	}
	var p store.Project
	if err := c.do("POST", "/api/projects", body, &p); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(p)
	}
	fmt.Printf("%s  %s  keys look like %s-1\n", p.Slug, p.Name, p.Prefix)
	return nil
}

func (c *client) removeProject(slug string, force bool) error {
	var p store.Project
	if err := c.do("GET", "/api/projects/"+slug, nil, &p); err != nil {
		return err
	}
	if !force {
		extra := ""
		if p.TicketCount > 0 {
			extra = fmt.Sprintf(" and its %d ticket(s)", p.TicketCount)
		}
		fmt.Printf("Delete project %q%s? This cannot be undone. [y/N] ", p.Name, extra)
		var answer string
		fmt.Scanln(&answer)
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("Cancelled.")
			return nil
		}
	}
	var res struct {
		DeletedTickets int `json:"deleted_tickets"`
	}
	if err := c.do("DELETE", "/api/projects/"+slug, nil, &res); err != nil {
		return err
	}
	fmt.Printf("Deleted project %s and %d ticket(s)\n", slug, res.DeletedTickets)
	return nil
}

// ---- batch import ----

func (c *client) importBatch(project string, dryRun, asJSON bool) error {
	if project == "" {
		return fmt.Errorf("import needs a project: pass --project, or set GEETPROJECT_PROJECT")
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return fmt.Errorf("no JSON on stdin; pipe a batch in, e.g. geetproject import --project %s < plan.json", project)
	}

	// Accept either {"tickets":[...]} or a bare [...] array, since a model asked
	// for "a list of tickets" reasonably often produces the latter.
	var body any
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var arr []store.ImportNode
		if err := json.Unmarshal(raw, &arr); err != nil {
			return fmt.Errorf("could not parse the batch as JSON: %w", err)
		}
		body = map[string]any{"tickets": arr}
	} else {
		var doc struct {
			Tickets []store.ImportNode `json:"tickets"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("could not parse the batch as JSON: %w", err)
		}
		body = doc
	}

	path := "/api/projects/" + project + "/import"
	if dryRun {
		path += "?dry_run=1"
	}
	var res struct {
		DryRun  bool                 `json:"dry_run"`
		Count   int                  `json:"count"`
		Tickets []store.ImportResult `json:"tickets"`
	}
	if err := c.do("POST", path, body, &res); err != nil {
		return err
	}
	if asJSON {
		return dumpJSON(res)
	}
	for _, t := range res.Tickets {
		fmt.Printf("%s%-9s %-8s %s\n", strings.Repeat("  ", t.Depth), t.Key, t.Type, t.Title)
	}
	if res.DryRun {
		fmt.Printf("Dry run: would create %d ticket(s) in %s. Nothing was written.\n", res.Count, project)
	} else {
		fmt.Printf("Created %d ticket(s) in %s.\n", res.Count, project)
	}
	return nil
}
