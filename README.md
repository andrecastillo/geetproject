# geet

A small, self-hosted kanban issue tracker for one person.

One Go binary with the web UI compiled into it, one SQLite file, one container.
No account, no cloud, no external services.

```
┌─ Todo ─────────────┐  ┌─ Done ─────────────┐
│ ┌────────────────┐ │  │ ┌────────────────┐ │
│ │ T-4  Login     │ │  │ │ ↳ in T-4       │ │
│ │ [Todo      ▾]  │ │  │ │ T-6  api       │ │
│ │ ────────────── │ │  │ │ [Done      ▾]  │ │
│ │ ○ T-5 form     │ │  │ └────────────────┘ │
│ │      [Todo  ▾] │ │  │                    │
│ └────────────────┘ │  │                    │
└────────────────────┘  └────────────────────┘
```

## What it does

- **Three levels of ticket** — epic → task → sub-task. An epic lists its tickets;
  a task lists its sub-tasks.
- **Boards are saved filters**, not folders. One pool of tickets; a board picks a
  ticket type and optionally some labels. So an epic can sit on the Epics board
  while its children sit on a team board, with nothing duplicated.
- **Sub-tasks nest, then break out.** A sub-task renders as a row inside its
  parent's card while it shares the parent's status, and becomes a card of its
  own — carrying a `↳ in T-4` breadcrumb — the moment its status differs.
- **Columns are per board**, each mapped to one global status, so "done" means
  the same thing everywhere.
- Markdown descriptions and comments, labels, drag-and-drop, and an inline status
  control on every card and sub-task row.
- A **CLI** over the same API, for when a terminal is faster than a browser.

Deliberately absent: priorities, dependency/blocker links, sprints, time
tracking, notifications, and multi-user. The point is to keep it small.

## Run it

### Docker (how it's meant to run)

```bash
docker run -d --name geet \
  -p 8080:8080 \
  -v /mnt/user/appdata/geet:/data \
  -e PUID=99 -e PGID=100 \
  ghcr.io/dv310p3r/geet:latest
```

Then open `http://<host>:8080`. On Unraid, `unraid-template.xml` in this repo
sets the same thing up through Community Applications; `PUID`/`PGID` default to
Unraid's `nobody:users` so the database isn't left owned by root.

`docker compose up --build` also works and is what the included
`docker-compose.yml` is for.

### From source

```bash
npm --prefix web ci && npm --prefix web run build   # builds the UI
go build -o geet ./cmd/geet                         # embeds it in the binary
./geet serve --addr :8080 --db ./geet.db
```

Requires Go 1.25+ and Node 22+. The build is pure Go (`CGO_ENABLED=0` works),
because the SQLite driver is `modernc.org/sqlite` rather than a cgo binding.

### Development

Run the API and the Vite dev server side by side; Vite proxies `/api` to 8080.

```bash
go run ./cmd/geet serve          # terminal 1
npm --prefix web run dev         # terminal 2, then open http://localhost:5173
```

## CLI

Every command talks to a running server over HTTP, so the CLI and the web UI can
never disagree. Point it somewhere else with `--server` or `$GEET_URL`.

```bash
geet boards                                     # list boards
geet board tasks                                # columns, cards, nested sub-tasks
geet ls --type task --status todo --label infra
geet new "Fix the importer" --type task --parent T-4 --label infra
geet show T-12
geet edit T-12 --status in-progress
geet edit T-12 --desc-file notes.md             # or --desc-file - for stdin
geet comment T-12 "Leaning towards a subtree merge."
geet rm T-12
```

`--label` creates the label if it doesn't exist yet. `--json` works on every
read command. `edit` sends only the flags you passed, so changing a title never
touches the description.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `GEET_ADDR` | `:8080` | Listen address for `geet serve` |
| `GEET_DB` | `./geet.db` (`/data/geet.db` in the container) | SQLite file |
| `GEET_URL` | `http://localhost:8080` | Server the CLI talks to |
| `PUID` / `PGID` | `99` / `100` | Container only: who owns the database file |

There is no authentication. Bind it to your LAN, not the internet.

## Backups

Everything lives in the single SQLite file at `GEET_DB`. Stopping the container
checkpoints the write-ahead log into it, so a copy taken while geet is stopped
is a complete, self-contained backup. To copy it while running, use
`sqlite3 geet.db ".backup out.db"` rather than `cp`, which can catch a partial
write.

## Layout

```
cmd/geet/          entrypoint: `serve` plus the CLI commands
internal/store/    schema and every SQL statement, including board assembly
internal/api/      JSON API over the store
internal/cli/      CLI, an HTTP client of that API
web/               React + Vite UI, compiled into the binary via go:embed
embed.go           the go:embed directive and SPA handler
```

The rule worth knowing before changing anything: **the nest/break-out logic
lives only in `store.GetBoard`**. The web UI never recomputes it — after a card
moves, the server returns the reassembled board and the client renders what it's
given.

`go test ./...` covers the hierarchy rules, cascading deletes, partial updates,
board assembly in both directions, and column ordering.
