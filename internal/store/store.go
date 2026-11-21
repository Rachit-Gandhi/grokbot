package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Rachit-Gandhi/grokbot/internal/llm"
)

// Store persists per-thread chat sessions in SQLite.
type Store struct {
	db *sql.DB
}

// Open creates (if needed) and migrates a SQLite database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for sqlite: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id, id);
`)
	return err
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Append stores a message for a thread.
func (s *Store) Append(ctx context.Context, threadID, role, content string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (thread_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		threadID, role, content, time.Now().Unix(),
	)
	return err
}

// History returns the most recent limit messages for a thread, oldest first.
func (s *Store) History(ctx context.Context, threadID string, limit int) ([]llm.Message, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT role, content FROM (
  SELECT role, content, id FROM messages
  WHERE thread_id = ?
  ORDER BY id DESC
  LIMIT ?
) ORDER BY id ASC`, threadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []llm.Message
	for rows.Next() {
		var m llm.Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Clear removes all messages for a thread.
func (s *Store) Clear(ctx context.Context, threadID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE thread_id = ?`, threadID)
	return err
}

// Count returns how many messages are stored for a thread.
func (s *Store) Count(ctx context.Context, threadID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE thread_id = ?`, threadID).Scan(&n)
	return n, err
}
