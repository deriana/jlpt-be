package sqlite

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"jlptmaster/backend/internal/domain"
)

var noisyJMDictMetaRegex = regexp.MustCompile(`(?i)\((female|male|fem|masc|humble|honorable|respectful|polite|informal|familiar|archaic|obsolete|abbreviated|abbreviation|colloquial|slang|vulgar|derogatory|poetic|uk|id|usually written using kana alone|used by [^)]+|used for [^)]+|used in [^)]+|used with [^)]+|not used for [^)]+|daytime greeting[^)]*|of three or more|of an animal|of birds|faster than[^)]*|stopping at[^)]*)\)`)

var curatedCommonSenseMeanings = map[string][]string{
	"まあ":   {"well; I think; my!", "yah; lumayan; tenang saja"},
	"兼ねて":  {"at the same time; also serving as", "sekalian; sekaligus (merangkap)"},
	"兼ねる":  {"to serve as both; to combine", "merangkap; sekaligus sebagai"},
	"詫び":   {"apology; excuse", "permintaan maaf; maaf"},
	"お詫び":  {"apology", "permintaan maaf; maaf"},
	"詫びる":  {"to apologize", "meminta maaf"},
	"上着":   {"jacket; coat; outerwear", "jaket; pakaian luar"},
	"勝手":   {"one's own way; without permission", "seenaknya; tanpa izin"},
	"やっぱり": {"as expected; after all", "ternyata memang; seperti dugaanku"},
	"やっぱ":  {"as expected; after all", "ternyata memang; seperti dugaanku"},
	"いっぱい": {"full; a lot; many", "banyak; penuh"},
}

// CleanDictionaryMeanings strips confusing raw JMdict metadata tags like "(female)", "(humble)", "(archaic)"
// and prioritizes the top 2-3 most relevant, concise definitions.
func CleanDictionaryMeanings(word string, rawMeanings []string) []string {
	if curated, ok := curatedCommonSenseMeanings[word]; ok {
		return curated
	}
	var cleaned []string
	seen := make(map[string]bool)
	for _, m := range rawMeanings {
		c := noisyJMDictMetaRegex.ReplaceAllString(m, "")
		c = strings.TrimSpace(strings.Join(strings.Fields(c), " "))
		c = strings.Trim(c, ";, ")
		if c == "" {
			continue
		}
		lower := strings.ToLower(c)
		if !seen[lower] {
			seen[lower] = true
			cleaned = append(cleaned, c)
		}
		if len(cleaned) >= 3 {
			break
		}
	}
	if len(cleaned) == 0 && len(rawMeanings) > 0 {
		return rawMeanings[:1]
	}
	return cleaned
}

type JLPTRepository struct {
	DB           *sql.DB
	mu           sync.RWMutex
	vocabIndex   map[string]domain.VocabItem
	readingIndex map[string]domain.VocabItem
	indexLoaded  bool
}

func NewJLPTRepository(db *sql.DB) *JLPTRepository {
	return &JLPTRepository{
		DB:           db,
		vocabIndex:   make(map[string]domain.VocabItem),
		readingIndex: make(map[string]domain.VocabItem),
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
			item.Meanings = CleanDictionaryMeanings(item.Word, item.Meanings)
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
	readIdx := make(map[string]domain.VocabItem, 12000)
	for rows.Next() {
		var item domain.VocabItem
		var meanJSON, posJSON, otherJSON string
		if err := rows.Scan(&item.ID, &item.Word, &item.Reading, &item.Romaji, &meanJSON, &item.Level, &posJSON, &otherJSON); err == nil {
			_ = json.Unmarshal([]byte(meanJSON), &item.Meanings)
			_ = json.Unmarshal([]byte(posJSON), &item.POS)
			item.Meanings = CleanDictionaryMeanings(item.Word, item.Meanings)

			// Only index terms with >= 2 runes OR kanji characters to avoid single-kana particle noise
			runes := []rune(item.Word)
			if len(runes) >= 2 || (len(runes) == 1 && isKanjiRune(runes[0])) {
				// Prefer きょう reading for 今日 over こんにち
				if item.Word == "今日" && item.Reading == "きょう" {
					idx[item.Word] = item
				} else if _, exists := idx[item.Word]; !exists {
					idx[item.Word] = item
				}
			}

			// Index by Hiragana reading (readingIndex) so Hiragana words in transcripts (e.g. おわび -> お詫び/詫び, うわぎ -> 上着) match Kanji dictionary entries
			readRunes := []rune(item.Reading)
			if len(readRunes) >= 2 {
				if item.Word == "今日" && item.Reading == "きょう" {
					readIdx[item.Reading] = item
				} else if _, exists := readIdx[item.Reading]; !exists {
					readIdx[item.Reading] = item
				}
			}

			// Index verb/adjective stem ONLY when the trailing character is Hiragana okurigana (e.g. 短い -> 短, 諦める -> 諦め)
			// NEVER drop a Kanji character from a 2-Kanji compound noun (which previously caused 会 to match 会議 or 前 to match 前者!)
			if len(runes) >= 2 && isKanjiRune(runes[0]) && isHiraganaRune(runes[len(runes)-1]) {
				stem := string(runes[:len(runes)-1])
				stemRunes := []rune(stem)
				if len(stemRunes) >= 2 {
					if _, exists := idx[stem]; !exists {
						idx[stem] = item
					}
				}
			}
		}
	}

	// Ensure high-frequency school/anime compounds & honorific forms (お詫び, 兼ねて, 広報, 生徒会, 浮気相手, etc.) are indexed
	animeExtras := []domain.VocabItem{
		{
			ID:       "custom-owabi",
			Word:     "お詫び",
			Reading:  "おわび",
			Romaji:   "owabi",
			Meanings: []string{"permintaan maaf; maaf", "apology"},
			Level:    "N3",
			POS:      []string{"n", "vs"},
		},
		{
			ID:       "custom-wabi",
			Word:     "詫び",
			Reading:  "わび",
			Romaji:   "wabi",
			Meanings: []string{"permintaan maaf; maaf", "apology"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-kanete",
			Word:     "兼ねて",
			Reading:  "かねて",
			Romaji:   "kanete",
			Meanings: []string{"sekalian; sekaligus (merangkap)", "at the same time; also serving as"},
			Level:    "N3",
			POS:      []string{"adv"},
		},
		{
			ID:       "custom-maa",
			Word:     "まあ",
			Reading:  "まあ",
			Romaji:   "maa",
			Meanings: []string{"yah; lumayan; tenang saja", "well; I think"},
			Level:    "N3",
			POS:      []string{"adv", "int"},
		},
		{
			ID:       "custom-yappari",
			Word:     "やっぱり",
			Reading:  "やっぱり",
			Romaji:   "yappari",
			Meanings: []string{"ternyata memang; seperti dugaanku", "as expected; after all"},
			Level:    "N4",
			POS:      []string{"adv"},
		},
		{
			ID:       "custom-uwagi",
			Word:     "上着",
			Reading:  "うわぎ",
			Romaji:   "uwagi",
			Meanings: []string{"jaket; pakaian luar", "jacket; coat; outerwear"},
			Level:    "N4",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-katte",
			Word:     "勝手",
			Reading:  "かって",
			Romaji:   "katte",
			Meanings: []string{"seenaknya; tanpa izin", "one's own way; without permission"},
			Level:    "N3",
			POS:      []string{"n", "adj-na"},
		},
		{
			ID:       "custom-kouhou",
			Word:     "広報",
			Reading:  "こうほう",
			Romaji:   "kouhou",
			Meanings: []string{"humas (hubungan masyarakat)", "public relations"},
			Level:    "N3",
			POS:      []string{"n", "vs"},
		},
		{
			ID:       "custom-seitokai",
			Word:     "生徒会",
			Reading:  "せいとかい",
			Romaji:   "seitokai",
			Meanings: []string{"OSIS (dewan siswa)", "student council"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-harikaeru",
			Word:     "張り替える",
			Reading:  "はりかえる",
			Romaji:   "harikaeru",
			Meanings: []string{"mengganti tempelan; menempel ulang", "to re-paste"},
			Level:    "N3",
			POS:      []string{"v1", "vt"},
		},
		{
			ID:       "custom-oneesan",
			Word:     "お姉さん",
			Reading:  "おねえさん",
			Romaji:   "oneesan",
			Meanings: []string{"kakak perempuan", "older sister"},
			Level:    "N5",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-uwaki",
			Word:     "浮気",
			Reading:  "うわき",
			Romaji:   "uwaki",
			Meanings: []string{"selingkuh; perselingkuhan", "cheating; affair"},
			Level:    "N3",
			POS:      []string{"n", "vs"},
		},
		{
			ID:       "custom-uwakiaite",
			Word:     "浮気相手",
			Reading:  "うわきあいて",
			Romaji:   "uwaki aite",
			Meanings: []string{"pasangan selingkuh; selingkuhan", "affair partner"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-youzumi",
			Word:     "用済み",
			Reading:  "ようずみ",
			Romaji:   "youzumi",
			Meanings: []string{"sudah tidak berguna lagi; urusan selesai", "having no further use"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-konyaku",
			Word:     "婚約",
			Reading:  "こんやく",
			Romaji:   "konyaku",
			Meanings: []string{"pertunangan", "engagement"},
			Level:    "N3",
			POS:      []string{"n", "vs"},
		},
		{
			ID:       "custom-konyakusha",
			Word:     "婚約者",
			Reading:  "こんやくしゃ",
			Romaji:   "konyakusha",
			Meanings: []string{"tunangan", "fiance; fiancee"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-shomu",
			Word:     "庶務",
			Reading:  "しょむ",
			Romaji:   "shomu",
			Meanings: []string{"urusan umum (OSIS/kantor)", "general affairs"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-shoki",
			Word:     "書記",
			Reading:  "しょき",
			Romaji:   "shoki",
			Meanings: []string{"sekretaris", "secretary; clerk"},
			Level:    "N3",
			POS:      []string{"n"},
		},
		{
			ID:       "custom-kanchigai",
			Word:     "勘違い",
			Reading:  "かんちがい",
			Romaji:   "kanchigai",
			Meanings: []string{"salah paham; salah sangka", "misunderstanding"},
			Level:    "N3",
			POS:      []string{"n", "vs"},
		},
	}
	for _, ex := range animeExtras {
		idx[ex.Word] = ex
		readIdx[ex.Reading] = ex
	}
	idx["コウホー"] = animeExtras[7]
	idx["やっぱ"] = animeExtras[4]
	readIdx["やっぱ"] = animeExtras[4]

	r.vocabIndex = idx
	r.readingIndex = readIdx
	r.indexLoaded = true
	return r.vocabIndex, nil
}

func (r *JLPTRepository) GetVocabReadingIndex() (map[string]domain.VocabItem, error) {
	if _, err := r.GetVocabMatcherIndex(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readingIndex, nil
}

func isKanjiRune(ch rune) bool {
	return (ch >= 0x4E00 && ch <= 0x9FFF) || (ch >= 0x3400 && ch <= 0x4DBF)
}

func isHiraganaRune(ch rune) bool {
	return ch >= 0x3040 && ch <= 0x309F
}
