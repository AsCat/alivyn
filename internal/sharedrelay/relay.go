package sharedrelay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const pairLifetime = 10 * time.Minute

type Sender interface {
	Send(context.Context, string, string) error
}

type IdempotentSender interface {
	SendIdempotent(context.Context, string, string, string) error
}

type Message struct {
	EventID   string `json:"event_id"`
	ChatID    string `json:"chat_id"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
}

type Pairing struct {
	Token     string    `json:"token"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	BotURL    string    `json:"bot_url"`
}

type queue struct {
	messages []queuedMessage
	notify   chan struct{}
	lastUsed time.Time
}

type queuedMessage struct {
	message Message
	until   time.Time
}

type Relay struct {
	db        *sql.DB
	botURL    string
	sender    Sender
	mu        sync.Mutex
	queues    map[string]*queue
	seen      map[string]time.Time
	lastPrune time.Time
}

func Open(ctx context.Context, dir, botURL string, sender Sender) (*Relay, error) {
	if sender == nil || botURL == "" {
		return nil, errors.New("relay needs a Feishu sender and bot URL")
	}
	u, err := url.Parse(botURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, errors.New("relay bot URL must be an HTTPS link")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "relay.db")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("relay database must be regular with 0600 permissions")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	file.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;
CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  code_hash TEXT UNIQUE,
  expires_ns INTEGER NOT NULL,
  open_id TEXT UNIQUE,
  chat_id TEXT,
  pairing_message_id TEXT,
  paired_ns INTEGER
);
CREATE INDEX IF NOT EXISTS agents_expire ON agents(expires_ns);`); err != nil {
		db.Close()
		return nil, err
	}
	return &Relay{db: db, botURL: botURL, sender: sender, queues: make(map[string]*queue), seen: make(map[string]time.Time)}, nil
}

func (r *Relay) Close() error { return r.db.Close() }

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (r *Relay) CreatePair(ctx context.Context, now time.Time) (Pairing, error) {
	if _, err := r.db.ExecContext(ctx, "DELETE FROM agents WHERE open_id IS NULL AND expires_ns < ?", now.UnixNano()); err != nil {
		return Pairing{}, err
	}
	var pending int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agents WHERE open_id IS NULL").Scan(&pending); err != nil {
		return Pairing{}, err
	}
	if pending >= 10000 {
		return Pairing{}, errors.New("too many pending pairings")
	}
	id, err := randomString(16)
	if err != nil {
		return Pairing{}, err
	}
	token, err := randomString(32)
	if err != nil {
		return Pairing{}, err
	}
	// Seven random bytes give a 56-bit, case-insensitive one-time code.
	codeBytes := make([]byte, 7)
	if _, err := rand.Read(codeBytes); err != nil {
		return Pairing{}, err
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(codeBytes)
	expires := now.Add(pairLifetime)
	_, err = r.db.ExecContext(ctx, "INSERT INTO agents(id, token_hash, code_hash, expires_ns) VALUES (?, ?, ?, ?)",
		id, digest(token), digest(code), expires.UnixNano())
	if err != nil {
		return Pairing{}, err
	}
	return Pairing{Token: token, Code: code[:6] + "-" + code[6:], ExpiresAt: expires, BotURL: r.botURL}, nil
}

func (r *Relay) Authenticate(ctx context.Context, token string) (string, error) {
	if len(token) < 32 || len(token) > 128 {
		return "", errors.New("invalid agent token")
	}
	var id string
	if err := r.db.QueryRowContext(ctx, "SELECT id FROM agents WHERE token_hash = ?", digest(token)).Scan(&id); err != nil {
		return "", errors.New("invalid agent token")
	}
	return id, nil
}

func normalizeCode(text string) string {
	text = strings.TrimSpace(strings.ToUpper(text))
	text = strings.TrimPrefix(text, "PAIR ")
	return strings.ReplaceAll(text, "-", "")
}

// Incoming is called only by the authenticated Feishu WebSocket adapter. It
// returns a brief bot response for pairing; ordinary messages go into RAM.
func (r *Relay) Incoming(ctx context.Context, openID string, msg Message, now time.Time) (string, error) {
	if openID == "" || msg.EventID == "" || msg.ChatID == "" || msg.MessageID == "" || strings.TrimSpace(msg.Text) == "" || len(msg.Text) > 65536 {
		return "", errors.New("invalid incoming Feishu message")
	}
	code := normalizeCode(msg.Text)
	if len(code) == 12 {
		paired, err := r.tryPair(ctx, openID, msg.ChatID, msg.MessageID, code, now)
		if err != nil {
			return "", err
		}
		if paired {
			return "Connected to Alivyn. Keep the local runtime running to receive replies. / 已连接意续；保持本地运行即可聊天。", nil
		}
	}
	var id, pairingMessageID string
	err := r.db.QueryRowContext(ctx, "SELECT id, pairing_message_id FROM agents WHERE open_id = ? AND chat_id = ?", openID, msg.ChatID).Scan(&id, &pairingMessageID)
	if err == nil {
		if msg.MessageID == pairingMessageID {
			return "", nil
		}
		r.enqueue(id, msg, now)
		return "", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if len(code) != 12 {
		return "Send the pairing code shown in Alivyn. / 请发送意续终端显示的配对码。", nil
	}
	return "Pairing code invalid or expired. / 配对码无效或已过期。", nil
}

func (r *Relay) tryPair(ctx context.Context, openID, chatID, messageID, code string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var targetID string
	err = tx.QueryRowContext(ctx, "SELECT id FROM agents WHERE code_hash = ? AND open_id IS NULL AND expires_ns >= ?", digest(code), now.UnixNano()).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agents SET open_id = NULL, chat_id = NULL, pairing_message_id = NULL, paired_ns = NULL WHERE open_id = ?", openID); err != nil {
		return false, fmt.Errorf("release previous Feishu binding: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET open_id = ?, chat_id = ?, pairing_message_id = ?, paired_ns = ?, code_hash = NULL
WHERE id = ?`, openID, chatID, messageID, now.UnixNano(), targetID); err != nil {
		return false, fmt.Errorf("pair Feishu user: %w", err)
	}
	return true, tx.Commit()
}

func (r *Relay) enqueue(id string, msg Message, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if until, exists := r.seen[msg.EventID]; exists && now.Before(until) {
		return
	}
	r.seen[msg.EventID] = now.Add(10 * time.Minute)
	if now.Sub(r.lastPrune) >= 10*time.Minute {
		for key, until := range r.seen {
			if !now.Before(until) {
				delete(r.seen, key)
			}
		}
		for key, old := range r.queues {
			old.dropExpired(now)
			if len(old.messages) == 0 && now.Sub(old.lastUsed) > time.Hour {
				delete(r.queues, key)
			}
		}
		r.lastPrune = now
	}
	q := r.queueLocked(id)
	q.dropExpired(now)
	if len(q.messages) >= 32 {
		q.messages = q.messages[1:]
	}
	q.messages = append(q.messages, queuedMessage{message: msg, until: now.Add(5 * time.Minute)})
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (r *Relay) queueLocked(id string) *queue {
	q := r.queues[id]
	if q == nil {
		q = &queue{notify: make(chan struct{}, 1)}
		r.queues[id] = q
	}
	q.lastUsed = time.Now()
	return q
}

func (q *queue) dropExpired(now time.Time) {
	kept := q.messages[:0]
	for _, item := range q.messages {
		if now.Before(item.until) {
			kept = append(kept, item)
		}
	}
	q.messages = kept
}

func copyMessages(items []queuedMessage) []Message {
	result := make([]Message, 0, len(items))
	for _, item := range items {
		result = append(result, item.message)
	}
	return result
}

func (r *Relay) Status(ctx context.Context, id string) (bool, error) {
	var paired sql.NullString
	err := r.db.QueryRowContext(ctx, "SELECT open_id FROM agents WHERE id = ?", id).Scan(&paired)
	return paired.Valid, err
}

func (r *Relay) Poll(ctx context.Context, id string) ([]Message, error) {
	r.mu.Lock()
	q := r.queueLocked(id)
	q.dropExpired(time.Now())
	if len(q.messages) > 0 {
		messages := copyMessages(q.messages)
		r.mu.Unlock()
		return messages, nil
	}
	notify := q.notify
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-notify:
	}
	r.mu.Lock()
	q.dropExpired(time.Now())
	messages := copyMessages(q.messages)
	r.mu.Unlock()
	return messages, nil
}

func (r *Relay) Ack(id string, eventIDs []string) {
	if len(eventIDs) == 0 {
		return
	}
	ack := make(map[string]bool, len(eventIDs))
	for _, eventID := range eventIDs {
		ack[eventID] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.queueLocked(id)
	kept := q.messages[:0]
	for _, item := range q.messages {
		if !ack[item.message.EventID] {
			kept = append(kept, item)
		}
	}
	q.messages = kept
}

func (r *Relay) Send(ctx context.Context, id, text string) error {
	if strings.TrimSpace(text) == "" || len(text) > 8192 {
		return errors.New("invalid outbound message")
	}
	var chatID string
	if err := r.db.QueryRowContext(ctx, "SELECT chat_id FROM agents WHERE id = ? AND open_id IS NOT NULL", id).Scan(&chatID); err != nil {
		return errors.New("agent is not paired")
	}
	return r.sender.Send(ctx, chatID, text)
}

func (r *Relay) SendProactive(ctx context.Context, id, text, key string) error {
	if strings.TrimSpace(text) == "" || len(text) > 8192 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return errors.New("invalid proactive message")
	}
	var chatID string
	if err := r.db.QueryRowContext(ctx, "SELECT chat_id FROM agents WHERE id = ? AND open_id IS NOT NULL", id).Scan(&chatID); err != nil {
		return errors.New("agent is not paired")
	}
	sender, ok := r.sender.(IdempotentSender)
	if !ok {
		return errors.New("relay sender does not support idempotent delivery")
	}
	sum := sha256.Sum256([]byte(id + ":" + key))
	return sender.SendIdempotent(ctx, chatID, text, hex.EncodeToString(sum[:]))
}
