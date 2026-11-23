package sqlite

import (
	"database/sql"
	"encoding/json"
	"time"

	"jlptmaster/backend/internal/domain"
)

type MiningRepository struct {
	DB *sql.DB
}

func NewMiningRepository(db *sql.DB) *MiningRepository {
	return &MiningRepository{DB: db}
}

func (r *MiningRepository) ListMinedSentences(userID int64) ([]domain.MinedSentence, error) {
	rows, err := r.DB.Query(`
		SELECT id, user_id, media_title, media_type, sentence, reading, translation,
		       COALESCE(source_type, 'softsub'), COALESCE(is_edited, 0),
		       start_time_ms, end_time_ms, timestamp_label, audio_url, matched_vocab_json,
		       target_level, saved_to_deck_id, created_at
		FROM mined_sentences
		WHERE user_id = ?
		ORDER BY id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.MinedSentence
	for rows.Next() {
		var m domain.MinedSentence
		var tokJSON string
		var isEditedInt int
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.MediaTitle, &m.MediaType, &m.Sentence, &m.Reading, &m.Translation,
			&m.SourceType, &isEditedInt,
			&m.StartTimeMs, &m.EndTimeMs, &m.TimestampLabel, &m.AudioURL, &tokJSON,
			&m.TargetLevel, &m.SavedToDeckID, &m.CreatedAt,
		); err == nil {
			m.IsEdited = isEditedInt != 0
			if m.SourceType == "" {
				m.SourceType = "softsub"
			}
			_ = json.Unmarshal([]byte(tokJSON), &m.MatchedVocab)
			if m.MatchedVocab == nil {
				m.MatchedVocab = []domain.MatchedVocabToken{}
			}
			list = append(list, m)
		}
	}
	if list == nil {
		list = []domain.MinedSentence{}
	}
	return list, nil
}

func (r *MiningRepository) CreateMinedSentence(m *domain.MinedSentence) error {
	if m.SourceType != "hardsub" && m.SourceType != "softsub" {
		m.SourceType = "softsub"
	}
	isEditedInt := 0
	if m.IsEdited {
		isEditedInt = 1
	}
	tokBytes, _ := json.Marshal(m.MatchedVocab)
	res, err := r.DB.Exec(`
		INSERT INTO mined_sentences (
			user_id, media_title, media_type, sentence, reading, translation,
			source_type, is_edited,
			start_time_ms, end_time_ms, timestamp_label, audio_url, matched_vocab_json,
			target_level, saved_to_deck_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.UserID, m.MediaTitle, m.MediaType, m.Sentence, m.Reading, m.Translation,
		m.SourceType, isEditedInt,
		m.StartTimeMs, m.EndTimeMs, m.TimestampLabel, m.AudioURL, string(tokBytes),
		m.TargetLevel, m.SavedToDeckID)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	m.CreatedAt = time.Now()

	today := time.Now().Format("2006-01-02")
	_, _ = r.DB.Exec(`
		INSERT INTO daily_activity (user_id, date, cards_reviewed, minutes_studied, sentences_mined, offline_sessions, accuracy_pct)
		VALUES (?, ?, 0, 2, 1, 1, 90)
		ON CONFLICT(user_id, date) DO UPDATE SET
			sentences_mined = daily_activity.sentences_mined + 1,
			minutes_studied = daily_activity.minutes_studied + 2
	`, m.UserID, today)

	return nil
}

func (r *MiningRepository) UpdateMinedSentence(userID int64, id int64, sentence string, translation string, matched []domain.MatchedVocabToken) error {
	tokBytes, _ := json.Marshal(matched)
	_, err := r.DB.Exec(`
		UPDATE mined_sentences
		SET sentence = ?, translation = ?, matched_vocab_json = ?, is_edited = 1
		WHERE id = ? AND user_id = ?
	`, sentence, translation, string(tokBytes), id, userID)
	return err
}
