package sqlite

import (
	"database/sql"
	"time"

	"jlptmaster/backend/internal/domain"
)

type SRSRepository struct {
	DB *sql.DB
}

func NewSRSRepository(db *sql.DB) *SRSRepository {
	return &SRSRepository{DB: db}
}

func (r *SRSRepository) ListDecks(userID int64) ([]domain.Deck, error) {
	rows, err := r.DB.Query(`
		SELECT d.id, d.user_id, d.name, d.description, d.jlpt_level, d.category, d.created_at,
		       COUNT(f.id) AS total_cards,
		       SUM(CASE WHEN f.next_review_at <= CURRENT_TIMESTAMP THEN 1 ELSE 0 END) AS due_cards,
		       SUM(CASE WHEN f.interval_days >= 7 THEN 1 ELSE 0 END) AS mastered_cards
		FROM decks d
		LEFT JOIN flashcards f ON f.deck_id = d.id
		WHERE d.user_id = ?
		GROUP BY d.id
		ORDER BY d.id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decks []domain.Deck
	for rows.Next() {
		var d domain.Deck
		var due, mastered sql.NullInt64
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Description, &d.JLPTLevel, &d.Category, &d.CreatedAt, &d.TotalCards, &due, &mastered); err == nil {
			d.DueCards = int(due.Int64)
			d.Mastered = int(mastered.Int64)
			decks = append(decks, d)
		}
	}
	if decks == nil {
		decks = []domain.Deck{}
	}
	return decks, nil
}

func (r *SRSRepository) CreateDeck(deck *domain.Deck) error {
	res, err := r.DB.Exec(`
		INSERT INTO decks (user_id, name, description, jlpt_level, category)
		VALUES (?, ?, ?, ?, ?)
	`, deck.UserID, deck.Name, deck.Description, deck.JLPTLevel, deck.Category)
	if err != nil {
		return err
	}
	deck.ID, _ = res.LastInsertId()
	deck.CreatedAt = time.Now()
	return nil
}

func (r *SRSRepository) ListCards(userID int64, deckID int64, onlyDue bool) ([]domain.Flashcard, error) {
	query := `
		SELECT id, deck_id, user_id, front_kanji, furigana_reading, romaji, back_meaning, jlpt_level, pos,
		       context_sentence, context_reading, context_meaning, media_source_id, media_title, media_type,
		       media_audio_url, start_time_ms, end_time_ms, timestamp_label, interval_days, ease_factor,
		       repetitions, lapses, last_quality, next_review_at
		FROM flashcards
		WHERE user_id = ?`
	args := []interface{}{userID}

	if deckID > 0 {
		query += " AND deck_id = ?"
		args = append(args, deckID)
	}
	if onlyDue {
		query += " AND next_review_at <= CURRENT_TIMESTAMP"
	}
	query += " ORDER BY next_review_at ASC, id ASC"

	rows, err := r.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var cards []domain.Flashcard
	for rows.Next() {
		var c domain.Flashcard
		if err := rows.Scan(
			&c.ID, &c.DeckID, &c.UserID, &c.FrontKanji, &c.FuriganaReading, &c.Romaji, &c.BackMeaning,
			&c.JLPTLevel, &c.POS, &c.ContextSentence, &c.ContextReading, &c.ContextMeaning,
			&c.MediaSourceID, &c.MediaTitle, &c.MediaType, &c.MediaAudioURL, &c.StartTimeMs, &c.EndTimeMs,
			&c.TimestampLabel, &c.IntervalDays, &c.EaseFactor, &c.Repetitions, &c.Lapses, &c.LastQuality, &c.NextReviewAt,
		); err == nil {
			c.IsDue = !c.NextReviewAt.After(now)
			cards = append(cards, c)
		}
	}
	if cards == nil {
		cards = []domain.Flashcard{}
	}
	return cards, nil
}

func (r *SRSRepository) GetCardByID(cardID int64) (*domain.Flashcard, error) {
	var c domain.Flashcard
	err := r.DB.QueryRow(`
		SELECT id, deck_id, user_id, front_kanji, furigana_reading, romaji, back_meaning, jlpt_level, pos,
		       context_sentence, context_reading, context_meaning, media_source_id, media_title, media_type,
		       media_audio_url, start_time_ms, end_time_ms, timestamp_label, interval_days, ease_factor,
		       repetitions, lapses, last_quality, next_review_at
		FROM flashcards WHERE id = ?
	`, cardID).Scan(
		&c.ID, &c.DeckID, &c.UserID, &c.FrontKanji, &c.FuriganaReading, &c.Romaji, &c.BackMeaning,
		&c.JLPTLevel, &c.POS, &c.ContextSentence, &c.ContextReading, &c.ContextMeaning,
		&c.MediaSourceID, &c.MediaTitle, &c.MediaType, &c.MediaAudioURL, &c.StartTimeMs, &c.EndTimeMs,
		&c.TimestampLabel, &c.IntervalDays, &c.EaseFactor, &c.Repetitions, &c.Lapses, &c.LastQuality, &c.NextReviewAt,
	)
	if err != nil {
		return nil, err
	}
	c.IsDue = !c.NextReviewAt.After(time.Now())
	return &c, nil
}

func (r *SRSRepository) CreateCard(c *domain.Flashcard) error {
	if c.EaseFactor <= 0 {
		c.EaseFactor = 2.5
	}
	res, err := r.DB.Exec(`
		INSERT INTO flashcards (
			deck_id, user_id, front_kanji, furigana_reading, romaji, back_meaning, jlpt_level, pos,
			context_sentence, context_reading, context_meaning, media_source_id, media_title, media_type,
			media_audio_url, start_time_ms, end_time_ms, timestamp_label, interval_days, ease_factor,
			repetitions, lapses, next_review_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, CURRENT_TIMESTAMP)
	`, c.DeckID, c.UserID, c.FrontKanji, c.FuriganaReading, c.Romaji, c.BackMeaning, c.JLPTLevel, c.POS,
		c.ContextSentence, c.ContextReading, c.ContextMeaning, c.MediaSourceID, c.MediaTitle, c.MediaType,
		c.MediaAudioURL, c.StartTimeMs, c.EndTimeMs, c.TimestampLabel, c.IntervalDays, c.EaseFactor)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	c.NextReviewAt = time.Now()
	c.IsDue = true
	return nil
}

func (r *SRSRepository) UpdateCardSRS(c *domain.Flashcard, durationSecs int, isOffline bool) error {
	_, err := r.DB.Exec(`
		UPDATE flashcards
		SET interval_days = ?, ease_factor = ?, repetitions = ?, lapses = ?, last_quality = ?, next_review_at = ?
		WHERE id = ?
	`, c.IntervalDays, c.EaseFactor, c.Repetitions, c.Lapses, c.LastQuality, c.NextReviewAt.Format("2006-01-02 15:04:05"), c.ID)
	if err != nil {
		return err
	}

	offInt := 0
	if isOffline {
		offInt = 1
	}
	_, _ = r.DB.Exec(`
		INSERT INTO review_logs (user_id, card_id, quality, interval_days, ease_factor, duration_secs, is_offline)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, c.UserID, c.ID, c.LastQuality, c.IntervalDays, c.EaseFactor, durationSecs, offInt)

	today := time.Now().Format("2006-01-02")
	addMin := 1
	if durationSecs > 60 {
		addMin = durationSecs / 60
	}
	_, _ = r.DB.Exec(`
		INSERT INTO daily_activity (user_id, date, cards_reviewed, minutes_studied, sentences_mined, offline_sessions, accuracy_pct)
		VALUES (?, ?, 1, ?, 0, ?, 90)
		ON CONFLICT(user_id, date) DO UPDATE SET
			cards_reviewed = daily_activity.cards_reviewed + 1,
			minutes_studied = daily_activity.minutes_studied + ?,
			offline_sessions = daily_activity.offline_sessions + ?
	`, c.UserID, today, addMin, offInt, addMin, offInt)

	return nil
}
