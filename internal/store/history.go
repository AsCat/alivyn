package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type History struct {
	db *sql.DB
}

type Entry struct {
	ID        int64
	Role      string
	Content   string
	CreatedAt time.Time
}

func OpenHistory(ctx context.Context, dir string) (*History, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "alivyn.db")
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("database permissions are too open; expected 0600")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	h := &History{db: db}
	if _, err := db.ExecContext(ctx, `
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA secure_delete = ON;
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role TEXT NOT NULL CHECK(role IN ('user','assistant')),
  content TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS memory_revisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  topic TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('upsert','cancel','forget')),
  happens_at TEXT,
  precision TEXT,
  provenance TEXT NOT NULL CHECK(provenance IN ('user_stated','model_inferred')),
  source_message_id INTEGER REFERENCES messages(id),
  source_at TEXT NOT NULL,
  supersedes_id INTEGER REFERENCES memory_revisions(id)
);
CREATE TABLE IF NOT EXISTS contact_settings (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  paused INTEGER NOT NULL DEFAULT 0 CHECK(paused IN (0,1)),
  quiet_start TEXT NOT NULL DEFAULT '22:00',
  quiet_end TEXT NOT NULL DEFAULT '08:00',
  daily_limit INTEGER NOT NULL DEFAULT 2
);
INSERT OR IGNORE INTO contact_settings(id) VALUES(1);
CREATE TABLE IF NOT EXISTS contact_intents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  topic TEXT NOT NULL,
  source_revision_id INTEGER UNIQUE REFERENCES memory_revisions(id),
  due_ns INTEGER NOT NULL,
  expires_ns INTEGER NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sent','expired','canceled','failed')),
  reason TEXT NOT NULL DEFAULT '',
  idempotency_key TEXT NOT NULL UNIQUE,
  created_ns INTEGER NOT NULL,
  updated_ns INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS contact_intents_due ON contact_intents(state, due_ns);
CREATE TABLE IF NOT EXISTS contact_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  intent_id INTEGER NOT NULL REFERENCES contact_intents(id),
  outcome TEXT NOT NULL CHECK(outcome IN ('sent','skipped','canceled','failed')),
  reason TEXT NOT NULL,
  at_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS contact_outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  intent_id INTEGER NOT NULL UNIQUE REFERENCES contact_intents(id),
  message_id INTEGER NOT NULL UNIQUE REFERENCES messages(id),
  idempotency_key TEXT NOT NULL UNIQUE,
  topic TEXT NOT NULL,
  body TEXT NOT NULL,
  sent_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS contact_delivery (
  intent_id INTEGER PRIMARY KEY REFERENCES contact_intents(id),
  body TEXT NOT NULL,
  first_attempt_ns INTEGER NOT NULL,
  last_attempt_ns INTEGER NOT NULL,
  attempts INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('pending','sent','failed')),
  sent_ns INTEGER
);
CREATE TABLE IF NOT EXISTS runtime_state (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  last_active_ns INTEGER,
  last_gap_created_ns INTEGER
);
INSERT OR IGNORE INTO runtime_state(id) VALUES(1);
CREATE TABLE IF NOT EXISTS feishu_inbox (
  event_id TEXT PRIMARY KEY,
  chat_id TEXT NOT NULL,
  message_id TEXT NOT NULL,
  user_message_id INTEGER NOT NULL REFERENCES messages(id),
  reply TEXT,
  state TEXT NOT NULL CHECK(state IN ('pending','ready','sent')),
  received_ns INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS feishu_inbox_work ON feishu_inbox(state, received_ns);
CREATE TABLE IF NOT EXISTS personal_memory (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  memory_key TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('upsert','forget')),
  value TEXT NOT NULL,
  source_message_id INTEGER NOT NULL REFERENCES messages(id),
  source_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS personal_memory_key ON personal_memory(memory_key, id);
CREATE TABLE IF NOT EXISTS relationship_state (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  stage TEXT NOT NULL,
  meaningful_turns INTEGER NOT NULL,
  shared_days INTEGER NOT NULL,
  disclosures INTEGER NOT NULL,
  last_shared_day TEXT NOT NULL,
  reserved INTEGER NOT NULL CHECK(reserved IN (0,1))
);
CREATE TABLE IF NOT EXISTS relationship_meta (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  version INTEGER NOT NULL
);
`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize history: %w", err)
	}
	if err := h.ensureRelationship(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize relationship: %w", err)
	}
	return h, nil
}

func (h *History) Close() error { return h.db.Close() }

func (h *History) Add(ctx context.Context, role, content string, at time.Time) error {
	if role != "user" && role != "assistant" {
		return errors.New("invalid message role")
	}
	if content == "" {
		return errors.New("message cannot be empty")
	}
	_, err := h.db.ExecContext(ctx, "INSERT INTO messages(role, content, created_at) VALUES(?, ?, ?)", role, content, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (h *History) Recent(ctx context.Context, limit int) ([]Entry, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("history limit must be between 1 and 100")
	}
	rows, err := h.db.QueryContext(ctx, "SELECT id, role, content, created_at FROM messages ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reverse []Entry
	for rows.Next() {
		var entry Entry
		var stamp string
		if err := rows.Scan(&entry.ID, &entry.Role, &entry.Content, &stamp); err != nil {
			return nil, err
		}
		entry.CreatedAt, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return nil, fmt.Errorf("invalid stored timestamp: %w", err)
		}
		reverse = append(reverse, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(reverse)-1; left < right; left, right = left+1, right-1 {
		reverse[left], reverse[right] = reverse[right], reverse[left]
	}
	return reverse, nil
}
