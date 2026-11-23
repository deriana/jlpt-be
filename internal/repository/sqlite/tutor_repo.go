package sqlite

import (
	"database/sql"
	"time"

	"jlptmaster/backend/internal/domain"
)

type TutorRepository struct {
	DB *sql.DB
}

func NewTutorRepository(db *sql.DB) *TutorRepository {
	return &TutorRepository{DB: db}
}

func (r *TutorRepository) ListMessages(userID int64, limit int) ([]domain.ChatMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.DB.Query(`
		SELECT id, user_id, role, content, clean_text, reading, emotion, mode, audio_url, created_at
		FROM chat_messages
		WHERE user_id = ?
		ORDER BY id ASC
		LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []domain.ChatMessage
	for rows.Next() {
		var m domain.ChatMessage
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.Content, &m.CleanText, &m.Reading, &m.Emotion, &m.Mode, &m.AudioURL, &m.CreatedAt); err == nil {
			msgs = append(msgs, m)
		}
	}
	if msgs == nil {
		msgs = []domain.ChatMessage{}
	}
	return msgs, nil
}

func (r *TutorRepository) SaveMessage(m *domain.ChatMessage) error {
	res, err := r.DB.Exec(`
		INSERT INTO chat_messages (user_id, role, content, clean_text, reading, emotion, mode, audio_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, m.UserID, m.Role, m.Content, m.CleanText, m.Reading, m.Emotion, m.Mode, m.AudioURL)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	m.CreatedAt = time.Now()
	return nil
}

func (r *TutorRepository) ClearHistory(userID int64) error {
	_, err := r.DB.Exec(`DELETE FROM chat_messages WHERE user_id = ?`, userID)
	return err
}
