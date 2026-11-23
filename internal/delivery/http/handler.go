package http

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
		MediaFilename  string `json:"media_filename"`
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
		req.MediaTitle = "Anime / Video Immersion Clip"
	}
	if req.MediaType == "" {
		req.MediaType = "anime"
	}

	// If mined from an uploaded video file, slice the ORIGINAL video voice clip into a permanent WAV file for SRS flashcards!
	audioURL := req.AudioURL
	if req.MediaFilename != "" {
		if _, publicURL, err := h.MiningUC.SliceOriginalAudioClip("./data/media_uploads", req.MediaFilename, req.StartTimeMs, req.EndTimeMs); err == nil {
			audioURL = publicURL
		}
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
		AudioURL:       audioURL,
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

// UploadVideoForSSE saves the uploaded video quickly and returns media_filename + media_url
// so the frontend can immediately open an SSE stream (/api/v1/mining/extract-sse) for real-time Japanese sentence extraction.
func (h *Handler) UploadVideoForSSE(c *gin.Context) {
	file, err := c.FormFile("video")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing video file in form data"})
		return
	}

	uploadsDir := "./data/media_uploads"
	_ = os.MkdirAll(uploadsDir, 0755)

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		ext = ".mp4"
	}
	safeName := fmt.Sprintf("video_%d%s", time.Now().UnixNano(), ext)
	savedPath := filepath.Join(uploadsDir, safeName)

	if err := c.SaveUploadedFile(file, savedPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save uploaded video: " + err.Error()})
		return
	}

	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(file.Filename, filepath.Ext(file.Filename))
	}

	// Save metadata sidecar so previously uploaded videos can be selected anytime without re-uploading
	metaBytes, _ := json.Marshal(map[string]interface{}{
		"media_filename": safeName,
		"media_title":    title,
		"media_url":      "/api/v1/mining/media/" + safeName,
		"size_bytes":     file.Size,
		"uploaded_at":    time.Now().Format(time.RFC3339),
	})
	_ = os.WriteFile(savedPath+".meta.json", metaBytes, 0644)

	c.JSON(http.StatusOK, gin.H{
		"media_filename": safeName,
		"media_title":    title,
		"media_url":      "/api/v1/mining/media/" + safeName,
		"size_bytes":     file.Size,
	})
}

// ListUploadedVideos returns all videos in ./data/media_uploads so the learner can re-extract or study any uploaded video without re-uploading.
func (h *Handler) ListUploadedVideos(c *gin.Context) {
	uploadsDir := "./data/media_uploads"
	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	var videos []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".srt") || strings.HasSuffix(lower, ".16k.wav") {
			continue
		}
		if !(strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mkv") || strings.HasSuffix(lower, ".webm") || strings.HasSuffix(lower, ".mov") || strings.HasSuffix(lower, ".mp3") || strings.HasSuffix(lower, ".wav")) {
			continue
		}

		info, _ := entry.Info()
		sizeBytes := int64(0)
		modTime := time.Now()
		if info != nil {
			sizeBytes = info.Size()
			modTime = info.ModTime()
		}

		title := name
		if metaBytes, err := os.ReadFile(filepath.Join(uploadsDir, name+".meta.json")); err == nil {
			var meta map[string]interface{}
			if json.Unmarshal(metaBytes, &meta) == nil {
				if t, ok := meta["media_title"].(string); ok && t != "" {
					title = t
				}
			}
		}

		videos = append(videos, map[string]interface{}{
			"media_filename": name,
			"media_title":    title,
			"media_url":      "/api/v1/mining/media/" + name,
			"size_bytes":     sizeBytes,
			"uploaded_at":    modTime.Format("2006-01-02 15:04:05"),
		})
	}

	sort.Slice(videos, func(i, j int) bool {
		return fmt.Sprint(videos[i]["uploaded_at"]) > fmt.Sprint(videos[j]["uploaded_at"])
	})
	if videos == nil {
		videos = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, videos)
}

// StreamVideoExtractionSSE streams real-time Server-Sent Events (SSE) while extracting ONLY Japanese speech (language="ja")
// from the uploaded video via FFmpeg + Local Offline Faster-Whisper, matching OpenJLPT N4/N3 vocab on each sentence as it arrives.
func (h *Handler) StreamVideoExtractionSSE(c *gin.Context) {
	mediaFilename := filepath.Base(c.Query("media"))
	startSec, _ := strconv.Atoi(c.DefaultQuery("start_sec", "0"))
	durationSec, _ := strconv.Atoi(c.DefaultQuery("duration_sec", "0"))

	if startSec < 0 {
		startSec = 0
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	sendSSE := func(payload interface{}) {
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(b))
		c.Writer.Flush()
	}

	uploadsDir := "./data/media_uploads"
	videoPath := filepath.Join(uploadsDir, mediaFilename)
	if _, err := os.Stat(videoPath); err != nil {
		sendSSE(map[string]interface{}{
			"type":    "error",
			"message": "File video tidak ditemukan: " + mediaFilename,
		})
		return
	}

	ctx := c.Request.Context()

	// Step 1: Extract 16kHz mono WAV for the target range using FFmpeg
	rangeDesc := "Full Video"
	if durationSec > 0 {
		rangeDesc = fmt.Sprintf("Detik %d s/d %d (%d detik)", startSec, startSec+durationSec, durationSec)
	}
	sendSSE(map[string]interface{}{
		"type":     "status",
		"stage":    "ffmpeg",
		"progress": 8,
		"message":  fmt.Sprintf("Mengekstrak track audio Jepang (16kHz WAV) via FFmpeg [%s]...", rangeDesc),
	})

	tmpWav := filepath.Join(uploadsDir, fmt.Sprintf("%s.%d_%d.16k.wav", mediaFilename, startSec, durationSec))
	defer os.Remove(tmpWav)

	ffArgs := []string{"-y"}
	if startSec > 0 {
		ffArgs = append(ffArgs, "-ss", strconv.Itoa(startSec))
	}
	ffArgs = append(ffArgs, "-i", videoPath)
	if durationSec > 0 {
		ffArgs = append(ffArgs, "-t", strconv.Itoa(durationSec))
	}
	ffArgs = append(ffArgs, "-vn", "-ac", "1", "-ar", "16000", "-f", "wav", tmpWav)

	cmdAudio := exec.CommandContext(ctx, "ffmpeg", ffArgs...)
	if out, err := cmdAudio.CombinedOutput(); err != nil {
		sendSSE(map[string]interface{}{
			"type":    "error",
			"message": fmt.Sprintf("FFmpeg gagal mengekstrak audio: %v (%s)", err, string(out)),
		})
		return
	}

	// Step 2: Run whisper_extract.py (Target ONLY Japanese speech: language="ja") and stream NDJSON lines as SSE
	pythonBin := "/home/hideri/coding/Open-LLM-VTuber/.venv/bin/python3"
	if _, err := os.Stat(pythonBin); err != nil {
		pythonBin = "python3"
	}
	scriptPath, _ := filepath.Abs("./scripts/whisper_extract.py")
	offsetMs := startSec * 1000

	cmdWhisper := exec.CommandContext(
		ctx,
		pythonBin,
		scriptPath,
		"--input", tmpWav,
		"--offset-ms", strconv.Itoa(offsetMs),
	)

	stdoutPipe, err := cmdWhisper.StdoutPipe()
	if err != nil {
		sendSSE(map[string]interface{}{
			"type":    "error",
			"message": "Gagal membuka pipe Faster-Whisper: " + err.Error(),
		})
		return
	}

	if err := cmdWhisper.Start(); err != nil {
		sendSSE(map[string]interface{}{
			"type":    "error",
			"message": "Gagal menjalankan Faster-Whisper: " + err.Error(),
		})
		return
	}

	scanner := bufio.NewScanner(stdoutPipe)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		evType, _ := raw["type"].(string)
		if evType == "segment" {
			sentence, _ := raw["sentence"].(string)
			startMs := int64(0)
			endMs := int64(0)
			idx := 1
			if v, ok := raw["start_time_ms"].(float64); ok {
				startMs = int64(v)
			}
			if v, ok := raw["end_time_ms"].(float64); ok {
				endMs = int64(v)
			}
			if v, ok := raw["index"].(float64); ok {
				idx = int(v)
			}

			// Match Japanese sentence against OpenJLPT N4/N3 SQLite database in real-time (<0.5ms)
			matched, n4n3Count, coverage := h.MiningUC.MatchSentenceVocab(sentence)
			clipURL := fmt.Sprintf(
				"/api/v1/mining/clip?media=%s&start_ms=%d&end_ms=%d",
				url.QueryEscape(mediaFilename),
				startMs,
				endMs,
			)
			cue := domain.MediaCue{
				Index:          idx,
				StartTimeMs:    startMs,
				EndTimeMs:      endMs,
				TimestampLabel: h.MiningUC.FormatTimestampRange(startMs, endMs),
				Sentence:       sentence,
				ClipAudioURL:   clipURL,
				MatchedVocab:   matched,
				N4N3Count:      n4n3Count,
				CoverageRate:   coverage,
			}
			raw["cue"] = cue
		}

		sendSSE(raw)
	}

	_ = cmdWhisper.Wait()
}

// ExtractUploadedVideo handles video file upload (.mp4, .mkv, .webm, .mov, .mp3, .wav),
// extracts per-sentence timestamps & subtitles via FFmpeg + Local Offline Faster-Whisper,
// and links every sentence to the ORIGINAL video's voice audio slice!
func (h *Handler) ExtractUploadedVideo(c *gin.Context) {
	file, err := c.FormFile("video")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing video file in form data"})
		return
	}

	uploadsDir := "./data/media_uploads"
	_ = os.MkdirAll(uploadsDir, 0755)

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		ext = ".mp4"
	}
	safeName := fmt.Sprintf("video_%d%s", time.Now().UnixNano(), ext)
	savedPath := filepath.Join(uploadsDir, safeName)

	if err := c.SaveUploadedFile(file, savedPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save uploaded video: " + err.Error()})
		return
	}

	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(file.Filename, filepath.Ext(file.Filename))
	}

	result, err := h.MiningUC.ExtractVideoToTimestampCues(savedPath, safeName, title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ServeVideoAudioClip slices and streams the exact [start_ms, end_ms] original voice segment from an uploaded video.
func (h *Handler) ServeVideoAudioClip(c *gin.Context) {
	media := c.Query("media")
	startMs, _ := strconv.ParseInt(c.Query("start_ms"), 10, 64)
	endMs, _ := strconv.ParseInt(c.Query("end_ms"), 10, 64)
	if media == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing media parameter"})
		return
	}

	clipPath, _, err := h.MiningUC.SliceOriginalAudioClip("./data/media_uploads", media, startMs, endMs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "audio/wav")
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(clipPath)
}

// ServeUploadedMedia streams uploaded video files or sliced original audio clips with HTTP Range support.
func (h *Handler) ServeUploadedMedia(c *gin.Context) {
	relPath := strings.TrimLeft(filepath.Clean(c.Param("filepath")), "/\\")
	if strings.Contains(relPath, "..") {
		c.Status(http.StatusForbidden)
		return
	}
	fullPath := filepath.Join("./data/media_uploads", relPath)
	c.File(fullPath)
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
