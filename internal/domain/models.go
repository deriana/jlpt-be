package domain

import "time"

// ==========================================
// 1. User & Profile Domain (Functional Req #1)
// ==========================================

type User struct {
	ID              int64     `json:"id"`
	Username        string    `json:"username"`
	Email           string    `json:"email"`
	PasswordHash    string    `json:"-"`
	JLPTTargetLevel string    `json:"jlpt_target_level"` // "N4" or "N3"
	DailyGoalMins   int       `json:"daily_goal_mins"`
	PreferredVoice  string    `json:"preferred_voice"`
	OfflineMode     bool      `json:"offline_mode"`
	StreakDays      int       `json:"streak_days"`
	LastStudyDate   string    `json:"last_study_date"`
	CreatedAt       time.Time `json:"created_at"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// ==========================================
// 2. OpenJLPT Dataset Domain (Functional Req #5)
// ==========================================

type ExampleSentence struct {
	Japanese string `json:"ja"`
	Reading  string `json:"reading,omitempty"`
	Romaji   string `json:"romaji,omitempty"`
	English  string `json:"en"`
}

type VocabItem struct {
	ID            string            `json:"id"`
	Word          string            `json:"word"`
	Reading       string            `json:"reading"`
	Romaji        string            `json:"romaji"`
	Meanings      []string          `json:"meanings"`
	Level         string            `json:"level"` // N5, N4, N3, N2, N1
	POS           []string          `json:"pos"`
	JMDictID      int64             `json:"jmdict_id,omitempty"`
	Examples      []ExampleSentence `json:"examples"`
	MasteryStatus string            `json:"mastery_status"` // "unseen", "learning", "mastered", "weak"
	AccuracyRate  float64           `json:"accuracy_rate"`
}

type KanjiItem struct {
	Character     string   `json:"character"`
	Level         string   `json:"level"`
	Strokes       int      `json:"strokes"`
	Grade         int      `json:"grade"`
	Freq          int      `json:"freq"`
	Radical       string   `json:"radical"`
	Onyomi        []string `json:"onyomi"`
	Kunyomi       []string `json:"kunyomi"`
	Meanings      []string `json:"meanings"`
	Words         []string `json:"words"`
	MasteryStatus string   `json:"mastery_status"`
}

type GrammarItem struct {
	ID            string            `json:"id"`
	Pattern       string            `json:"pattern"`
	Reading       string            `json:"reading"`
	Romaji        string            `json:"romaji"`
	Level         string            `json:"level"`
	Meaning       string            `json:"meaning"`
	Formation     string            `json:"formation"`
	Examples      []ExampleSentence `json:"examples"`
	Tags          []string          `json:"tags"`
	Notes         string            `json:"notes"`
	MasteryStatus string            `json:"mastery_status"`
}

type OpenJLPTStats struct {
	VocabByLevel   map[string]int `json:"vocab_by_level"`
	KanjiByLevel   map[string]int `json:"kanji_by_level"`
	GrammarByLevel map[string]int `json:"grammar_by_level"`
	TotalVocab     int            `json:"total_vocab"`
	TotalKanji     int            `json:"total_kanji"`
	TotalGrammar   int            `json:"total_grammar"`
	LastSyncedAt   string         `json:"last_synced_at"`
	SourceRepo     string         `json:"source_repo"`
}

// ==========================================
// 3. Interactive SRS Flashcards Domain (Functional Req #2)
// ==========================================

type Deck struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	JLPTLevel   string    `json:"jlpt_level"` // "N4", "N3", "Mixed", "Media"
	Category    string    `json:"category"`   // "vocabulary", "kanji", "grammar", "media-mining"
	TotalCards  int       `json:"total_cards"`
	DueCards    int       `json:"due_cards"`
	Mastered    int       `json:"mastered_cards"`
	CreatedAt   time.Time `json:"created_at"`
}

type Flashcard struct {
	ID              int64     `json:"id"`
	DeckID          int64     `json:"deck_id"`
	UserID          int64     `json:"user_id"`
	FrontKanji      string    `json:"front_kanji"`
	FuriganaReading string    `json:"furigana_reading"`
	Romaji          string    `json:"romaji"`
	BackMeaning     string    `json:"back_meaning"`
	JLPTLevel       string    `json:"jlpt_level"`
	POS             string    `json:"pos"`
	ContextSentence string    `json:"context_sentence"`
	ContextReading  string    `json:"context_reading"`
	ContextMeaning  string    `json:"context_meaning"`
	MediaSourceID   int64     `json:"media_source_id,omitempty"`
	MediaTitle      string    `json:"media_title,omitempty"`
	MediaType       string    `json:"media_type,omitempty"` // "anime", "music", "textbook"
	MediaAudioURL   string    `json:"media_audio_url,omitempty"`
	StartTimeMs     int64     `json:"start_time_ms"`
	EndTimeMs       int64     `json:"end_time_ms"`
	TimestampLabel  string    `json:"timestamp_label"` // e.g. "01:24.50 - 01:28.20"
	IntervalDays    int       `json:"interval_days"`
	EaseFactor      float64   `json:"ease_factor"`
	Repetitions     int       `json:"repetitions"`
	Lapses          int       `json:"lapses"`
	LastQuality     int       `json:"last_quality"`
	NextReviewAt    time.Time `json:"next_review_at"`
	IsDue           bool      `json:"is_due"`
}

type ReviewSubmission struct {
	CardID       int64 `json:"card_id" binding:"required"`
	Quality      int   `json:"quality" binding:"required"` // 1: Again, 2: Hard, 3: Good, 4: Easy
	DurationSecs int   `json:"duration_secs"`
	IsOffline    bool  `json:"is_offline"`
}

// ==========================================
// 4. Anime & Music Sentence Mining + Matcher (Functional Req #3, #4, #5)
// ==========================================

type MatchedVocabToken struct {
	VocabID  string   `json:"vocab_id"`
	Surface  string   `json:"surface"`
	Word     string   `json:"word"`
	Reading  string   `json:"reading"`
	Romaji   string   `json:"romaji"`
	Meanings []string `json:"meanings"`
	Level    string   `json:"level"`
	POS      []string `json:"pos"`
	StartIdx int      `json:"start_idx"`
	EndIdx   int      `json:"end_idx"`
}

type MediaCue struct {
	Index          int                 `json:"index"`
	StartTimeMs    int64               `json:"start_time_ms"`
	EndTimeMs      int64               `json:"end_time_ms"`
	TimestampLabel string              `json:"timestamp_label"`
	Sentence       string              `json:"sentence"`
	Translation    string              `json:"translation,omitempty"`
	MatchedVocab   []MatchedVocabToken `json:"matched_vocab"`
	N4N3Count      int                 `json:"n4_n3_count"`
	CoverageRate   float64             `json:"coverage_rate"`
}

type MediaPreset struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Episode     string     `json:"episode"`
	MediaType   string     `json:"media_type"` // "anime" or "music"
	Format      string     `json:"format"`     // "srt" or "lrc"
	CoverAccent string     `json:"cover_accent"`
	RawContent  string     `json:"raw_content"`
	Cues        []MediaCue `json:"cues"`
}

type MinedSentence struct {
	ID             int64               `json:"id"`
	UserID         int64               `json:"user_id"`
	MediaTitle     string              `json:"media_title"`
	MediaType      string              `json:"media_type"` // "anime" or "music"
	Sentence       string              `json:"sentence"`
	Reading        string              `json:"reading"`
	Translation    string              `json:"translation"`
	StartTimeMs    int64               `json:"start_time_ms"`
	EndTimeMs      int64               `json:"end_time_ms"`
	TimestampLabel string              `json:"timestamp_label"`
	AudioURL       string              `json:"audio_url"`
	MatchedVocab   []MatchedVocabToken `json:"matched_vocab"`
	TargetLevel    string              `json:"target_level"`
	SavedToDeckID  int64               `json:"saved_to_deck_id"`
	CreatedAt      time.Time           `json:"created_at"`
}

// ==========================================
// 5. Dashboard & Progress Tracker (Functional Req #6)
// ==========================================

type WeakSpotItem struct {
	Term         string  `json:"term"`
	Reading      string  `json:"reading"`
	Meaning      string  `json:"meaning"`
	Level        string  `json:"level"`
	ItemType     string  `json:"item_type"` // "vocab", "kanji", "grammar"
	Lapses       int     `json:"lapses"`
	AccuracyRate float64 `json:"accuracy_rate"`
}

type DailyActivityPoint struct {
	Date            string `json:"date"`
	CardsReviewed   int    `json:"cards_reviewed"`
	MinutesStudied  int    `json:"minutes_studied"`
	SentencesMined  int    `json:"sentences_mined"`
	OfflineSessions int    `json:"offline_sessions"`
	AccuracyPct     int    `json:"accuracy_pct"`
}

type LevelMasteryMetric struct {
	Level           string  `json:"level"`
	VocabTotal      int     `json:"vocab_total"`
	VocabLearned    int     `json:"vocab_learned"`
	VocabCoverage   float64 `json:"vocab_coverage"`
	KanjiTotal      int     `json:"kanji_total"`
	KanjiLearned    int     `json:"kanji_learned"`
	KanjiCoverage   float64 `json:"kanji_coverage"`
	GrammarTotal    int     `json:"grammar_total"`
	GrammarLearned  int     `json:"grammar_learned"`
	GrammarCoverage float64 `json:"grammar_coverage"`
}

type DashboardSummary struct {
	StreakDays            int                  `json:"streak_days"`
	TodayStudyMinutes     int                  `json:"today_study_minutes"`
	DailyGoalMinutes      int                  `json:"daily_goal_minutes"`
	DueCardsCount         int                  `json:"due_cards_count"`
	TotalReviewsCount     int                  `json:"total_reviews_count"`
	MinedSentencesCount   int                  `json:"mined_sentences_count"`
	MediaDeckRatioPct     float64              `json:"media_deck_ratio_pct"`
	OfflineSessionRatio   float64              `json:"offline_session_ratio"`
	N4N3CombinedCoverage  float64              `json:"n4_n3_combined_coverage"`
	TargetCoverageGoalPct float64              `json:"target_coverage_goal_pct"`
	SixMonthProjectionPct float64              `json:"six_month_projection_pct"`
	N4Mastery             LevelMasteryMetric   `json:"n4_mastery"`
	N3Mastery             LevelMasteryMetric   `json:"n3_mastery"`
	WeakSpots             []WeakSpotItem       `json:"weak_spots"`
	WeeklyActivity        []DailyActivityPoint `json:"weekly_activity"`
	SQLiteQueryLatencyMs  float64              `json:"sqlite_query_latency_ms"`
}

// ==========================================
// 6. Windows VOICEVOX TTS & Lip-Sync Domain
// ==========================================

type VoicevoxSpeaker struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character"`
	Style     string `json:"style"`
	Lang      string `json:"lang"`
}

type TTSStatus struct {
	Provider        string            `json:"provider"`
	ConfiguredURL   string            `json:"configured_url"`
	ResolvedURL     string            `json:"resolved_url"`
	WindowsHostIP   string            `json:"windows_host_ip"`
	EngineOnline    bool              `json:"engine_online"`
	ActiveSpeakerID string            `json:"active_speaker_id"`
	FallbackMode    string            `json:"fallback_mode"`
	Voices          []VoicevoxSpeaker `json:"voices"`
}

type TTSSpeakRequest struct {
	Text      string  `json:"text" binding:"required"`
	SpeakerID string  `json:"speaker_id"`
	Emotion   string  `json:"emotion"`
	Speed     float64 `json:"speed"`
}

type TTSSpeakResponse struct {
	AudioURL   string    `json:"audio_url"`
	SpeakerID  string    `json:"speaker_id"`
	EngineUsed string    `json:"engine_used"` // "voicevox-windows" or "offline-acoustic-synth"
	Volumes    []float64 `json:"volumes"`     // 20ms RMS volume chunks for Live2D lip-sync
	ChunkMs    int       `json:"chunk_ms"`
	DurationMs int       `json:"duration_ms"`
}

// ==========================================
// 7. Local AI Chatbot & Live2D Companion Domain
// ==========================================

type ChatMessage struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	CleanText string    `json:"clean_text"`
	Reading   string    `json:"reading,omitempty"`
	Emotion   string    `json:"emotion"` // "joy", "blush", "surprise", "sadness", "anger", "neutral"
	Mode      string    `json:"mode"`    // "sensei", "roleplay", "grammar", "quiz"
	AudioURL  string    `json:"audio_url,omitempty"`
	Volumes   []float64 `json:"volumes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ChatRequest struct {
	Message   string `json:"message" binding:"required"`
	Mode      string `json:"mode"` // "sensei", "roleplay", "grammar", "quiz"
	SpeakerID string `json:"speaker_id"`
	WithTTS   bool   `json:"with_tts"`
}

type ChatResponse struct {
	Reply         ChatMessage `json:"reply"`
	LLMProvider   string      `json:"llm_provider"` // "ollama-local" or "offline-openjlpt-sensei"
	ResolvedLLMIP string      `json:"resolved_llm_ip"`
}
