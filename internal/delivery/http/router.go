package http

import (
	"github.com/gin-gonic/gin"
)

func NewRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(CORSMiddleware())
	r.Use(LatencyHeaderMiddleware())
	r.Use(OfflineFirstAuthMiddleware(h.AuthUC))

	api := r.Group("/api/v1")
	{
		// Health & Settings
		api.GET("/health", h.GetHealth)
		api.PUT("/settings", h.UpdateSettings)

		// Auth & Profile
		api.POST("/auth/login", h.Login)
		api.POST("/auth/register", h.Register)
		api.GET("/auth/me", h.GetProfile)
		api.PUT("/auth/profile", h.UpdateProfile)

		// Dashboard & Kanji/Grammar Progress Tracker
		api.GET("/dashboard/summary", h.GetDashboardSummary)

		// Interactive SRS Flashcards
		api.GET("/srs/decks", h.ListDecks)
		api.POST("/srs/decks", h.CreateDeck)
		api.GET("/srs/cards", h.ListCards)
		api.POST("/srs/cards", h.CreateCard)
		api.POST("/srs/review", h.SubmitReview)

		// Anime & Music Sentence Mining + OpenJLPT Matcher
		api.GET("/mining/presets", h.GetMiningPresets)
		api.POST("/mining/parse", h.ParseMediaScript)
		api.POST("/mining/match", h.MatchSentenceVocab)
		api.GET("/mining/sentences", h.ListMinedSentences)
		api.POST("/mining/sentences", h.SaveMinedSentence)

		// OpenJLPT Offline Dictionary & Scraper Sync
		api.GET("/jlpt/vocab", h.SearchVocab)
		api.GET("/jlpt/kanji", h.SearchKanji)
		api.GET("/jlpt/grammar", h.SearchGrammar)
		api.POST("/jlpt/mastery", h.ToggleMastery)
		api.GET("/jlpt/stats", h.GetOpenJLPTStats)
		api.POST("/jlpt/sync", h.SyncOpenJLPT)

		// Windows VOICEVOX TTS & Voice Switcher
		api.GET("/tts/status", h.GetTTSStatus)
		api.POST("/tts/voice", h.ChangeVoicevoxVoice)
		api.POST("/tts/speak", h.SpeakTTS)
		api.GET("/tts/audio/:filename", h.ServeTTSAudio)

		// Local AI Sensei Chatbot & Live2D Companion
		api.GET("/tutor/history", h.ListChatHistory)
		api.POST("/tutor/chat", h.SendChatMessage)
		api.DELETE("/tutor/history", h.ClearChatHistory)
	}

	return r
}
