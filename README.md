# geetproject

A small, self-hosted kanban issue tracker for one person.

One Go binary with the web UI compiled into it, one SQLite file, one container.
No account, no cloud, no external services.

```
┌──────────────┬────────────────────────────────────────────┐
│ PROJECTS     │  All  Epics  Frontend            + New     │
│              ├────────────────────────────────────────────┤
│ ▸ All        │  ┌─ Todo ─────────┐  ┌─ Done ───────────┐  │
│ ▪ mai        │  │ MAI-4  Login   │  │ ↳ in MAI-4       │  │
│ ▸ mini-kg    │  │ [Todo      ▾]  │  │ MAI-6  api       │  │
│              │  │ ────────────── │  │ [Done        ▾]  │  │
│              │  │ ○ MAI-5 form   │  └──────────────────┘  │
│ + New project│  │      [Todo  ▾] │                        │
│              │  └────────────────┘                        │
└──────────────┴────────────────────────────────────────────┘
```

`MAI-5` shares its parent's status, so it nests inside the card. `MAI-6` moved to
Done, so it broke out into a card of its own with a breadcrumb back to `MAI-4`.

## What it does

- **Projects** are the top-level container, listed down the left. Every ticket
  belongs to exactly one, and keys are per project — `MAI-1` alongside `KG-1`.
  A pinned **All projects** scope shows everything at once, with each card
  naming where it came from.
- **Three levels of ticket** — epic → task → sub-task. An epic lists its tickets;
  a task lists its sub-tasks. A child always lives in its parent's project, and
  moving a ticket moves its whole subtree.
- **Views are saved filters** within a project, shown as tabs. A view picks a
  ticket type and optionally some labels, so "just the epics in mai" or "tasks
  labelled frontend" are ordinary views. Every project gets **All** and **Epics**
  to start with.
- **Sub-tasks nest, then break out.** A sub-task renders as a row inside its
  parent's card while it shares the parent's status, and becomes a card of its
  own — carrying a `↳ in T-4` breadcrumb — the moment its status differs.
- **Columns are per board**, each mapped to one global status, so "done" means
  the same thing everywhere.
- Markdown descriptions and comments, labels (shared across projects),
  drag-and-drop, and an inline status control on every card and sub-task row.
- A **CLI** over the same API, for when a terminal is faster than a browser.

Deliberately absent: priorities, dependency/blocker links, sprints, time
tracking, notifications, and multi-user. The point is to keep it small.

## Run it

### Docker (how it's meant to run)

```bash
docker run -d --name geetproject \
  -p 8080:8080 \
  -v /mnt/user/appdata/geetproject:/data \
  -e PUID=99 -e PGID=100 \
  ghcr.io/andrecastillo/geetproject:latest
```

Then open `http://<host>:8080`. On Unraid, `unraid-template.xml` in this repo
sets the same thing up through Community Applications; `PUID`/`PGID` default to
Unraid's `nobody:users` so the database isn't left owned by root.

`docker compose up --build` also works and is what the included
`docker-compose.yml` is for.

### From source

```bash
npm --prefix web ci && npm --prefix web run build   # builds the UI
go build -o geetproject ./cmd/geetproject                         # embeds it in the binary
./geetproject serve --addr :8080 --db ./geet.db
```

Requires Go 1.25+ and Node 22+. The build is pure Go (`CGO_ENABLED=0` works),
because the SQLite driver is `modernc.org/sqlite` rather than a cgo binding.

### Development

Run the API and the Vite dev server side by side; Vite proxies `/api` to 8080.

```bash
go run ./cmd/geetproject serve          # terminal 1
npm --prefix web run dev         # terminal 2, then open http://localhost:5173
```

## CLI

Every command talks to a running server over HTTP, so the CLI and the web UI can
never disagree. Point it somewhere else with `--server` or `$GEETPROJECT_URL`.

```bash
geetproject projects                                   # list projects with ticket counts
geetproject project new "mini-kg" --prefix KG
geetproject boards --project mai                       # views in a project ('all' for global)
geetproject board mai epics                            # columns, cards, nested sub-tasks
geetproject ls --project mai --type task --status todo
geetproject new "Fix the importer" --project mai --label infra
geetproject new "Stream tokens" --parent MAI-4          # inherits its parent's project
geetproject show MAI-12
geetproject edit MAI-12 --status in-progress
geetproject edit MAI-12 --desc-file notes.md           # or --desc-file - for stdin
geetproject edit MAI-12 --project mini-kg              # moves its sub-tasks too
geetproject comment MAI-12 "Leaning towards a subtree merge."
geetproject rm MAI-12
geetproject project rm mai                             # deletes its tickets, after a prompt
```

`--label` creates the label if it doesn't exist yet. `--json` works on every
read command. `edit` sends only the flags you passed, so changing a title never
touches the description. Set `GEETPROJECT_PROJECT` to stay in one project without
repeating `--project`.

### Batch import

One call creates a whole tree, in a single transaction. This is the way to file
a plan's worth of work at once, and the way an LLM should use geetproject.

```bash
geetproject import --project mai --dry-run < plan.json   # validate, write nothing
geetproject import --project mai < plan.json
```

```json
{
  "tickets": [
    { "type": "epic", "title": "Chat UI",
      "description": "Front the knowledge graph with a chat interface.",
      "labels": ["app"],
      "children": [
        { "title": "Flask /chat route", "status": "in-progress",
          "children": [
            { "title": "Stream tokens back to the client" },
            { "title": "Wire up the tool schema" }
          ]}
      ]}
  ]
}
```

Only `title` is required. `type` is inferred from the shape — a child of an epic
is a task, a child of a task is a sub-task, and a top-level entry is an epic if
it has children. Unknown labels are created. A bare JSON array works in place of
`{"tickets": [...]}`.

The batch is **all-or-nothing**: it is validated in full first, and every problem
is reported at once naming the offending node, so a failure leaves nothing
behind and a retry cannot half-duplicate the tree.

```
$ geetproject import --project mai < broken.json
geetproject: 3 problem(s) with the batch:
  tickets[0]: title is required
  tickets[1]: unknown type "story" (want epic, task or subtask)
  tickets[2]: unknown status "doing"
```

### Driving geetproject from an LLM

`geetproject agent-guide` prints the whole interface — model, batch format, commands —
in a form meant to be read by a model. It ships inside the binary, so it cannot
drift from the code.

```bash
geetproject agent-guide                       # read it
geetproject agent-guide >> ~/CLAUDE.md        # or paste it into a system prompt
```

Install the CLI where every project can reach it:

```bash
go build -ldflags="-s -w" -o ~/.local/bin/geetproject ./cmd/geetproject
```

To keep the server up so the CLI always has something to talk to, either point
`GEETPROJECT_URL` at the container on your server, or run it locally as a user service:

```bash
systemctl --user enable --now geetproject     # see contrib/geetproject.service
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `GEETPROJECT_ADDR` | `:8080` | Listen address for `geetproject serve` |
| `GEETPROJECT_DB` | `./geet.db` (`/data/geet.db` in the container) | SQLite file |
| `GEETPROJECT_URL` | `http://localhost:8080` | Server the CLI talks to |
| `GEETPROJECT_PROJECT` | *(unset)* | Default project for CLI commands |
| `PUID` / `PGID` | `99` / `100` | Container only: who owns the database file |

There is no authentication. Bind it to your LAN, not the internet.

## Backups

Everything lives in the single SQLite file at `GEETPROJECT_DB`. Stopping the container
checkpoints the write-ahead log into it, so a copy taken while geetproject is stopped
is a complete, self-contained backup. To copy it while running, use
`sqlite3 geet.db ".backup out.db"` rather than `cp`, which can catch a partial
write.

## Layout

```
cmd/geetproject/          entrypoint: `serve` plus the CLI commands
internal/store/    schema, migrations, and every SQL statement including board assembly
internal/api/      JSON API over the store
internal/cli/      CLI, an HTTP client of that API
web/               React + Vite UI, compiled into the binary via go:embed
embed.go           the go:embed directive and SPA handler
```

The rule worth knowing before changing anything: **the nest/break-out logic
lives only in `store.GetBoard`**. The web UI never recomputes it — after a card
moves, the server returns the reassembled board and the client renders what it's
given.

Ticket keys are never reused. Each project has a counter that only increments,
so a `see MAI-2` written into a description keeps pointing at the same ticket
even after deletes — and moving a ticket between projects deliberately keeps its
original key for the same reason.

`go test ./...` covers the hierarchy and same-project rules, cascading deletes,
partial updates, board assembly in both directions, column ordering, per-project
key sequences, and migrating a real pre-projects database in place.
