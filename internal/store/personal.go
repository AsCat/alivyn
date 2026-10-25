package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/AsCat/alivyn/internal/memory"
)

type PersonalFact struct {
	Key             string    `json:"key"`
	Value           string    `json:"value"`
	SourceMessageID int64     `json:"source_message_id"`
	SourceAt        time.Time `json:"source_at"`
}

func recordPersonalTx(ctx context.Context, tx *sql.Tx, change memory.PersonalChange, sourceID int64, at time.Time) error {
	if change.Forget {
		if _, err := tx.ExecContext(ctx, "DELETE FROM character_turns"); err != nil {
			return err
		}
		// Decisions can contain paraphrases of a forgotten fact. They are
		// derived state and can be planned again if a delivery is retried.
		if _, err := tx.ExecContext(ctx, "DELETE FROM actor_decisions"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM actor_continuity"); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT source_message_id,value FROM personal_memory WHERE memory_key = ? AND action = 'upsert'", change.Key)
		if err != nil {
			return err
		}
		var sources []int64
		var names []string
		for rows.Next() {
			var id int64
			var value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return err
			}
			sources = append(sources, id)
			if change.Key == "name" {
				for _, prefix := range []string{"用户希望被称为", "The user prefers to be called "} {
					if strings.HasPrefix(value, prefix) && len(value) > len(prefix) {
						names = append(names, strings.TrimPrefix(value, prefix))
					}
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		// Later recalls may repeat a name without creating a new personal fact.
		// Remove those echoes as well, including prepared channel replies.
		for _, name := range names {
			if _, err := tx.ExecContext(ctx, "UPDATE messages SET content='[forgotten memory]' WHERE id < ? AND instr(lower(content),lower(?))>0", sourceID, name); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply=NULL,state='sent' WHERE state IN ('pending','ready') AND instr(lower(COALESCE(reply,'')),lower(?))>0", name); err != nil {
				return err
			}
		}
		for _, id := range sources {
			// A response to the original disclosure may repeat the fact.
			if _, err := tx.ExecContext(ctx, `UPDATE messages SET content = '[forgotten memory]'
WHERE id >= ? AND id < COALESCE((SELECT MIN(id) FROM messages WHERE role = 'user' AND id > ?), 9223372036854775807)`, id, id); err != nil {
				return err
			}
			if err := redactLinkedReplyTx(ctx, tx, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE feishu_inbox SET reply = NULL, state = 'sent' WHERE user_message_id = ? AND state IN ('pending','ready')", id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM personal_memory WHERE memory_key = ?", change.Key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE messages SET content = '[forgotten memory]' WHERE id = ?", sourceID); err != nil {
			return err
		}
		// A prepared proactive message may have mentioned the forgotten fact.
		rows, err = tx.QueryContext(ctx, "SELECT DISTINCT topic FROM contact_intents WHERE state = 'pending'")
		if err != nil {
			return err
		}
		var topics []string
		for rows.Next() {
			var topic string
			if err := rows.Scan(&topic); err != nil {
				rows.Close()
				return err
			}
			topics = append(topics, topic)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, topic := range topics {
			if err := cancelPendingTopicTx(ctx, tx, topic, at, "personal memory forgotten"); err != nil {
				return err
			}
		}
		// The tombstone lets the immediate reply acknowledge forgetting without
		// retaining the forgotten subject in the database.
		change.Key = fmt.Sprintf("forgotten:%d", sourceID)
	}
	action := "upsert"
	if change.Forget {
		action = "forget"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO personal_memory(memory_key, action, value, source_message_id, source_at)
VALUES (?, ?, ?, ?, ?)`, change.Key, action, change.Value, sourceID, at.UTC().Format(time.RFC3339Nano))
	return err
}

func (h *History) ActivePersonal(ctx context.Context) ([]PersonalFact, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT memory_key, value, source_message_id, source_at
FROM personal_memory p WHERE action = 'upsert' AND id =
  (SELECT MAX(n.id) FROM personal_memory n WHERE n.memory_key = p.memory_key)
ORDER BY id DESC LIMIT 24`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var facts []PersonalFact
	for rows.Next() {
		var fact PersonalFact
		var stamp string
		if err := rows.Scan(&fact.Key, &fact.Value, &fact.SourceMessageID, &stamp); err != nil {
			return nil, err
		}
		fact.SourceAt, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return nil, fmt.Errorf("invalid personal memory timestamp: %w", err)
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}
