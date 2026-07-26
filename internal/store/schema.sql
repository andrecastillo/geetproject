-- geetproject schema. Applied on open; see store.migrate().

-- Small key/value table, kept for future schema bookkeeping.
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Projects are the top-level container. Every ticket belongs to exactly one.
-- ticket_seq is this project's monotonic key counter: it only ever increments,
-- so a deleted ticket's key is never reissued to a different ticket.
CREATE TABLE IF NOT EXISTS project (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  slug       TEXT UNIQUE NOT NULL,
  prefix     TEXT UNIQUE NOT NULL,
  color      TEXT NOT NULL DEFAULT '',
  position   INTEGER NOT NULL,
  ticket_seq INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS status (
  id       INTEGER PRIMARY KEY,
  name     TEXT NOT NULL,
  slug     TEXT UNIQUE NOT NULL,
  color    TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL,
  is_done  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS ticket (
  id          INTEGER PRIMARY KEY,
  key         TEXT UNIQUE NOT NULL,
  project_id  INTEGER NOT NULL REFERENCES project(id) ON DELETE CASCADE,
  type        TEXT NOT NULL CHECK (type IN ('epic','task','subtask')),
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status_id   INTEGER NOT NULL REFERENCES status(id),
  parent_id   INTEGER REFERENCES ticket(id) ON DELETE CASCADE,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

-- A board is a saved view. project_id NULL means the cross-project "All
-- projects" scope; slug is unique per scope, not globally, because every
-- project wants a view called 'epics'.
CREATE TABLE IF NOT EXISTS board (
  id                INTEGER PRIMARY KEY,
  name              TEXT NOT NULL,
  slug              TEXT NOT NULL,
  project_id        INTEGER REFERENCES project(id) ON DELETE CASCADE,
  filter_type       TEXT NOT NULL DEFAULT 'any'
                    CHECK (filter_type IN ('epic','task','subtask','any')),
  filter_label_mode TEXT NOT NULL DEFAULT 'any'
                    CHECK (filter_label_mode IN ('any','all')),
  position          INTEGER NOT NULL,
  created_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS board_label (
  board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
  label_id INTEGER NOT NULL REFERENCES label(id) ON DELETE CASCADE,
  PRIMARY KEY (board_id, label_id)
);

CREATE TABLE IF NOT EXISTS board_status (
  board_id  INTEGER NOT NULL REFERENCES board(id)  ON DELETE CASCADE,
  status_id INTEGER NOT NULL REFERENCES status(id) ON DELETE CASCADE,
  position  INTEGER NOT NULL,
  PRIMARY KEY (board_id, status_id)
);

CREATE TABLE IF NOT EXISTS card_order (
  board_id  INTEGER NOT NULL REFERENCES board(id)  ON DELETE CASCADE,
  ticket_id INTEGER NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
  position  REAL NOT NULL,
  PRIMARY KEY (board_id, ticket_id)
);

CREATE TABLE IF NOT EXISTS label (
  id    INTEGER PRIMARY KEY,
  name  TEXT UNIQUE NOT NULL,
  color TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ticket_label (
  ticket_id INTEGER NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
  label_id  INTEGER NOT NULL REFERENCES label(id)  ON DELETE CASCADE,
  PRIMARY KEY (ticket_id, label_id)
);

CREATE TABLE IF NOT EXISTS comment (
  id         INTEGER PRIMARY KEY,
  ticket_id  INTEGER NOT NULL REFERENCES ticket(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ticket_parent ON ticket(parent_id);
CREATE INDEX IF NOT EXISTS idx_ticket_status ON ticket(status_id);
CREATE INDEX IF NOT EXISTS idx_ticket_type   ON ticket(type);
CREATE INDEX IF NOT EXISTS idx_comment_tkt   ON comment(ticket_id);

-- Indexes over project_id live in store.go, not here: this file runs before the
-- projects migration, and on an old database those columns don't exist yet.
