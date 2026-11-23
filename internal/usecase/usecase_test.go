package usecase

import (
	"os"
	"path/filepath"
	"testing"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/repository/sqlite"
)

func setupTestDB(t *testing.T) (*sqlite.Database, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	openjlptPath, _ := filepath.Abs("../../data/openjlpt/openjlpt.sqlite")

	cfg := &config.AppConfig{
		Database: config.DatabaseConfig{
			SQLitePath:         filepath.Join(tmpDir, "test_jlptmaster.db"),
			OpenJLPTSQLitePath: openjlptPath,
		},
		TTS: config.TTSConfig{
			Provider:       "voicevox",
			VoicevoxURL:    "http://127.0.0.1:50021",
			DefaultSpeaker: "3",
			SpeedScale:     1.0,
			CacheDir:       filepath.Join(tmpDir, "audio_cache"),
		},
	}

	db, err := sqlite.NewDatabase(cfg)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	return db, func() {
		db.DB.Close()
		_ = os.RemoveAll(tmpDir)
	}
}

func TestOpenJLPTMatcherAndMediaParser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	jlptRepo := sqlite.NewJLPTRepository(db.DB)
	srsRepo := sqlite.NewSRSRepository(db.DB)
	miningRepo := sqlite.NewMiningRepository(db.DB)
	miningUC := NewMiningUsecase(miningRepo, srsRepo, jlptRepo)

	// 1. Test Automated OpenJLPT Vocabulary Matcher
	sentence := "明日の約束を忘れないうちに旅行の準備をしよう。"
	tokens, n4n3Count, coverage := miningUC.MatchSentenceVocab(sentence)
	if len(tokens) == 0 {
		t.Fatalf("expected matched OpenJLPT tokens for sentence, got 0")
	}
	if n4n3Count == 0 {
		t.Fatalf("expected N4/N3 vocabulary match count > 0, got 0")
	}
	if coverage <= 0 {
		t.Fatalf("expected coverage rate > 0, got %f", coverage)
	}

	// 2. Test Anime .SRT Subtitle Parsing with Timestamps
	srtSample := `1
00:01:14,200 --> 00:01:19,800
人間の寿命は短いってわかっていたのに。
Even though I knew human lifespans are short.`

	cues := miningUC.ParseMediaText(srtSample, "srt")
	if len(cues) != 1 {
		t.Fatalf("expected 1 parsed SRT cue, got %d", len(cues))
	}
	if cues[0].StartTimeMs != 74200 || cues[0].EndTimeMs != 79800 {
		t.Fatalf("unexpected SRT timestamps: start=%d end=%d", cues[0].StartTimeMs, cues[0].EndTimeMs)
	}

	// 3. Test Song .LRC Lyric Parsing with Timestamps
	lrcSample := `[00:08.50]沈むように溶けてゆくように、二人だけの空が広がる夜に。`
	lrcCues := miningUC.ParseMediaText(lrcSample, "lrc")
	if len(lrcCues) != 1 || lrcCues[0].StartTimeMs != 8500 {
		t.Fatalf("unexpected LRC cue parse result: %+v", lrcCues)
	}
}

func TestSM2SpacedRepetitionAndVoicevoxOfflineSynth(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	jlptRepo := sqlite.NewJLPTRepository(db.DB)
	srsRepo := sqlite.NewSRSRepository(db.DB)
	srsUC := NewSRSUsecase(srsRepo, jlptRepo)

	cards, err := srsUC.ListCards(1, 0, false)
	if err != nil || len(cards) == 0 {
		t.Fatalf("expected seeded starter flashcards, got err=%v len=%d", err, len(cards))
	}

	card := cards[0]
	updated, err := srsUC.SubmitReview(1, domain.ReviewSubmission{
		CardID:       card.ID,
		Quality:      4, // Easy
		DurationSecs: 5,
		IsOffline:    true,
	})
	if err != nil {
		t.Fatalf("SubmitReview failed: %v", err)
	}
	if updated.IntervalDays <= card.IntervalDays {
		t.Fatalf("expected SM-2 interval to increase on quality=4, was %d now %d", card.IntervalDays, updated.IntervalDays)
	}

	// Test VOICEVOX / Offline Japanese Acoustic WAV + 20ms RMS Lip-Sync
	ttsUC := NewTTSUsecase(db.Cfg)
	ttsRes, err := ttsUC.Synthesize(domain.TTSSpeakRequest{
		Text:      "こんにちは！今日も日本語を勉強しましょう！",
		SpeakerID: "3",
		Emotion:   "joy",
	})
	if err != nil {
		t.Fatalf("TTS Synthesize failed: %v", err)
	}
	if len(ttsRes.Volumes) == 0 {
		t.Fatalf("expected non-empty 20ms RMS volume chunks for Live2D lip-sync")
	}
}
