package http

import (
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/usecase"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Cfg         *config.AppConfig
	AuthUC      *usecase.AuthUsecase
	JLPTUC      *usecase.JLPTUsecase
	SRSUC       *usecase.SRSUsecase
	MiningUC    *usecase.MiningUsecase
	ProgressUC  *usecase.ProgressUsecase
	TTSUC       *usecase.TTSUsecase
	TutorUC     *usecase.TutorUsecase
	TranslateUC *usecase.TranslateUsecase
}

func NewHandler(
	cfg *config.AppConfig,
	authUC *usecase.AuthUsecase,
	jlptUC *usecase.JLPTUsecase,
	srsUC *usecase.SRSUsecase,
	miningUC *usecase.MiningUsecase,
	progressUC *usecase.ProgressUsecase,
	ttsUC *usecase.TTSUsecase,
	tutorUC *usecase.TutorUsecase,
	translateUC *usecase.TranslateUsecase,
) *Handler {
	return &Handler{
		Cfg:         cfg,
		AuthUC:      authUC,
		JLPTUC:      jlptUC,
		SRSUC:       srsUC,
		MiningUC:    miningUC,
		ProgressUC:  progressUC,
		TTSUC:       ttsUC,
		TutorUC:     tutorUC,
		TranslateUC: translateUC,
	}
}

// ==========================================
// 1. Health & System Settings Handlers
// ==========================================

func (h *Handler) GetHealth(c *gin.Context) {
	ttsStatus := h.TTSUC.GetStatus()
	resolvedLLM, llmOnline, hostIP := config.ResolveServiceURL(h.Cfg.LLM.BaseURL, "models", 700*time.Millisecond)
	stats, _ := h.JLPTUC.GetStats()

	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"offline_ready":   true,
		"windows_host_ip": hostIP,
		"tts":             ttsStatus,
		"llm": gin.H{
			"provider":       h.Cfg.LLM.Provider,
			"configured_url": h.Cfg.LLM.BaseURL,
			"resolved_url":   resolvedLLM,
			"model":          h.Cfg.LLM.Model,
			"sensei_name":    h.Cfg.LLM.SenseiName,
			"archetype":      h.Cfg.LLM.Archetype,
			"online":         llmOnline,
			"fallback":       "offline-openjlpt-sensei",
		},
		"live2d":   h.Cfg.Live2D,
		"openjlpt": stats,
	})
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	var req struct {
		VoicevoxURL     string  `json:"voicevox_url"`
		ActiveSpeakerID string  `json:"active_speaker_id"`
		SpeedScale      float64 `json:"speed_scale"`
		LLMBaseURL      string  `json:"llm_base_url"`
		LLMModel        string  `json:"llm_model"`
		SenseiName      string  `json:"sensei_name"`
		Archetype       string  `json:"archetype"`
		ActiveLive2D    string  `json:"active_live2d"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_ = h.Cfg.Update(func(cfg *config.AppConfig) {
		if req.VoicevoxURL != "" {
			cfg.TTS.VoicevoxURL = req.VoicevoxURL
		}
		if req.ActiveSpeakerID != "" {
			cfg.TTS.DefaultSpeaker = req.ActiveSpeakerID
		}
		if req.SpeedScale >= 0.5 && req.SpeedScale <= 2.0 {
			cfg.TTS.SpeedScale = req.SpeedScale
		}
		if req.LLMBaseURL != "" {
			cfg.LLM.BaseURL = req.LLMBaseURL
		}
		if req.LLMModel != "" {
			cfg.LLM.Model = req.LLMModel
		}
		if req.SenseiName != "" {
			cfg.LLM.SenseiName = req.SenseiName
		}
		if req.Archetype != "" {
			cfg.LLM.Archetype = req.Archetype
		}
		if req.ActiveLive2D != "" {
			cfg.Live2D.ActiveModel = req.ActiveLive2D
		}
	})

	h.GetHealth(c)
}

// ==========================================
// 2. Authentication & Profile Handlers
// ==========================================

func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.AuthUC.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Username        string `json:"username" binding:"required"`
		Email           string `json:"email" binding:"required"`
		Password        string `json:"password" binding:"required"`
		JLPTTargetLevel string `json:"jlpt_target_level"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.AuthUC.Register(req.Username, req.Email, req.Password, req.JLPTTargetLevel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) GetProfile(c *gin.Context) {
	user, err := h.AuthUC.GetOrDefaultUser(getUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *Handler) UpdateProfile(c *gin.Context) {
	var req struct {
		Username        string `json:"username"`
		Email           string `json:"email"`
		Password        string `json:"password"`
		JLPTTargetLevel string `json:"jlpt_target_level"`
		DailyGoalMins   int    `json:"daily_goal_mins"`
		PreferredVoice  string `json:"preferred_voice"`
		OfflineMode     bool   `json:"offline_mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	user, err := h.AuthUC.UpdateProfile(
		getUserID(c),
		req.Username,
		req.Email,
		req.Password,
		req.JLPTTargetLevel,
		req.DailyGoalMins,
		req.PreferredVoice,
		req.OfflineMode,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.PreferredVoice != "" {
		_ = h.TTSUC.SetDefaultVoice(req.PreferredVoice, "", 0)
	}
	c.JSON(http.StatusOK, user)
}

// ==========================================
// 3. Dashboard & Progress Tracker Handlers
// ==========================================

func (h *Handler) GetDashboardSummary(c *gin.Context) {
	summary, err := h.ProgressUC.GetDashboardSummary(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, summary)
}

// ==========================================
// 4. SRS Flashcards & Decks Handlers
// ==========================================

func (h *Handler) ListDecks(c *gin.Context) {
	decks, err := h.SRSUC.ListDecks(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, decks)
}

func (h *Handler) CreateDeck(c *gin.Context) {
	var req struct {
		Name          string `json:"name" binding:"required"`
		Description   string `json:"description"`
		JLPTLevel     string `json:"jlpt_level"`
		Category      string `json:"category"`
		AutoSeedCount int    `json:"auto_seed_count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	deck, err := h.SRSUC.CreateDeck(getUserID(c), req.Name, req.Description, req.JLPTLevel, req.Category, req.AutoSeedCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, deck)
}

func (h *Handler) ListCards(c *gin.Context) {
	deckID, _ := strconv.ParseInt(c.Query("deck_id"), 10, 64)
	onlyDue := c.Query("due_only") == "true"
	cards, err := h.SRSUC.ListCards(getUserID(c), deckID, onlyDue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cards)
}

func (h *Handler) CreateCard(c *gin.Context) {
	var req domain.Flashcard
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	card, err := h.SRSUC.CreateCard(getUserID(c), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, card)
}

func (h *Handler) SubmitReview(c *gin.Context) {
	var req domain.ReviewSubmission
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	card, err := h.SRSUC.SubmitReview(getUserID(c), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, card)
}

// ==========================================
// 5. Anime & Music Sentence Mining Handlers
// ==========================================

func (h *Handler) GetMiningPresets(c *gin.Context) {
	c.JSON(http.StatusOK, h.MiningUC.GetMediaPresets())
}

func (h *Handler) ParseMediaScript(c *gin.Context) {
	var req struct {
		RawContent string `json:"raw_content" binding:"required"`
		Format     string `json:"format"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cues := h.MiningUC.ParseMediaText(req.RawContent, req.Format)
	c.JSON(http.StatusOK, gin.H{
		"cues":  cues,
		"total": len(cues),
	})
}

func (h *Handler) MatchSentenceVocab(c *gin.Context) {
	var req struct {
		Sentence string `json:"sentence" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tokens, n4n3Count, coverage := h.MiningUC.MatchSentenceVocab(req.Sentence)
	c.JSON(http.StatusOK, gin.H{
		"matched_vocab": tokens,
		"n4_n3_count":   n4n3Count,
		"coverage_rate": coverage,
	})
}

func (h *Handler) ListMinedSentences(c *gin.Context) {
	list, err := h.MiningUC.ListMinedSentences(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *Handler) SaveMinedSentence(c *gin.Context) {
	var req struct {
		MediaTitle     string `json:"media_title"`
		MediaType      string `json:"media_type"`
		Sentence       string `json:"sentence" binding:"required"`
		Reading        string `json:"reading"`
		Translation    string `json:"translation"`
		StartTimeMs    int64  `json:"start_time_ms"`
		EndTimeMs      int64  `json:"end_time_ms"`
		TimestampLabel string `json:"timestamp_label"`
		AudioURL       string `json:"audio_url"`
		TargetWord     string `json:"target_word"`
		DeckID         int64  `json:"deck_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.MediaTitle == "" {
		req.MediaTitle = "Anime / Song Immersion Clip"
	}
	if req.MediaType == "" {
		req.MediaType = "anime"
	}
	mined := &domain.MinedSentence{
		MediaTitle:     req.MediaTitle,
		MediaType:      req.MediaType,
		Sentence:       req.Sentence,
		Reading:        req.Reading,
		Translation:    req.Translation,
		StartTimeMs:    req.StartTimeMs,
		EndTimeMs:      req.EndTimeMs,
		TimestampLabel: req.TimestampLabel,
		AudioURL:       req.AudioURL,
	}
	savedMined, card, err := h.MiningUC.SaveMinedSentenceToDeck(getUserID(c), mined, req.TargetWord, req.DeckID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"mined_sentence": savedMined,
		"flashcard":      card,
	})
}

// ==========================================
// 6. OpenJLPT Dictionary & Sync Handlers
// ==========================================

func (h *Handler) SearchVocab(c *gin.Context) {
	level := c.DefaultQuery("level", "N4")
	query := c.Query("q")
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "48"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	items, total, err := h.JLPTUC.SearchVocab(getUserID(c), level, query, status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func (h *Handler) SearchKanji(c *gin.Context) {
	level := c.DefaultQuery("level", "N4")
	query := c.Query("q")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "60"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	items, total, err := h.JLPTUC.SearchKanji(getUserID(c), level, query, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func (h *Handler) SearchGrammar(c *gin.Context) {
	level := c.DefaultQuery("level", "N4")
	query := c.Query("q")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "40"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	items, total, err := h.JLPTUC.SearchGrammar(getUserID(c), level, query, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func (h *Handler) ToggleMastery(c *gin.Context) {
	var req struct {
		ItemType string `json:"item_type" binding:"required"`
		ItemID   string `json:"item_id" binding:"required"`
		Level    string `json:"level" binding:"required"`
		Status   string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.JLPTUC.ToggleMastery(getUserID(c), req.ItemType, req.ItemID, req.Level, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) GetOpenJLPTStats(c *gin.Context) {
	stats, err := h.JLPTUC.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) SyncOpenJLPT(c *gin.Context) {
	stats, err := h.JLPTUC.SyncFromOpenJLPT()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// ==========================================
// 7. Windows VOICEVOX TTS Handlers
// ==========================================

func (h *Handler) GetTTSStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.TTSUC.GetStatus())
}

func (h *Handler) ChangeVoicevoxVoice(c *gin.Context) {
	var req struct {
		SpeakerID   string  `json:"speaker_id" binding:"required"`
		VoicevoxURL string  `json:"voicevox_url"`
		Speed       float64 `json:"speed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.TTSUC.SetDefaultVoice(req.SpeakerID, req.VoicevoxURL, req.Speed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.TTSUC.GetStatus())
}

func (h *Handler) SpeakTTS(c *gin.Context) {
	var req domain.TTSSpeakRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.TTSUC.Synthesize(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) ServeTTSAudio(c *gin.Context) {
	filename := filepath.Base(c.Param("filename"))
	fullPath := filepath.Join(h.Cfg.TTS.CacheDir, filename)
	c.Header("Content-Type", "audio/wav")
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(fullPath)
}

// ==========================================
// 8. Local AI Chatbot & Live2D Tutor Handlers
// ==========================================

func (h *Handler) ListChatHistory(c *gin.Context) {
	msgs, err := h.TutorUC.ListHistory(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, msgs)
}

func (h *Handler) SendChatMessage(c *gin.Context) {
	var req domain.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.TutorUC.Chat(getUserID(c), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) ClearChatHistory(c *gin.Context) {
	if err := h.TutorUC.ClearHistory(getUserID(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cleared"})
}

// ==========================================
// 9. Auto-Translate (EN -> Bahasa Indonesia)
// ==========================================

func (h *Handler) TranslateTexts(c *gin.Context) {
	var req struct {
		Text        string   `json:"text"`
		Texts       []string `json:"texts"`
		AllowOnline bool     `json:"allow_online"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Text != "" && len(req.Texts) == 0 {
		req.Texts = []string{req.Text}
	}
	translations := h.TranslateUC.TranslateBatchENtoID(req.Texts, req.AllowOnline)
	single := ""
	if req.Text != "" {
		single = translations[req.Text]
	}
	c.JSON(http.StatusOK, gin.H{
		"translated":   single,
		"translations": translations,
	})
}
