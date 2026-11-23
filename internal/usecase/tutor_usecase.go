package usecase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/repository/sqlite"
)

type TutorUsecase struct {
	Cfg       *config.AppConfig
	TutorRepo *sqlite.TutorRepository
	JLPTRepo  *sqlite.JLPTRepository
	TTS       *TTSUsecase
}

var emotionTagRegex = regexp.MustCompile(`\[(joy|blush|surprise|sadness|anger|neutral)\]`)

func NewTutorUsecase(cfg *config.AppConfig, tutorRepo *sqlite.TutorRepository, jlptRepo *sqlite.JLPTRepository, tts *TTSUsecase) *TutorUsecase {
	return &TutorUsecase{
		Cfg:       cfg,
		TutorRepo: tutorRepo,
		JLPTRepo:  jlptRepo,
		TTS:       tts,
	}
}

func (u *TutorUsecase) ListHistory(userID int64) ([]domain.ChatMessage, error) {
	return u.TutorRepo.ListMessages(userID, 60)
}

func (u *TutorUsecase) ClearHistory(userID int64) error {
	return u.TutorRepo.ClearHistory(userID)
}

// Chat sends the learner's message to the Local AI (Ollama / OpenAI-compatible server via WSL2-to-Windows
// auto-gateway) or falls back to the Offline OpenJLPT Japanese Sensei engine if Ollama is offline,
// parses Live2D emotion tags, and optionally synthesizes VOICEVOX speech + lip-sync volumes.
func (u *TutorUsecase) Chat(userID int64, req domain.ChatRequest) (*domain.ChatResponse, error) {
	mode := req.Mode
	if mode == "" {
		mode = "sensei"
	}

	userMsg := &domain.ChatMessage{
		UserID:    userID,
		Role:      "user",
		Content:   req.Message,
		CleanText: req.Message,
		Emotion:   "neutral",
		Mode:      mode,
	}
	_ = u.TutorRepo.SaveMessage(userMsg)

	// Probe Local LLM (Ollama on localhost or Windows Host Gateway IP)
	resolvedLLM, llmOnline, _ := config.ResolveServiceURL(u.Cfg.LLM.BaseURL, "models", 900*time.Millisecond)
	providerUsed := "offline-openjlpt-sensei"
	var rawReply string

	if llmOnline {
		if reply, err := u.callLocalLLM(resolvedLLM, req.Message, mode); err == nil && strings.TrimSpace(reply) != "" {
			rawReply = reply
			providerUsed = "ollama-local (" + u.Cfg.LLM.Model + ")"
		}
	}

	if strings.TrimSpace(rawReply) == "" {
		rawReply = u.generateOfflineSenseiReply(userID, req.Message, mode)
	}

	emotion, cleanText, jaSpeech := parseEmotionAndSpeech(rawReply)

	assistantMsg := &domain.ChatMessage{
		UserID:    userID,
		Role:      "assistant",
		Content:   rawReply,
		CleanText: cleanText,
		Reading:   jaSpeech,
		Emotion:   emotion,
		Mode:      mode,
	}

	if req.WithTTS {
		ttsRes, err := u.TTS.Synthesize(domain.TTSSpeakRequest{
			Text:      jaSpeech,
			SpeakerID: req.SpeakerID,
			Emotion:   emotion,
		})
		if err == nil && ttsRes != nil {
			assistantMsg.AudioURL = ttsRes.AudioURL
			assistantMsg.Volumes = ttsRes.Volumes
		}
	}

	_ = u.TutorRepo.SaveMessage(assistantMsg)

	return &domain.ChatResponse{
		Reply:         *assistantMsg,
		LLMProvider:   providerUsed,
		ResolvedLLMIP: resolvedLLM,
	}, nil
}

func (u *TutorUsecase) callLocalLLM(baseURL, message, mode string) (string, error) {
	sysPrompt := fmt.Sprintf(
		"You are %s, an interactive Live2D Japanese JLPT N4 and N3 Sensei tutor. "+
			"Always start your response with one emotion tag from [joy], [blush], [surprise], [sadness], [anger], or [neutral] so your Live2D avatar reacts. "+
			"Include a natural Japanese sentence first (with furigana/romaji in parentheses), followed by a concise, helpful explanation for a JLPT N4/N3 learner. Current study mode: %s.",
		u.Cfg.LLM.SenseiName, mode,
	)

	payload := map[string]interface{}{
		"model": u.Cfg.LLM.Model,
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": message},
		},
		"temperature": u.Cfg.LLM.Temperature,
		"stream":      false,
	}
	bodyBytes, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 12 * time.Second}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if u.Cfg.LLM.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+u.Cfg.LLM.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm status %d", resp.StatusCode)
	}

	var resData struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil || len(resData.Choices) == 0 {
		return "", fmt.Errorf("empty llm response")
	}
	return resData.Choices[0].Message.Content, nil
}

// generateOfflineSenseiReply queries the local SQLite OpenJLPT database (vocab, grammar, kanji)
// so that even without Ollama or internet access, the AI Sensei provides accurate JLPT N4/N3 explanations!
func (u *TutorUsecase) generateOfflineSenseiReply(userID int64, userText string, mode string) string {
	q := strings.TrimSpace(userText)
	lower := strings.ToLower(q)

	switch mode {
	case "quiz":
		vocabs, _, _ := u.JLPTRepo.SearchVocab(userID, "N3", "", "", 15, 0)
		if len(vocabs) > 0 {
			pick := vocabs[int(time.Now().Unix())%len(vocabs)]
			exJa := "毎日コツコツ勉強するのが大切です。"
			if len(pick.Examples) > 0 {
				exJa = pick.Examples[0].Japanese
			}
			return fmt.Sprintf(
				"[surprise] よし、クイズの時間ですよ！ (Yoshi, kuizu no jikan desu yo!)\n\n"+
					"**JLPT %s Challenge:** What is the reading and meaning of **「%s」**?\n"+
					"• **Hint / Example:** %s\n"+
					"• **Answer Preview:** Reading: `%s (%s)` — Meaning: *%s*\n\n"+
					"Try pronouncing it aloud or click the VOICEVOX speaker button!",
				pick.Level, pick.Word, exJa, pick.Reading, pick.Romaji, strings.Join(pick.Meanings, ", "),
			)
		}

	case "roleplay":
		return "[blush] いらっしゃいませ！東京のアニメカフェへようこそ！今日は何を注文しますか？ (Irasshaimase! Welcome to the Tokyo Anime Cafe! What would you like to order today?)\n\n" +
			"• **Useful N4 Pattern:** `〜をお願いします (~o onegaishimasu)` — I'd like ~, please.\n" +
			"• **Useful N3 Pattern:** `おすすめは何ですか？ (Osusume wa nan desu ka?)` — What do you recommend?\n" +
			"Reply in Japanese or Romaji to continue our roleplay!"
	}

	// Search local OpenJLPT grammar or vocab matching the user's query
	searchKey := q
	for _, prefix := range []string{"explain ", "what is ", "meaning of ", "grammar ", "how to use ", "?"} {
		searchKey = strings.ReplaceAll(strings.ToLower(searchKey), prefix, "")
	}
	searchKey = strings.TrimSpace(searchKey)

	if len(searchKey) >= 1 {
		grammars, _, _ := u.JLPTRepo.SearchGrammar(userID, "ALL", searchKey, 2, 0)
		if len(grammars) > 0 {
			g := grammars[0]
			exLine := ""
			if len(g.Examples) > 0 {
				exLine = fmt.Sprintf("\n• **Example:** 「%s」 — *%s*", g.Examples[0].Japanese, g.Examples[0].English)
			}
			return fmt.Sprintf(
				"[joy] いい質問ですね！それについて説明しますね。 (Ii shitsumon desu ne! Let me explain that grammar point!)\n\n"+
					"**OpenJLPT %s Grammar: 「%s」 (%s)**\n"+
					"• **Meaning:** %s\n"+
					"• **Formation:** `%s`%s\n"+
					"• **Sensei Note:** %s",
				g.Level, g.Pattern, g.Romaji, g.Meaning, g.Formation, exLine, g.Notes,
			)
		}

		vocabs, _, _ := u.JLPTRepo.SearchVocab(userID, "ALL", searchKey, "", 2, 0)
		if len(vocabs) > 0 {
			v := vocabs[0]
			exLine := fmt.Sprintf("%sを使って文を作ってみましょう！", v.Word)
			if len(v.Examples) > 0 {
				exLine = fmt.Sprintf("「%s」 (%s)", v.Examples[0].Japanese, v.Examples[0].English)
			}
			return fmt.Sprintf(
				"[joy] なるほど！「%s（%s）」はJLPT %sのとても大事な言葉ですよ。\n\n"+
					"**OpenJLPT %s Vocabulary: 「%s」**\n"+
					"• **Reading:** `%s` (%s)\n"+
					"• **Meanings:** %s\n"+
					"• **Context Example:** %s",
				v.Word, v.Reading, v.Level, v.Level, v.Word, v.Reading, v.Romaji, strings.Join(v.Meanings, "; "), exLine,
			)
		}
	}

	if strings.Contains(lower, "n4") || strings.Contains(lower, "n3") {
		return "[joy] 一緒にJLPT合格を目指して頑張りましょう！ (Issho ni JLPT goukaku o mezashite ganbarimashou!)\n\n" +
			"To reach **90% JLPT N4 & N3 mastery within 6 months**, I recommend:\n" +
			"1. **15 mins SRS Flashcards** daily (focus on your Weak Spots list).\n" +
			"2. **10 mins Anime/Music Sentence Mining** with timestamp audio replay.\n" +
			"3. **5 mins Speaking Practice** with me using Windows VOICEVOX voices!"
	}

	return "[blush] べ、別にあんたのために教えるんじゃないからね！でも…毎日勉強してるのは偉いと思うよ。 (I-it's not like I'm teaching this just for you! B-but I think it's admirable you're studying every day!)\n\n" +
		"Ask me about any **JLPT N4/N3 vocabulary, kanji, or grammar pattern** (e.g., `諦める`, `約束`, `〜てあげる`, `ように`), or switch to **Quiz** or **Roleplay** mode!"
}

func parseEmotionAndSpeech(raw string) (emotion string, clean string, japaneseSpeech string) {
	emotion = "joy"
	if m := emotionTagRegex.FindStringSubmatch(raw); len(m) == 2 {
		emotion = m[1]
	}
	clean = strings.TrimSpace(emotionTagRegex.ReplaceAllString(raw, ""))

	// Extract the Japanese portion at the start of the message for clean VOICEVOX synthesis
	firstLine := strings.Split(clean, "\n")[0]
	if idx := strings.Index(firstLine, "("); idx > 0 {
		firstLine = strings.TrimSpace(firstLine[:idx])
	}
	if len([]rune(firstLine)) > 80 {
		runes := []rune(firstLine)
		firstLine = string(runes[:80])
	}
	japaneseSpeech = firstLine
	if japaneseSpeech == "" {
		japaneseSpeech = "一緒に日本語を勉強しましょう！"
	}
	return emotion, clean, japaneseSpeech
}
