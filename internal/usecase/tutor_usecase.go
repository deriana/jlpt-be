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
	Cfg         *config.AppConfig
	TutorRepo   *sqlite.TutorRepository
	JLPTRepo    *sqlite.JLPTRepository
	TTS         *TTSUsecase
	TranslateUC *TranslateUsecase
}

var emotionTagRegex = regexp.MustCompile(`\[(joy|blush|surprise|sadness|anger|neutral)\]`)

func NewTutorUsecase(cfg *config.AppConfig, tutorRepo *sqlite.TutorRepository, jlptRepo *sqlite.JLPTRepository, tts *TTSUsecase, translateUC *TranslateUsecase) *TutorUsecase {
	return &TutorUsecase{
		Cfg:         cfg,
		TutorRepo:   tutorRepo,
		JLPTRepo:    jlptRepo,
		TTS:         tts,
		TranslateUC: translateUC,
	}
}

func (u *TutorUsecase) ListHistory(userID int64) ([]domain.ChatMessage, error) {
	return u.TutorRepo.ListMessages(userID, 60)
}

func (u *TutorUsecase) ClearHistory(userID int64) error {
	return u.TutorRepo.ClearHistory(userID)
}

func (u *TutorUsecase) Chat(userID int64, req domain.ChatRequest) (*domain.ChatResponse, error) {
	mode := req.Mode
	if mode == "" {
		mode = "sensei"
	}
	lang := strings.ToLower(strings.TrimSpace(req.Lang))
	if lang == "" {
		lang = "id"
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

	resolvedLLM, llmOnline, _ := config.ResolveServiceURL(u.Cfg.LLM.BaseURL, "models", 900*time.Millisecond)
	providerUsed := "offline-openjlpt-sensei"
	var rawReply string

	if llmOnline {
		if reply, err := u.callLocalLLM(resolvedLLM, req.Message, mode, lang); err == nil && strings.TrimSpace(reply) != "" {
			rawReply = reply
			providerUsed = "ollama-local (" + u.Cfg.LLM.Model + ")"
		}
	}

	if strings.TrimSpace(rawReply) == "" {
		rawReply = u.generateOfflineSenseiReply(userID, req.Message, mode, lang)
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

func (u *TutorUsecase) callLocalLLM(baseURL, message, mode, lang string) (string, error) {
	langInstruction := "Explain clearly in Bahasa Indonesia while providing Japanese sentences with Furigana/Romaji."
	if lang == "en" {
		langInstruction = "Explain clearly in English while providing Japanese sentences with Furigana/Romaji."
	}

	sysPrompt := fmt.Sprintf(
		"You are %s, an interactive Live2D Japanese JLPT N4 and N3 Sensei tutor. "+
			"Always start your response with one emotion tag from [joy], [blush], [surprise], [sadness], [anger], or [neutral] so your Live2D avatar reacts. "+
			"Include a natural Japanese sentence on the first line (with romaji in parentheses), followed by a concise explanation. %s Current study mode: %s.",
		u.Cfg.LLM.SenseiName, langInstruction, mode,
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

func (u *TutorUsecase) generateOfflineSenseiReply(userID int64, userText string, mode string, lang string) string {
	q := strings.TrimSpace(userText)
	isIndo := lang != "en"

	switch mode {
	case "quiz":
		vocabs, _, _ := u.JLPTRepo.SearchVocab(userID, "N3", "", "", 15, 0)
		if len(vocabs) > 0 {
			pick := vocabs[int(time.Now().Unix())%len(vocabs)]
			exJa := "毎日コツコツ勉強するのが大切です。"
			if len(pick.Examples) > 0 {
				exJa = pick.Examples[0].Japanese
			}
			meaningStr := strings.Join(pick.Meanings, ", ")
			if isIndo && u.TranslateUC != nil {
				meaningStr = u.TranslateUC.TranslateENtoID(meaningStr, true)
			}
			if isIndo {
				return fmt.Sprintf(
					"[surprise] よし、クイズの時間ですよ！ (Yoshi, waktunya kuis JLPT!)\n\n"+
						"**Tantangan JLPT %s:** Apa cara baca dan arti dari **「%s」**?\n"+
						"• **Contoh Kalimat:** %s\n"+
						"• **Kunci Jawaban:** Cara baca: `%s (%s)` — Arti: *%s*\n\n"+
						"Coba ucapkan keras-keras atau klik tombol VOICEVOX untuk mendengar pelafalannya!",
					pick.Level, pick.Word, exJa, pick.Reading, pick.Romaji, meaningStr,
				)
			}
			return fmt.Sprintf(
				"[surprise] よし、クイズの時間ですよ！ (Yoshi, kuizu no jikan desu yo!)\n\n"+
					"**JLPT %s Challenge:** What is the reading and meaning of **「%s」**?\n"+
					"• **Hint / Example:** %s\n"+
					"• **Answer Preview:** Reading: `%s (%s)` — Meaning: *%s*",
				pick.Level, pick.Word, exJa, pick.Reading, pick.Romaji, meaningStr,
			)
		}

	case "roleplay":
		if isIndo {
			return "[blush] いらっしゃいませ！東京のアニメカフェへようこそ！今日は何を注文しますか？ (Irasshaimase! Selamat datang di Kafe Anime Tokyo! Hari ini mau pesan apa?)\n\n" +
				"• **Pola N4 Penting:** `〜をお願いします (~o onegaishimasu)` — Tolong pesan ~.\n" +
				"• **Pola N3 Penting:** `おすすめは何ですか？ (Osusume wa nan desu ka?)` — Apa menu rekomendasinya?\n" +
				"Balas dengan bahasa Jepang atau Romaji untuk lanjut latihan percakapan!"
		}
		return "[blush] いらっしゃいませ！東京のアニメカフェへようこそ！今日は何を注文しますか？ (Irasshaimase! Welcome to the Tokyo Anime Cafe! What would you like to order today?)\n\n" +
			"• **Useful N4 Pattern:** `〜をお願いします (~o onegaishimasu)` — I'd like ~, please.\n" +
			"• **Useful N3 Pattern:** `おすすめは何ですか？ (Osusume wa nan desu ka?)` — What do you recommend?"
	}

	searchKey := q
	for _, prefix := range []string{"jelaskan ", "apa arti ", "arti ", "grammar ", "pola ", "explain ", "what is ", "meaning of ", "how to use ", "with examples", "?"} {
		searchKey = strings.ReplaceAll(strings.ToLower(searchKey), prefix, "")
	}
	searchKey = strings.TrimSpace(searchKey)

	if len(searchKey) >= 1 {
		grammars, _, _ := u.JLPTRepo.SearchGrammar(userID, "ALL", searchKey, 2, 0)
		if len(grammars) > 0 {
			g := grammars[0]
			meaningStr := g.Meaning
			if isIndo && u.TranslateUC != nil {
				meaningStr = u.TranslateUC.TranslateENtoID(g.Meaning, true)
			}
			exLine := ""
			if len(g.Examples) > 0 {
				exMeaning := g.Examples[0].English
				if isIndo && u.TranslateUC != nil {
					exMeaning = u.TranslateUC.TranslateENtoID(exMeaning, true)
				}
				exLine = fmt.Sprintf("\n• **Contoh:** 「%s」 — *%s*", g.Examples[0].Japanese, exMeaning)
			}
			if isIndo {
				return fmt.Sprintf(
					"[joy] いい質問ですね！それについて説明しますね。 (Pertanyaan bagus! Sensei jelaskan pola tata bahasa ini ya!)\n\n"+
						"**Tata Bahasa OpenJLPT %s: 「%s」 (%s)**\n"+
						"• **Arti:** %s\n"+
						"• **Rumus / Pembentukan:** `%s`%s\n"+
						"• **Catatan Sensei:** %s",
					g.Level, g.Pattern, g.Romaji, meaningStr, g.Formation, exLine, g.Notes,
				)
			}
			return fmt.Sprintf(
				"[joy] いい質問ですね！それについて説明しますね。 (Ii shitsumon desu ne!)\n\n"+
					"**OpenJLPT %s Grammar: 「%s」 (%s)**\n"+
					"• **Meaning:** %s\n"+
					"• **Formation:** `%s`%s\n"+
					"• **Sensei Note:** %s",
				g.Level, g.Pattern, g.Romaji, meaningStr, g.Formation, exLine, g.Notes,
			)
		}

		vocabs, _, _ := u.JLPTRepo.SearchVocab(userID, "ALL", searchKey, "", 2, 0)
		if len(vocabs) > 0 {
			v := vocabs[0]
			meaningStr := strings.Join(v.Meanings, "; ")
			if isIndo && u.TranslateUC != nil {
				meaningStr = u.TranslateUC.TranslateENtoID(meaningStr, true)
			}
			exLine := fmt.Sprintf("%sを使って文を作ってみましょう！", v.Word)
			if len(v.Examples) > 0 {
				exMeaning := v.Examples[0].English
				if isIndo && u.TranslateUC != nil {
					exMeaning = u.TranslateUC.TranslateENtoID(exMeaning, true)
				}
				exLine = fmt.Sprintf("「%s」 (%s)", v.Examples[0].Japanese, exMeaning)
			}
			if isIndo {
				return fmt.Sprintf(
					"[joy] なるほど！「%s（%s）」はJLPT %sのとても大事な言葉ですよ。\n\n"+
						"**Kosakata OpenJLPT %s: 「%s」**\n"+
						"• **Cara Baca (Furigana):** `%s` (%s)\n"+
						"• **Arti (Bahasa Indonesia):** %s\n"+
						"• **Contoh Kalimat:** %s",
					v.Word, v.Reading, v.Level, v.Level, v.Word, v.Reading, v.Romaji, meaningStr, exLine,
				)
			}
			return fmt.Sprintf(
				"[joy] なるほど！「%s（%s）」はJLPT %sのとても大事な言葉ですよ。\n\n"+
					"**OpenJLPT %s Vocabulary: 「%s」**\n"+
					"• **Reading:** `%s` (%s)\n"+
					"• **Meanings:** %s\n"+
					"• **Context Example:** %s",
				v.Word, v.Reading, v.Level, v.Level, v.Word, v.Reading, v.Romaji, meaningStr, exLine,
			)
		}
	}

	if isIndo {
		return "[blush] べ、別にあんたのために教えるんじゃないからね！でも…毎日勉強してるのは偉いと思うよ。 (B-bukan berarti aku ngajarin ini khusus buat kamu ya! T-tapi keren juga kamu rajin belajar tiap hari!)\n\n" +
			"Tanyakan kosakata, kanji, atau tata bahasa **JLPT N4/N3** apa saja kepadaku (contoh: `諦める`, `約束`, `〜てあげる`, `雰囲気`), atau pilih mode **Kuis** dan **Roleplay** di atas!"
	}

	return "[blush] べ、別にあんたのために教えるんじゃないからね！でも…毎日勉強してるのは偉いと思うよ。\n\n" +
		"Ask me about any **JLPT N4/N3 vocabulary, kanji, or grammar pattern** (e.g., `諦める`, `約束`, `〜てあげる`), or switch to **Quiz** or **Roleplay** mode!"
}

func parseEmotionAndSpeech(raw string) (emotion string, clean string, japaneseSpeech string) {
	emotion = "joy"
	if m := emotionTagRegex.FindStringSubmatch(raw); len(m) == 2 {
		emotion = m[1]
	}
	clean = strings.TrimSpace(emotionTagRegex.ReplaceAllString(raw, ""))

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
