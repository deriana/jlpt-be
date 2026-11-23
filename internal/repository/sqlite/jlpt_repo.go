package sqlite

import (
	"database/sql"
	"encoding/json"
	"strings"
	"sync"

	"jlptmaster/backend/internal/domain"
)

type JLPTRepository struct {
	DB          *sql.DB
	mu          sync.RWMutex
	vocabIndex  map[string]domain.VocabItem
	indexLoaded bool
}

func NewJLPTRepository(db *sql.DB) *JLPTRepository {
	return &JLPTRepository{
		DB:         db,
		vocabIndex: make(map[string]domain.VocabItem),
	}
}

func (r *JLPTRepository) SearchVocab(userID int64, level string, query string, status string, limit int, offset int) ([]domain.VocabItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	where := []string{"1=1"}
	args := []interface{}{userID}

	if level != "" && level != "ALL" {
		where = append(where, "v.level = ?")
		args = append(args, strings.ToUpper(level))
	}
	if query != "" {
		q := "%" + strings.TrimSpace(query) + "%"
		where = append(where, "(v.word LIKE ? OR v.reading LIKE ? OR v.romaji LIKE ? OR v.meanings LIKE ?)")
		args = append(args, q, q, q, q)
	}
	if status != "" && status != "ALL" {
		if status == "unseen" {
			where = append(where, "m.status IS NULL")
		} else {
			where = append(where, "m.status = ?")
			args = append(args, status)
		}
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countSQL := `
		SELECT COUNT(*) FROM vocab v
		LEFT JOIN user_item_mastery m ON m.user_id = ? AND m.item_type = 'vocab' AND m.item_id = v.id
		WHERE ` + whereClause
	_ = r.DB.QueryRow(countSQL, args...).Scan(&total)

	querySQL := `
		SELECT v.id, v.word, v.reading, v.romaji, v.meanings, v.level, v.pos, v.jmdict_id, v.examples, COALESCE(m.status, 'unseen')
		FROM vocab v
		LEFT JOIN user_item_mastery m ON m.user_id = ? AND m.item_type = 'vocab' AND m.item_id = v.id
		WHERE ` + whereClause + `
		ORDER BY CASE v.level WHEN 'N4' THEN 1 WHEN 'N3' THEN 2 WHEN 'N5' THEN 3 WHEN 'N2' THEN 4 ELSE 5 END, v.word ASC
		LIMIT ? OFFSET ?`

	queryArgs := append(args, limit, offset)
	rows, err := r.DB.Query(querySQL, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []domain.VocabItem
	for rows.Next() {
		var item domain.VocabItem
		var meaningsJSON, posJSON, examplesJSON string
		if err := rows.Scan(&item.ID, &item.Word, &item.Reading, &item.Romaji, &meaningsJSON, &item.Level, &posJSON, &item.JMDictID, &examplesJSON, &item.MasteryStatus); err == nil {
			_ = json.Unmarshal([]byte(meaningsJSON), &item.Meanings)
			_ = json.Unmarshal([]byte(posJSON), &item.POS)
			_ = json.Unmarshal([]byte(examplesJSON), &item.Examples)
			items = append(items, item)
		}
	}
	if items == nil {
		items = []domain.VocabItem{}
	}
	return items, total, nil
}

func (r *JLPTRepository) SearchKanji(userID int64, level string, query string, limit int, offset int) ([]domain.KanjiItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	where := []string{"1=1"}
	args := []interface{}{userID}

	if level != "" && level != "ALL" {
		where = append(where, "k.level = ?")
		args = append(args, strings.ToUpper(level))
	}
	if query != "" {
		q := "%" + strings.TrimSpace(query) + "%"
		where = append(where, "(k.character LIKE ? OR k.onyomi LIKE ? OR k.kunyomi LIKE ? OR k.meanings LIKE ?)")
		args = append(args, q, q, q, q)
	}

	whereClause := strings.Join(where, " AND ")
	var total int
	_ = r.DB.QueryRow(`SELECT COUNT(*) FROM kanji k LEFT JOIN user_item_mastery m ON m.user_id = ? AND m.item_type = 'kanji' AND m.item_id = k.character WHERE `+whereClause, args...).Scan(&total)

	querySQL := `
		SELECT k.character, k.level, k.strokes, k.grade, k.freq, k.radical, k.onyomi, k.kunyomi, k.meanings, k.words, COALESCE(m.status, 'unseen')
		FROM kanji k
		LEFT JOIN user_item_mastery m ON m.user_id = ? AND m.item_type = 'kanji' AND m.item_id = k.character
		WHERE ` + whereClause + `
		ORDER BY CASE k.level WHEN 'N4' THEN 1 WHEN 'N3' THEN 2 WHEN 'N5' THEN 3 ELSE 4 END, k.freq ASC
		LIMIT ? OFFSET ?`

	rows, err := r.DB.Query(querySQL, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []domain.KanjiItem
	for rows.Next() {
		var item domain.KanjiItem
		var onJSON, kunJSON, meanJSON, wordsJSON string
		if err := rows.Scan(&item.Character, &item.Level, &item.Strokes, &item.Grade, &item.Freq, &item.Radical, &onJSON, &kunJSON, &meanJSON, &wordsJSON, &item.MasteryStatus); err == nil {
			_ = json.Unmarshal([]byte(onJSON), &item.Onyomi)
			_ = json.Unmarshal([]byte(kunJSON), &item.Kunyomi)
			_ = json.Unmarshal([]byte(meanJSON), &item.Meanings)
			_ = json.Unmarshal([]byte(wordsJSON), &item.Words)
			items = append(items, item)
		}
	}
	if items == nil {
		items = []domain.KanjiItem{}
	}
	return items, total, nil
}

func (r *JLPTRepository) SearchGrammar(userID int64, level string, query string, limit int, offset int) ([]domain.GrammarItem, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := []string{"1=1"}
	args := []interface{}{userID}

	if level != "" && level != "ALL" {
		where = append(where, "g.level = ?")
		args = append(args, strings.ToUpper(level))
	}
	if query != "" {
		q := "%" + strings.TrimSpace(query) + "%"
		where = append(where, "(g.pattern LIKE ? OR g.romaji LIKE ? OR g.meaning LIKE ? OR g.formation LIKE ?)")
		args = append(args, q, q, q, q)
	}

	whereClause := strings.Join(where, " AND ")
	var total int
	_ = r.DB.QueryRow(`SELECT COUNT(*) FROM grammar g LEFT JOIN user_item_mastery m ON m.user_id = ? AND m.item_type = 'grammar' AND m.item_id = g.id WHERE `+whereClause, args...).Scan(&total)

	querySQL := `
		SELECT g.id, g.pattern, g.reading, g.romaji, g.level, g.meaning, g.formation, g.examples, g.tags, g.notes, COALESCE(m.status, 'unseen')
		FROM grammar g
		LEFT JOIN user_item_mastery m ON m.user_id = ? AND m.item_type = 'grammar' AND m.item_id = g.id
		WHERE ` + whereClause + `
		ORDER BY CASE g.level WHEN 'N4' THEN 1 WHEN 'N3' THEN 2 WHEN 'N5' THEN 3 ELSE 4 END, g.pattern ASC
		LIMIT ? OFFSET ?`

	rows, err := r.DB.Query(querySQL, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []domain.GrammarItem
	for rows.Next() {
		var item domain.GrammarItem
		var exJSON, tagsJSON string
		if err := rows.Scan(&item.ID, &item.Pattern, &item.Reading, &item.Romaji, &item.Level, &item.Meaning, &item.Formation, &exJSON, &tagsJSON, &item.Notes, &item.MasteryStatus); err == nil {
			_ = json.Unmarshal([]byte(exJSON), &item.Examples)
			_ = json.Unmarshal([]byte(tagsJSON), &item.Tags)
			items = append(items, item)
		}
	}
	if items == nil {
		items = []domain.GrammarItem{}
	}
	return items, total, nil
}

func (r *JLPTRepository) ToggleMastery(userID int64, itemType string, itemID string, level string, status string) error {
	_, err := r.DB.Exec(`
		INSERT INTO user_item_mastery (user_id, item_type, item_id, level, status, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, item_type, item_id) DO UPDATE SET status = excluded.status, updated_at = CURRENT_TIMESTAMP
	`, userID, itemType, itemID, level, status)
	return err
}

// GetVocabMatcherIndex loads and caches an in-memory map of OpenJLPT words (prioritizing N4 & N3)
// for instant (<1ms) longest-match sentence tokenization during Anime & Music Sentence Mining.
func (r *JLPTRepository) GetVocabMatcherIndex() (map[string]domain.VocabItem, error) {
	r.mu.RLock()
	if r.indexLoaded && len(r.vocabIndex) > 0 {
		defer r.mu.RUnlock()
		return r.vocabIndex, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexLoaded && len(r.vocabIndex) > 0 {
		return r.vocabIndex, nil
	}

	rows, err := r.DB.Query(`
		SELECT id, word, reading, romaji, meanings, level, pos, other_forms
		FROM vocab
		ORDER BY CASE level WHEN 'N4' THEN 1 WHEN 'N3' THEN 2 WHEN 'N5' THEN 3 WHEN 'N2' THEN 4 ELSE 5 END
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	idx := make(map[string]domain.VocabItem, 12000)
	for rows.Next() {
		var item domain.VocabItem
		var meanJSON, posJSON, otherJSON string
		if err := rows.Scan(&item.ID, &item.Word, &item.Reading, &item.Romaji, &meanJSON, &item.Level, &posJSON, &otherJSON); err == nil {
			_ = json.Unmarshal([]byte(meanJSON), &item.Meanings)
			_ = json.Unmarshal([]byte(posJSON), &item.POS)

			// Only index terms with >= 2 runes OR kanji characters to avoid single-kana particle noise
			runes := []rune(item.Word)
			if len(runes) >= 2 || (len(runes) == 1 && isKanjiRune(runes[0])) {
				if _, exists := idx[item.Word]; !exists {
					idx[item.Word] = item
				}
			}
			// Also index verb stems (dropping final う-row/る for godan/ichidan conjugated matches)
			if len(runes) >= 2 && isKanjiRune(runes[0]) {
				stem := string(runes[:len(runes)-1])
				if len([]rune(stem)) >= 1 {
					if _, exists := idx[stem]; !exists {
						idx[stem] = item
					}
				}
			}
		}
	}
	r.vocabIndex = idx
	r.indexLoaded = true
	return r.vocabIndex, nil
}

func isKanjiRune(ch rune) bool {
	return (ch >= 0x4E00 && ch <= 0x9FFF) || (ch >= 0x3400 && ch <= 0x4DBF)
}
