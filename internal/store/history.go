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
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role TEXT NOT NULL CHECK(role IN ('user','assistant')),
  content TEXT NOT NULL,
  created_at TEXT NOT NULL
);`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize history: %w", err)
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
