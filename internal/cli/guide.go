package cli

// AgentGuide is printed by `geetproject agent-guide`. It is the interface description
// meant to be read by an LLM: complete enough to use the tool correctly without
// guessing, short enough to paste into a system prompt or a CLAUDE.md.
//
// Keep it in step with the commands. It is documentation that ships inside the
// binary precisely so it cannot be left behind in a separate file.
const AgentGuide = `# geetproject — task tracker CLI

geetproject tracks tickets in a local SQLite database behind a small HTTP server.
Every command below talks to that server, so the CLI and the web UI never
disagree. If a command reports it cannot reach geetproject, the server is not running:
say so rather than retrying.

## Model

- **Project** — the top-level container. Every ticket belongs to exactly one.
  Ticket keys are per project: MAI-1, MAI-2 in "mai", KG-1 in "mini-kg".
- **Ticket** — one of three types, nested exactly three levels deep:
  - "epic"    — a container of tasks. Cannot have a parent.
  - "task"    — real work. May sit under an epic, or stand alone.
  - "subtask" — a step within a task. Must have a task parent.
- A child always lives in its parent's project. Moving a ticket moves its
  whole subtree.
- **Status** — one of: backlog, todo, in-progress, done. Defaults to backlog.
- **Label** — a free-form tag, shared across all projects, created on first use.

Keys are never reused, so a key written into a description keeps pointing at the
same ticket forever. Refer to tickets by key (MAI-12), never by title.

## Creating a batch (the main entry point)

Pipe JSON on stdin. One call creates a whole tree in a single transaction: if
anything is wrong, nothing is written.

    geetproject import --project mai <<'EOF'
    {
      "tickets": [
        {
          "type": "epic",
          "title": "Chat UI",
          "description": "Front the knowledge graph with a chat interface.",
          "labels": ["app"],
          "children": [
            {
              "type": "task",
              "title": "Flask /chat route",
              "status": "in-progress",
              "children": [
                { "title": "Stream tokens back to the client" },
                { "title": "Wire up the tool schema" }
              ]
            }
          ]
        }
      ]
    }
    EOF

Every field except "title" is optional:

- "type"        — inferred when omitted: a child of an epic is a task, a child
                  of a task is a subtask, and a top-level entry is an epic if it
                  has children or a task if it does not.
- "description" — markdown. Use it for the detail; keep titles to one line.
- "status"      — backlog | todo | in-progress | done.
- "labels"      — array of names; unknown ones are created.
- "children"    — nested tickets, same shape.

A bare JSON array is accepted in place of {"tickets": [...]}.

**Always dry-run first.** It validates the whole batch, prints the keys it would
assign, and writes nothing:

    geetproject import --project mai --dry-run < plan.json

A rejected batch reports every problem at once, each naming the offending node
(e.g. "tickets[0].children[1]: unknown status \"doing\""). Fix them all and
re-run; a failed import leaves nothing behind, so retrying is safe.

## Single tickets

    geetproject new "Fix the importer" --project mai --type task --label infra
    geetproject new "Stream tokens" --parent MAI-4        # joins its parent's project
    geetproject show MAI-12
    geetproject edit MAI-12 --status in-progress
    geetproject edit MAI-12 --title "New title"           # only the flags you pass change
    geetproject edit MAI-12 --desc-file notes.md          # or --desc-file - to read stdin
    geetproject comment MAI-12 "Tried the subtree merge; it works."
    geetproject rm MAI-12 --force

edit only touches the flags given, so changing a title never clears a
description. There is no need to re-send fields you are not changing.

## Reading

    geetproject projects                                  # slugs, prefixes, ticket counts
    geetproject ls --project mai                          # add --type/--status/--label/--search
    geetproject ls --project mai --status todo --json
    geetproject boards --project mai                      # the saved views in a project
    geetproject board mai epics                           # a view's columns and cards

Every read command takes --json for machine-readable output. Prefer it when you
intend to parse the result.

## Projects

    geetproject projects
    geetproject project new "mini-kg" --prefix KG
    geetproject project rm mai --force                    # deletes its tickets too

Create a project before importing into it. Ask before deleting one: it destroys
every ticket it holds.

## Conventions

- Set GEETPROJECT_PROJECT to avoid repeating --project.
- Set GEETPROJECT_URL if the server is not at http://localhost:8080.
- Exit status is 0 on success, 1 on failure, with the reason on stderr.
- Write a genuine description on anything non-obvious. A title alone is rarely
  enough for the person who picks the ticket up later.
- Prefer one import of a considered tree over many one-off "new" calls.
`
