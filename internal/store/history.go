package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type History struct {
	db         *sql.DB
	deliveryMu sync.RWMutex
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
CREATE TABLE IF NOT EXISTS local_commands (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  command TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS command_results (
  source_message_id INTEGER PRIMARY KEY REFERENCES local_commands(source_message_id),
  zh TEXT NOT NULL,
  en TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS character_control (
  id INTEGER PRIMARY KEY CHECK(id=1),
  mode TEXT NOT NULL DEFAULT 'running' CHECK(mode IN ('running','paused','diagnostic')),
  return_mode TEXT NOT NULL DEFAULT 'running',
  confirm_until_ns INTEGER NOT NULL DEFAULT 0,
  blocked_through_id INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO character_control(id) VALUES(1);
CREATE TABLE IF NOT EXISTS character_absence (
  id INTEGER PRIMARY KEY CHECK(id=1),
  source_message_id INTEGER NOT NULL REFERENCES messages(id),
  kind TEXT NOT NULL,
  reason TEXT NOT NULL,
  until_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS reply_deferrals (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  until_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS inbox_receipts (
  event_id TEXT PRIMARY KEY
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
  daily_limit INTEGER NOT NULL DEFAULT 12,
  limit_mode TEXT NOT NULL DEFAULT 'adaptive' CHECK(limit_mode IN ('adaptive','fixed'))
);
INSERT OR IGNORE INTO contact_settings(id) VALUES(1);
CREATE TABLE IF NOT EXISTS character_tuning (
  id INTEGER PRIMARY KEY CHECK(id=1),
  initiative INTEGER NOT NULL DEFAULT 25 CHECK(initiative BETWEEN 0 AND 100)
);
INSERT OR IGNORE INTO character_tuning(id) VALUES(1);
CREATE TABLE IF NOT EXISTS initiative_clock (
  id INTEGER PRIMARY KEY CHECK(id=1),
  next_check_ns INTEGER NOT NULL DEFAULT 0,
  budget_day TEXT NOT NULL DEFAULT '',
  checks INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO initiative_clock(id) VALUES(1);
CREATE TABLE IF NOT EXISTS initiative_test_request (
  id INTEGER PRIMARY KEY CHECK(id=1),
  requested_ns INTEGER NOT NULL,
  expires_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS initiative_test_intents (
  intent_id INTEGER PRIMARY KEY REFERENCES contact_intents(id)
);
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
CREATE TABLE IF NOT EXISTS initiative_checks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_message_id INTEGER NOT NULL REFERENCES messages(id),
  through_message_id INTEGER NOT NULL REFERENCES messages(id),
  at_ns INTEGER NOT NULL,
  outcome TEXT NOT NULL DEFAULT 'considering',
  reason TEXT NOT NULL DEFAULT '',
  proposal_json TEXT NOT NULL DEFAULT '',
  topic_key TEXT NOT NULL DEFAULT '',
  intent_id INTEGER REFERENCES contact_intents(id)
);
CREATE TABLE IF NOT EXISTS inner_wake_clock (
  id INTEGER PRIMARY KEY CHECK(id=1),
  next_check_ns INTEGER NOT NULL DEFAULT 0,
  budget_day TEXT NOT NULL DEFAULT '',
  checks INTEGER NOT NULL DEFAULT 0,
  last_source_id INTEGER NOT NULL DEFAULT 0
);
INSERT OR IGNORE INTO inner_wake_clock(id) VALUES(1);
CREATE TABLE IF NOT EXISTS inner_wakes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_message_id INTEGER NOT NULL REFERENCES messages(id),
  through_message_id INTEGER NOT NULL REFERENCES messages(id),
  at_ns INTEGER NOT NULL,
  state_json TEXT NOT NULL DEFAULT '',
  outcome TEXT NOT NULL DEFAULT 'considering' CHECK(outcome IN ('considering','done','superseded','error'))
);
CREATE INDEX IF NOT EXISTS inner_wakes_recent ON inner_wakes(outcome,at_ns);
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
CREATE TABLE IF NOT EXISTS contact_delivery_parts (
  intent_id INTEGER NOT NULL REFERENCES contact_intents(id),
  part_index INTEGER NOT NULL,
  text TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sent')),
  sent_ns INTEGER,
  PRIMARY KEY(intent_id,part_index)
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
CREATE TABLE IF NOT EXISTS feishu_reply_groups (
  event_id TEXT PRIMARY KEY REFERENCES feishu_inbox(event_id) ON DELETE CASCADE,
  anchor_event_id TEXT NOT NULL REFERENCES feishu_inbox(event_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS feishu_reply_parts (
  event_id TEXT NOT NULL REFERENCES feishu_inbox(event_id) ON DELETE CASCADE,
  part_index INTEGER NOT NULL,
  text TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sent')),
  PRIMARY KEY(event_id,part_index)
);
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
CREATE TABLE IF NOT EXISTS world_state (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  day INTEGER NOT NULL,
  local_date TEXT NOT NULL,
  progress INTEGER NOT NULL,
  last_user_turns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS world_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  day INTEGER NOT NULL,
  local_date TEXT NOT NULL,
  kind TEXT NOT NULL,
  zh TEXT NOT NULL,
  en TEXT NOT NULL,
  related_to_user INTEGER NOT NULL CHECK(related_to_user IN (0,1)),
  UNIQUE(day, kind)
);
CREATE TABLE IF NOT EXISTS actor_decisions (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  decision_json TEXT NOT NULL,
  decided_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS character_turns (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  turn_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  committed INTEGER NOT NULL DEFAULT 0 CHECK(committed IN (0,1))
);
CREATE TABLE IF NOT EXISTS character_self_facts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  fact_key TEXT NOT NULL,
  value TEXT NOT NULL,
  source_message_id INTEGER NOT NULL REFERENCES character_turns(source_message_id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  UNIQUE(source_message_id, fact_key)
);
CREATE INDEX IF NOT EXISTS character_self_facts_key ON character_self_facts(fact_key, source_message_id);
CREATE TABLE IF NOT EXISTS actor_continuity (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  state_json TEXT NOT NULL,
  expires_ns INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS relationship_snapshots (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  state_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS assistant_turns (
  source_message_id INTEGER PRIMARY KEY REFERENCES messages(id),
  assistant_message_id INTEGER NOT NULL UNIQUE REFERENCES messages(id)
);
CREATE TABLE IF NOT EXISTS goal_world_state (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  state_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS goal_world_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  day INTEGER NOT NULL UNIQUE,
  local_date TEXT NOT NULL,
  action TEXT NOT NULL,
  object_id TEXT NOT NULL,
  parent_event_id INTEGER REFERENCES goal_world_events(id),
  provenance TEXT NOT NULL,
  zh TEXT NOT NULL,
  en TEXT NOT NULL,
  related_to_user INTEGER NOT NULL CHECK(related_to_user IN (0,1)),
  catchup INTEGER NOT NULL CHECK(catchup IN (0,1))
);
CREATE TABLE IF NOT EXISTS goal_world_engagements (
  source_message_id INTEGER NOT NULL REFERENCES actor_decisions(source_message_id) ON DELETE CASCADE,
  object_id TEXT NOT NULL,
  event_id INTEGER NOT NULL REFERENCES goal_world_events(id),
  at_ns INTEGER NOT NULL,
  PRIMARY KEY(source_message_id, object_id)
);
CREATE TABLE IF NOT EXISTS life_reading_clock (
  id INTEGER PRIMARY KEY CHECK(id=1),
  generation INTEGER NOT NULL DEFAULT 0,
  claimed_date TEXT NOT NULL DEFAULT '',
  claimed_until_ns INTEGER NOT NULL DEFAULT 0,
  completed_date TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO life_reading_clock(id) VALUES(1);
CREATE TABLE IF NOT EXISTS life_readings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  local_date TEXT NOT NULL,
  occurred_ns INTEGER NOT NULL,
  title TEXT NOT NULL,
  source_url TEXT NOT NULL,
  excerpt TEXT NOT NULL,
  thought TEXT NOT NULL,
  feeling TEXT NOT NULL,
  question TEXT NOT NULL,
  UNIQUE(local_date),
  UNIQUE(source_url)
);
CREATE INDEX IF NOT EXISTS life_readings_time ON life_readings(occurred_ns);
`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize history: %w", err)
	}
	if err := h.ensureContactPolicy(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize contact policy: %w", err)
	}
	if err := h.ensureRelationship(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize relationship: %w", err)
	}
	if err := h.ensureExperienceEvents(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize experience events: %w", err)
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
	return h.recent(ctx, limit, 0)
}

// RecentThrough excludes messages written after the specified user message.
// This keeps an inbox worker from answering a later queued message first.
func (h *History) RecentThrough(ctx context.Context, limit int, throughID int64) ([]Entry, error) {
	if throughID < 1 {
		return nil, errors.New("history cutoff must be positive")
	}
	return h.recent(ctx, limit, throughID)
}

func (h *History) PriorUserTurns(ctx context.Context, beforeID int64) (int, error) {
	if beforeID < 1 {
		return 0, errors.New("history cutoff must be positive")
	}
	var count int
	err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages
WHERE role = 'user' AND id < ? AND content != '[forgotten memory]' AND NOT EXISTS(SELECT 1 FROM local_commands c WHERE c.source_message_id=messages.id)`, beforeID).Scan(&count)
	return count, err
}

func (h *History) recent(ctx context.Context, limit int, throughID int64) ([]Entry, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("history limit must be between 1 and 100")
	}
	query := "SELECT id, role, content, created_at FROM messages WHERE (? = 0 OR id <= ?) ORDER BY id DESC LIMIT ?"
	rows, err := h.db.QueryContext(ctx, query, throughID, throughID, limit)
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
