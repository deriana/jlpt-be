package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/domain"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

type Database struct {
	DB  *sql.DB
	Cfg *config.AppConfig
}

func NewDatabase(cfg *config.AppConfig) (*Database, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Database.SQLitePath), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.TTS.CacheDir, 0755); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", cfg.Database.SQLitePath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	database := &Database{DB: db, Cfg: cfg}
	if err := database.Migrate(); err != nil {
		return nil, err
	}
	if err := database.SyncOpenJLPTIfEmpty(); err != nil {
		log.Printf("[SQLite] OpenJLPT initial sync warning: %v", err)
	}
	if err := database.SeedDefaultData(); err != nil {
		log.Printf("[SQLite] Default seed warning: %v", err)
	}

	return database, nil
}

func (d *Database) Migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		jlpt_target_level TEXT NOT NULL DEFAULT 'N3',
		daily_goal_mins INTEGER NOT NULL DEFAULT 30,
		preferred_voice TEXT NOT NULL DEFAULT '3',
		offline_mode INTEGER NOT NULL DEFAULT 1,
		streak_days INTEGER NOT NULL DEFAULT 12,
		last_study_date TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS vocab (
		id TEXT PRIMARY KEY,
		word TEXT NOT NULL,
		reading TEXT NOT NULL,
		romaji TEXT NOT NULL,
		meanings TEXT NOT NULL DEFAULT '[]',
		level TEXT NOT NULL,
		pos TEXT NOT NULL DEFAULT '[]',
		jmdict_id INTEGER DEFAULT 0,
		other_forms TEXT NOT NULL DEFAULT '[]',
		other_readings TEXT NOT NULL DEFAULT '[]',
		examples TEXT NOT NULL DEFAULT '[]'
	);
	CREATE INDEX IF NOT EXISTS idx_vocab_level ON vocab(level);
	CREATE INDEX IF NOT EXISTS idx_vocab_word ON vocab(word);
	CREATE INDEX IF NOT EXISTS idx_vocab_reading ON vocab(reading);

	CREATE TABLE IF NOT EXISTS kanji (
		character TEXT PRIMARY KEY,
		level TEXT NOT NULL,
		strokes INTEGER DEFAULT 0,
		grade INTEGER DEFAULT 0,
		freq INTEGER DEFAULT 0,
		radical TEXT DEFAULT '',
		radical_number INTEGER DEFAULT 0,
		onyomi TEXT NOT NULL DEFAULT '[]',
		kunyomi TEXT NOT NULL DEFAULT '[]',
		nanori TEXT NOT NULL DEFAULT '[]',
		meanings TEXT NOT NULL DEFAULT '[]',
		words TEXT NOT NULL DEFAULT '[]',
		supplementary INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_kanji_level ON kanji(level);

	CREATE TABLE IF NOT EXISTS grammar (
		id TEXT PRIMARY KEY,
		pattern TEXT NOT NULL,
		reading TEXT DEFAULT '',
		romaji TEXT NOT NULL DEFAULT '',
		level TEXT NOT NULL,
		meaning TEXT NOT NULL,
		formation TEXT NOT NULL DEFAULT '',
		examples TEXT NOT NULL DEFAULT '[]',
		tags TEXT NOT NULL DEFAULT '[]',
		notes TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_grammar_level ON grammar(level);

	CREATE TABLE IF NOT EXISTS user_item_mastery (
		user_id INTEGER NOT NULL,
		item_type TEXT NOT NULL, -- 'vocab', 'kanji', 'grammar'
		item_id TEXT NOT NULL,
		level TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'learning', -- 'unseen', 'learning', 'mastered', 'weak'
		correct_count INTEGER NOT NULL DEFAULT 0,
		Wrong_count INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, item_type, item_id)
	);

	CREATE TABLE IF NOT EXISTS decks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		jlpt_level TEXT NOT NULL DEFAULT 'N4',
		category TEXT NOT NULL DEFAULT 'vocabulary',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS flashcards (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		deck_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		front_kanji TEXT NOT NULL,
		furigana_reading TEXT NOT NULL,
		romaji TEXT NOT NULL DEFAULT '',
		back_meaning TEXT NOT NULL,
		jlpt_level TEXT NOT NULL DEFAULT 'N4',
		pos TEXT NOT NULL DEFAULT '',
		context_sentence TEXT NOT NULL DEFAULT '',
		context_reading TEXT NOT NULL DEFAULT '',
		context_meaning TEXT NOT NULL DEFAULT '',
		media_source_id INTEGER NOT NULL DEFAULT 0,
		media_title TEXT NOT NULL DEFAULT '',
		media_type TEXT NOT NULL DEFAULT 'textbook',
		media_audio_url TEXT NOT NULL DEFAULT '',
		start_time_ms INTEGER NOT NULL DEFAULT 0,
		end_time_ms INTEGER NOT NULL DEFAULT 0,
		timestamp_label TEXT NOT NULL DEFAULT '',
		interval_days INTEGER NOT NULL DEFAULT 0,
		ease_factor REAL NOT NULL DEFAULT 2.5,
		repetitions INTEGER NOT NULL DEFAULT 0,
		lapses INTEGER NOT NULL DEFAULT 0,
		last_quality INTEGER NOT NULL DEFAULT 0,
		next_review_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (deck_id) REFERENCES decks(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_flashcards_deck ON flashcards(deck_id);
	CREATE INDEX IF NOT EXISTS idx_flashcards_next_review ON flashcards(next_review_at);

	CREATE TABLE IF NOT EXISTS review_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		card_id INTEGER NOT NULL,
		quality INTEGER NOT NULL,
		interval_days INTEGER NOT NULL,
		ease_factor REAL NOT NULL,
		duration_secs INTEGER NOT NULL DEFAULT 5,
		is_offline INTEGER NOT NULL DEFAULT 1,
		reviewed_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS mined_sentences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		media_title TEXT NOT NULL,
		media_type TEXT NOT NULL DEFAULT 'anime',
		sentence TEXT NOT NULL,
		reading TEXT NOT NULL DEFAULT '',
		translation TEXT NOT NULL DEFAULT '',
		source_type TEXT NOT NULL DEFAULT 'softsub',
		is_edited INTEGER NOT NULL DEFAULT 0,
		start_time_ms INTEGER NOT NULL DEFAULT 0,
		end_time_ms INTEGER NOT NULL DEFAULT 0,
		timestamp_label TEXT NOT NULL DEFAULT '',
		audio_url TEXT NOT NULL DEFAULT '',
		matched_vocab_json TEXT NOT NULL DEFAULT '[]',
		target_level TEXT NOT NULL DEFAULT 'N4',
		saved_to_deck_id INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS daily_activity (
		user_id INTEGER NOT NULL,
		date TEXT NOT NULL,
		cards_reviewed INTEGER NOT NULL DEFAULT 0,
		minutes_studied INTEGER NOT NULL DEFAULT 0,
		sentences_mined INTEGER NOT NULL DEFAULT 0,
		offline_sessions INTEGER NOT NULL DEFAULT 0,
		accuracy_pct INTEGER NOT NULL DEFAULT 88,
		PRIMARY KEY (user_id, date)
	);

	CREATE TABLE IF NOT EXISTS chat_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		clean_text TEXT NOT NULL,
		reading TEXT NOT NULL DEFAULT '',
		emotion TEXT NOT NULL DEFAULT 'neutral',
		mode TEXT NOT NULL DEFAULT 'sensei',
		audio_url TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := d.DB.Exec(schema)
	if err != nil {
		return err
	}
	// Non-destructive migration for existing SQLite databases
	_, _ = d.DB.Exec(`ALTER TABLE mined_sentences ADD COLUMN source_type TEXT NOT NULL DEFAULT 'softsub'`)
	_, _ = d.DB.Exec(`ALTER TABLE mined_sentences ADD COLUMN is_edited INTEGER NOT NULL DEFAULT 0`)
	return nil
}

// SyncOpenJLPTIfEmpty checks if OpenJLPT tables are populated; if empty, imports from openjlpt.sqlite or JSON.
func (d *Database) SyncOpenJLPTIfEmpty() error {
	var count int
	if err := d.DB.QueryRow("SELECT COUNT(*) FROM vocab").Scan(&count); err == nil && count > 100 {
		return nil
	}
	_, err := d.SyncFromOpenJLPTSource()
	return err
}

// SyncFromOpenJLPTSource attaches data/openjlpt/openjlpt.sqlite (from github.com/evanclan/OpenJLPT)
// and bulk imports all N5, N4, N3, N2, N1 vocab, kanji, and grammar into jlptmaster.db.
func (d *Database) SyncFromOpenJLPTSource() (*domain.OpenJLPTStats, error) {
	sourcePath := d.Cfg.Database.OpenJLPTSQLitePath
	if _, err := os.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("OpenJLPT sqlite file not found at %s: %w", sourcePath, err)
	}

	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		absSource = sourcePath
	}

	// Use ATTACH DATABASE for ultra-fast bulk import of 10,000+ OpenJLPT rows
	_, err = d.DB.Exec(fmt.Sprintf(`ATTACH DATABASE '%s' AS openjlpt_src`, absSource))
	if err != nil {
		return nil, fmt.Errorf("failed to attach openjlpt source: %w", err)
	}
	defer d.DB.Exec(`DETACH DATABASE openjlpt_src`)

	tx, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO vocab (id, word, reading, romaji, meanings, level, pos, jmdict_id, other_forms, other_readings, examples)
		SELECT id, word, reading, romaji, meanings, level, pos, COALESCE(jmdict_id, 0), other_forms, other_readings, examples
		FROM openjlpt_src.vocab;

		INSERT OR REPLACE INTO kanji (character, level, strokes, grade, freq, radical, radical_number, onyomi, kunyomi, nanori, meanings, words, supplementary)
		SELECT character, level, COALESCE(strokes, 0), COALESCE(grade, 0), COALESCE(freq, 0), COALESCE(radical, ''), COALESCE(radical_number, 0), onyomi, kunyomi, nanori, meanings, words, supplementary
		FROM openjlpt_src.kanji;

		INSERT OR REPLACE INTO grammar (id, pattern, reading, romaji, level, meaning, formation, examples, tags, notes)
		SELECT id, pattern, COALESCE(reading, ''), romaji, level, meaning, formation, examples, tags, COALESCE(notes, '')
		FROM openjlpt_src.grammar;
	`)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to bulk copy OpenJLPT tables: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return d.GetOpenJLPTStats()
}

func (d *Database) GetOpenJLPTStats() (*domain.OpenJLPTStats, error) {
	stats := &domain.OpenJLPTStats{
		VocabByLevel:   make(map[string]int),
		KanjiByLevel:   make(map[string]int),
		GrammarByLevel: make(map[string]int),
		LastSyncedAt:   time.Now().Format(time.RFC3339),
		SourceRepo:     "https://github.com/evanclan/OpenJLPT",
	}

	rows, err := d.DB.Query("SELECT level, COUNT(*) FROM vocab GROUP BY level")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var lvl string
			var cnt int
			if rows.Scan(&lvl, &cnt) == nil {
				stats.VocabByLevel[lvl] = cnt
				stats.TotalVocab += cnt
			}
		}
	}

	kRows, err := d.DB.Query("SELECT level, COUNT(*) FROM kanji GROUP BY level")
	if err == nil {
		defer kRows.Close()
		for kRows.Next() {
			var lvl string
			var cnt int
			if kRows.Scan(&lvl, &cnt) == nil {
				stats.KanjiByLevel[lvl] = cnt
				stats.TotalKanji += cnt
			}
		}
	}

	gRows, err := d.DB.Query("SELECT level, COUNT(*) FROM grammar GROUP BY level")
	if err == nil {
		defer gRows.Close()
		for gRows.Next() {
			var lvl string
			var cnt int
			if gRows.Scan(&lvl, &cnt) == nil {
				stats.GrammarByLevel[lvl] = cnt
				stats.TotalGrammar += cnt
			}
		}
	}

	return stats, nil
}

// SeedDefaultData seeds the default user, mock/starter N4 & N3 flashcard decks,
// anime/music mined sentences with timestamps, mastery progress, and initial AI Sensei welcome.
func (d *Database) SeedDefaultData() error {
	var userCount int
	if err := d.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount); err == nil && userCount > 0 {
		return nil
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("jlptmaster123"), bcrypt.DefaultCost)
	today := time.Now().Format("2006-01-02")

	res, err := d.DB.Exec(`
		INSERT INTO users (username, email, password_hash, jlpt_target_level, daily_goal_mins, preferred_voice, offline_mode, streak_days, last_study_date)
		VALUES ('deryana', 'derana765@gmail.com', ?, 'N3', 30, '3', 1, 14, ?)
	`, string(hash), today)
	if err != nil {
		return err
	}
	userID, _ := res.LastInsertId()

	// Seed 3 Core Decks: N4 Core, N3 Immersion, and Anime & Music Mined Deck
	d1, _ := d.DB.Exec(`INSERT INTO decks (user_id, name, description, jlpt_level, category) VALUES (?, 'JLPT N4 Core Vocabulary & Kanji', 'Essential N4 verbs, adjectives, and daily life kanji from OpenJLPT', 'N4', 'vocabulary')`, userID)
	deckN4ID, _ := d1.LastInsertId()

	d2, _ := d.DB.Exec(`INSERT INTO decks (user_id, name, description, jlpt_level, category) VALUES (?, 'JLPT N3 Mastery & Grammar Context', 'Intermediate N3 expressions, abstract nouns, and conversational patterns', 'N3', 'vocabulary')`, userID)
	deckN3ID, _ := d2.LastInsertId()

	d3, _ := d.DB.Exec(`INSERT INTO decks (user_id, name, description, jlpt_level, category) VALUES (?, 'Anime & J-Pop Sentence Mining Deck', 'Real-world contextual sentences mined from anime subtitles (.srt) and song lyrics (.lrc) with timestamp audio', 'Mixed', 'media-mining')`, userID)
	deckMediaID, _ := d3.LastInsertId()

	now := time.Now()
	dueTime := now.Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	futureTime := now.Add(24 * time.Hour).Format("2006-01-02 15:04:05")

	// Seed N4 Flashcards
	n4Cards := []struct {
		kanji, reading, romaji, meaning, pos, sent, sentRead, sentEn string
		due                                                          bool
		reps, lapses, interval                                       int
	}{
		{"約束", "やくそく", "yakusoku", "promise, appointment, arrangement", "n, vs", "明日の約束を忘れないでくださいね。", "あしたのやくそくをわすれないでくださいね。", "Please don't forget tomorrow's promise.", true, 3, 0, 4},
		{"準備", "じゅんび", "junbi", "preparation, arrangements, getting ready", "n, vs", "旅行の準備はもう終わりましたか。", "りょこうのじゅんびはもうおわりましたか。", "Have you finished preparing for the trip already?", true, 2, 1, 2},
		{"経験", "けいけん", "keiken", "experience", "n, vs", "日本で働くのはいい経験になります。", "にほんでたらくのはいいけいけんになります。", "Working in Japan will be a good experience.", true, 4, 0, 7},
		{"相談", "そうだん", "soudan", "consultation, discussion", "n, vs", "先生に将来のことを相談しました。", "せんせいにしょうらいのことをそうだんしました。", "I consulted with my teacher about my future.", true, 1, 2, 1},
		{"丁寧", "ていねい", "teinei", "polite, courteous, careful", "adj-na", "彼女はいつも丁寧な日本語を話します。", "かのじょはいつもていねいなにほんごをはなします。", "She always speaks polite Japanese.", false, 5, 0, 14},
		{"習慣", "しゅうかん", "shuukan", "custom, habit, practice", "n", "毎朝３０分日本語を勉強する習慣があります。", "まいあささんじゅっぷんにほんごをべんきょうするしゅうかんがあります。", "I have a habit of studying Japanese for 30 minutes every morning.", true, 2, 0, 3},
		{"複雑", "ふくざつ", "fukuzatsu", "complex, complicated", "adj-na", "このアニメのストーリーは少し複雑です。", "このアニメのストーリーはすこしふくざつです。", "The story of this anime is a little complicated.", true, 1, 3, 1},
		{"特急", "とっきゅう", "tokkyuu", "limited express (train)", "n", "京都まで特急電車で行きましょう。", "きょうとまでとっきゅうでんしゃでいきましょう。", "Let's go to Kyoto by limited express train.", false, 4, 0, 10},
	}

	for _, c := range n4Cards {
		nextRev := futureTime
		if c.due {
			nextRev = dueTime
		}
		_, _ = d.DB.Exec(`
			INSERT INTO flashcards (deck_id, user_id, front_kanji, furigana_reading, romaji, back_meaning, jlpt_level, pos, context_sentence, context_reading, context_meaning, media_type, interval_days, ease_factor, repetitions, lapses, next_review_at)
			VALUES (?, ?, ?, ?, ?, ?, 'N4', ?, ?, ?, ?, 'textbook', ?, 2.5, ?, ?, ?)
		`, deckN4ID, userID, c.kanji, c.reading, c.romaji, c.meaning, c.pos, c.sent, c.sentRead, c.sentEn, c.interval, c.reps, c.lapses, nextRev)
	}

	// Seed N3 Flashcards
	n3Cards := []struct {
		kanji, reading, romaji, meaning, pos, sent, sentRead, sentEn string
		due                                                          bool
		reps, lapses, interval                                       int
	}{
		{"諦める", "あきらめる", "akirameru", "to give up, to abandon", "v1, vt", "最後まで絶対に諦めないで！", "さいごまでぜったいにあきらめないで！", "Never give up until the very end!", true, 3, 0, 5},
		{"感動", "かんどう", "kandou", "being deeply moved emotionally, excitement", "n, vs", "あの映画のラストシーンには本当に感動した。", "あのえいがのラストシーンにはほんとうにかんどうした。", "I was truly moved by the final scene of that movie.", true, 2, 0, 3},
		{"雰囲気", "ふんいき", "fun'iki", "atmosphere, mood, ambiance", "n", "このカフェはとても落ち着いた雰囲気ですね。", "このカフェはとてもおちついたふんいきですね。", "This cafe has a very calm atmosphere, doesn't it?", true, 1, 3, 1},
		{"偶然", "ぐうぜん", "guuzen", "by chance, unexpectedly, coincidence", "n, adv, adj-na", "駅で偶然昔の友達に会った。", "えきでぐうぜんむかしのともだちにあった。", "I ran into an old friend by chance at the station.", true, 2, 1, 2},
		{"影響", "えいきょう", "eikyou", "influence, effect", "n, vs", "日本のアニメは世界中に大きな影響を与えている。", "にほんのアニメはせかいじゅうにおおきなえいきょうをあたえている。", "Japanese anime has a huge influence all over the world.", false, 5, 0, 12},
		{"懐かしい", "なつかしい", "natsukashii", "dear, desired, missed, nostalgic", "adj-i", "この曲を聴くと高校時代が懐かしい。", "このきょくをきくとこうこうじだいがなつかしい。", "Listening to this song makes me nostalgic for my high school days.", true, 3, 0, 4},
	}

	for _, c := range n3Cards {
		nextRev := futureTime
		if c.due {
			nextRev = dueTime
		}
		_, _ = d.DB.Exec(`
			INSERT INTO flashcards (deck_id, user_id, front_kanji, furigana_reading, romaji, back_meaning, jlpt_level, pos, context_sentence, context_reading, context_meaning, media_type, interval_days, ease_factor, repetitions, lapses, next_review_at)
			VALUES (?, ?, ?, ?, ?, ?, 'N3', ?, ?, ?, ?, 'textbook', ?, 2.5, ?, ?, ?)
		`, deckN3ID, userID, c.kanji, c.reading, c.romaji, c.meaning, c.pos, c.sent, c.sentRead, c.sentEn, c.interval, c.reps, c.lapses, nextRev)
	}

	// Seed Mined Anime & Music Sentences + Corresponding Media Flashcards with Timestamps!
	minedSamples := []struct {
		title, mType, sentence, reading, translation, tsLabel string
		startMs, endMs                                        int64
		targetWord, targetReading, targetRomaji, targetMean   string
		level                                                 string
		tokens                                                []domain.MatchedVocabToken
	}{
		{
			title: "Sousou no Frieren - Ep 01", mType: "anime",
			sentence: "人間の寿命は短いってわかっていたのに、どうして私はもっと知ろうとしなかったんだろう。",
			reading:  "にんげんのじゅみょうはみじかいってわかっていたのに、どうしてわたしはもっとしろうとしなかったんだろう。",
			translation: "Even though I knew human lifespans are short, why didn't I try to get to know him better?",
			tsLabel: "00:14.20 - 00:19.80", startMs: 14200, endMs: 19800,
			targetWord: "短い", targetReading: "みじかい", targetRomaji: "mijikai", targetMean: "short, brief", level: "N4",
			tokens: []domain.MatchedVocabToken{
				{VocabID: "n3-ningen", Surface: "人間", Word: "人間", Reading: "にんげん", Romaji: "ningen", Meanings: []string{"human being", "person"}, Level: "N3"},
				{VocabID: "n4-mijikai", Surface: "短い", Word: "短い", Reading: "みじかい", Romaji: "mijikai", Meanings: []string{"short", "brief"}, Level: "N4"},
				{VocabID: "n4-motto", Surface: "もっと", Word: "もっと", Reading: "もっと", Romaji: "motto", Meanings: []string{"more", "even more"}, Level: "N4"},
			},
		},
		{
			title: "Bocchi the Rock! - Ep 08", mType: "anime",
			sentence: "諦めなければ、きっと私たちの音楽がみんなに届くはずだよ！",
			reading:  "あきらめなければ、きっとわたしたちのおんがくがみんなにとどくはずだよ！",
			translation: "If we don't give up, our music will surely reach everyone!",
			tsLabel: "01:42.50 - 01:47.10", startMs: 102500, endMs: 107100,
			targetWord: "届く", targetReading: "とどく", targetRomaji: "todoku", targetMean: "to reach, to arrive, to be delivered", level: "N4",
			tokens: []domain.MatchedVocabToken{
				{VocabID: "n3-akirameru", Surface: "諦め", Word: "諦める", Reading: "あきらめる", Romaji: "akirameru", Meanings: []string{"to give up", "to abandon"}, Level: "N3"},
				{VocabID: "n4-kitto", Surface: "きっと", Word: "きっと", Reading: "きっと", Romaji: "kitto", Meanings: []string{"surely", "undoubtedly"}, Level: "N4"},
				{VocabID: "n4-todoku", Surface: "届く", Word: "届く", Reading: "とどく", Romaji: "todoku", Meanings: []string{"to reach", "to arrive"}, Level: "N4"},
			},
		},
		{
			title: "YOASOBI - Yoru ni Kakeru (Lyrics)", mType: "music",
			sentence: "沈むように溶けてゆくように、二人だけの空が広がる夜に。",
			reading:  "しずむようにとけてゆくように、ふたりだけのそらがひろがるよるに。",
			translation: "Like sinking, like melting away, into the night where a sky just for the two of us spreads out.",
			tsLabel: "00:08.50 - 00:15.20", startMs: 8500, endMs: 15200,
			targetWord: "広がる", targetReading: "ひろがる", targetRomaji: "hirogaru", targetMean: "to spread out, to extend, to stretch", level: "N3",
			tokens: []domain.MatchedVocabToken{
				{VocabID: "n3-shizumu", Surface: "沈む", Word: "沈む", Reading: "しずむ", Romaji: "shizumu", Meanings: []string{"to sink", "to feel depressed"}, Level: "N3"},
				{VocabID: "n3-tokeru", Surface: "溶けて", Word: "溶ける", Reading: "とける", Romaji: "tokeru", Meanings: []string{"to melt", "to dissolve"}, Level: "N3"},
				{VocabID: "n3-hirogaru", Surface: "広がる", Word: "広がる", Reading: "ひろがる", Romaji: "hirogaru", Meanings: []string{"to spread out", "to extend"}, Level: "N3"},
			},
		},
		{
			title: "Kimi no Na wa (Your Name) - Subtitle", mType: "anime",
			sentence: "大事なのは忘れないこと。君の名前を絶対に忘れたくない！",
			reading:  "だいじなのはわすれないこと。きみのなまえをぜったいにわすれたくない！",
			translation: "What's important is not forgetting. I never want to forget your name!",
			tsLabel: "01:18:22.00 - 01:18:27.40", startMs: 4702000, endMs: 4707400,
			targetWord: "絶対", targetReading: "ぜったい", targetRomaji: "zettai", targetMean: "absolutely, unconditionally", level: "N3",
			tokens: []domain.MatchedVocabToken{
				{VocabID: "n4-daiji", Surface: "大事", Word: "大事", Reading: "だいじ", Romaji: "daiji", Meanings: []string{"important", "valuable"}, Level: "N4"},
				{VocabID: "n3-zettai", Surface: "絶対", Word: "絶対", Reading: "ぜったい", Romaji: "zettai", Meanings: []string{"absolutely", "definitely"}, Level: "N3"},
			},
		},
	}

	for _, ms := range minedSamples {
		tokBytes, _ := json.Marshal(ms.tokens)
		resMined, _ := d.DB.Exec(`
			INSERT INTO mined_sentences (user_id, media_title, media_type, sentence, reading, translation, start_time_ms, end_time_ms, timestamp_label, matched_vocab_json, target_level, saved_to_deck_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, userID, ms.title, ms.mType, ms.sentence, ms.reading, ms.translation, ms.startMs, ms.endMs, ms.tsLabel, string(tokBytes), ms.level, deckMediaID)
		minedID, _ := resMined.LastInsertId()

		_, _ = d.DB.Exec(`
			INSERT INTO flashcards (deck_id, user_id, front_kanji, furigana_reading, romaji, back_meaning, jlpt_level, pos, context_sentence, context_reading, context_meaning, media_source_id, media_title, media_type, start_time_ms, end_time_ms, timestamp_label, interval_days, ease_factor, repetitions, lapses, next_review_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'mined', ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 2.5, 1, 0, ?)
		`, deckMediaID, userID, ms.targetWord, ms.targetReading, ms.targetRomaji, ms.targetMean, ms.level, ms.sentence, ms.reading, ms.translation, minedID, ms.title, ms.mType, ms.startMs, ms.endMs, ms.tsLabel, dueTime)
	}

	// Seed realistic user_item_mastery rows from actual OpenJLPT N4 & N3 items so Dashboard shows realistic coverage (~78% N4, ~62% N3)
	_, _ = d.DB.Exec(`
		INSERT OR IGNORE INTO user_item_mastery (user_id, item_type, item_id, level, status, correct_count, wrong_count)
		SELECT ?, 'vocab', id, 'N4', 'mastered', 5, 0 FROM vocab WHERE level = 'N4' LIMIT 492;

		INSERT OR IGNORE INTO user_item_mastery (user_id, item_type, item_id, level, status, correct_count, wrong_count)
		SELECT ?, 'vocab', id, 'N3', 'mastered', 4, 0 FROM vocab WHERE level = 'N3' LIMIT 1028;

		INSERT OR IGNORE INTO user_item_mastery (user_id, item_type, item_id, level, status, correct_count, wrong_count)
		SELECT ?, 'kanji', character, 'N4', 'mastered', 6, 0 FROM kanji WHERE level = 'N4' LIMIT 138;

		INSERT OR IGNORE INTO user_item_mastery (user_id, item_type, item_id, level, status, correct_count, wrong_count)
		SELECT ?, 'kanji', character, 'N3', 'mastered', 4, 1 FROM kanji WHERE level = 'N3' LIMIT 248;

		INSERT OR IGNORE INTO user_item_mastery (user_id, item_type, item_id, level, status, correct_count, wrong_count)
		SELECT ?, 'grammar', id, 'N4', 'mastered', 5, 0 FROM grammar WHERE level = 'N4' LIMIT 81;

		INSERT OR IGNORE INTO user_item_mastery (user_id, item_type, item_id, level, status, correct_count, wrong_count)
		SELECT ?, 'grammar', id, 'N3', 'mastered', 3, 1 FROM grammar WHERE level = 'N3' LIMIT 66;
	`, userID, userID, userID, userID, userID, userID)

	// Seed 7 days of study activity
	activityData := []struct {
		daysAgo, cards, mins, mined, offline, acc int
	}{
		{6, 42, 32, 4, 1, 86},
		{5, 55, 38, 6, 2, 89},
		{4, 38, 30, 3, 1, 85},
		{3, 64, 45, 8, 2, 91},
		{2, 50, 35, 5, 1, 90},
		{1, 72, 48, 9, 2, 93},
		{0, 48, 34, 4, 1, 92},
	}
	for _, act := range activityData {
		dStr := now.AddDate(0, 0, -act.daysAgo).Format("2006-01-02")
		_, _ = d.DB.Exec(`
			INSERT OR REPLACE INTO daily_activity (user_id, date, cards_reviewed, minutes_studied, sentences_mined, offline_sessions, accuracy_pct)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, userID, dStr, act.cards, act.mins, act.mined, act.offline, act.acc)
	}

	// Seed initial AI Sensei welcome message
	_, _ = d.DB.Exec(`
		INSERT INTO chat_messages (user_id, role, content, clean_text, reading, emotion, mode)
		VALUES (?, 'assistant', '[joy] おかえりなさい！ (Okaerinasai!) Welcome back to JLPTMaster! I am Ryza-sensei, your offline Live2D Japanese tutor. Ask me any JLPT N4/N3 grammar question, paste an anime sentence to break down, or click any VOICEVOX voice to practice listening!', 'おかえりなさい！ (Okaerinasai!) Welcome back to JLPTMaster! I am Ryza-sensei, your offline Live2D Japanese tutor. Ask me any JLPT N4/N3 grammar question, paste an anime sentence to break down, or click any VOICEVOX voice to practice listening!', 'おかえりなさい！', 'joy', 'sensei')
	`, userID)

	return nil
}
