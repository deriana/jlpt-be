package usecase

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/repository/sqlite"
)

type MiningUsecase struct {
	MiningRepo *sqlite.MiningRepository
	SRSRepo    *sqlite.SRSRepository
	JLPTRepo   *sqlite.JLPTRepository
}

func NewMiningUsecase(miningRepo *sqlite.MiningRepository, srsRepo *sqlite.SRSRepository, jlptRepo *sqlite.JLPTRepository) *MiningUsecase {
	return &MiningUsecase{
		MiningRepo: miningRepo,
		SRSRepo:    srsRepo,
		JLPTRepo:   jlptRepo,
	}
}

// MatchSentenceVocab runs longest-match Japanese tokenization against the SQLite OpenJLPT index,
// automatically highlighting and defining JLPT N4 and N3 (plus N5/N2/N1) vocabulary in the sentence.
func (u *MiningUsecase) MatchSentenceVocab(sentence string) ([]domain.MatchedVocabToken, int, float64) {
	idx, err := u.JLPTRepo.GetVocabMatcherIndex()
	if err != nil || len(idx) == 0 {
		return []domain.MatchedVocabToken{}, 0, 0
	}

	runes := []rune(sentence)
	n := len(runes)
	var matches []domain.MatchedVocabToken
	seenWords := make(map[string]bool)
	matchedRunes := 0
	totalContentRunes := 0

	for _, r := range runes {
		if !isPunctuationOrSpace(r) {
			totalContentRunes++
		}
	}

	i := 0
	for i < n {
		if isPunctuationOrSpace(runes[i]) {
			i++
			continue
		}

		matched := false
		maxLen := 8
		if i+maxLen > n {
			maxLen = n - i
		}

		for l := maxLen; l >= 1; l-- {
			sub := string(runes[i : i+l])
			if v, ok := idx[sub]; ok {
				if !seenWords[v.Word] {
					seenWords[v.Word] = true
					matches = append(matches, domain.MatchedVocabToken{
						VocabID:  v.ID,
						Surface:  sub,
						Word:     v.Word,
						Reading:  v.Reading,
						Romaji:   v.Romaji,
						Meanings: v.Meanings,
						Level:    v.Level,
						POS:      v.POS,
						StartIdx: i,
						EndIdx:   i + l,
					})
				}
				matchedRunes += l
				i += l
				matched = true
				break
			}
		}
		if !matched {
			i++
		}
	}

	n4n3Count := 0
	for _, m := range matches {
		if m.Level == "N4" || m.Level == "N3" {
			n4n3Count++
		}
	}

	coverage := 0.0
	if totalContentRunes > 0 {
		coverage = (float64(matchedRunes) / float64(totalContentRunes)) * 100.0
		if coverage > 100.0 {
			coverage = 100.0
		}
	}
	if matches == nil {
		matches = []domain.MatchedVocabToken{}
	}
	return matches, n4n3Count, coverage
}

var (
	srtTimeRegex = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,\.](\d{2,3})\s*-->\s*(\d{1,2}):(\d{2}):(\d{2})[,\.](\d{2,3})`)
	lrcTimeRegex = regexp.MustCompile(`\[(\d{1,2}):(\d{2})(?:[\.:](\d{2,3}))?\](.*)`)
)

// ParseMediaText parses .srt anime subtitles, .lrc Japanese song lyrics, or plain text lines,
// extracts timestamps, and runs Automated OpenJLPT Vocabulary Matching on every line.
func (u *MiningUsecase) ParseMediaText(rawContent string, format string) []domain.MediaCue {
	trimmed := strings.TrimSpace(rawContent)
	if trimmed == "" {
		return []domain.MediaCue{}
	}

	// Auto-detect format if needed
	lowerFmt := strings.ToLower(format)
	if lowerFmt == "" || lowerFmt == "auto" {
		if srtTimeRegex.MatchString(trimmed) {
			lowerFmt = "srt"
		} else if lrcTimeRegex.MatchString(trimmed) {
			lowerFmt = "lrc"
		} else {
			lowerFmt = "plain"
		}
	}

	var cues []domain.MediaCue
	switch lowerFmt {
	case "srt":
		blocks := regexp.MustCompile(`\r?\n\r?\n+`).Split(trimmed, -1)
		idx := 1
		for _, block := range blocks {
			lines := strings.Split(strings.TrimSpace(block), "\n")
			var startMs, endMs int64
			var textLines []string
			foundTime := false
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if m := srtTimeRegex.FindStringSubmatch(line); len(m) == 9 {
					startMs = hmsToMs(m[1], m[2], m[3], m[4])
					endMs = hmsToMs(m[5], m[6], m[7], m[8])
					foundTime = true
					continue
				}
				if !foundTime {
					if _, err := strconv.Atoi(line); err == nil {
						continue
					}
				}
				line = StripSubtitleMarkup(line)
				if line != "" {
					textLines = append(textLines, line)
				}
			}
			if len(textLines) > 0 {
				var jaText, enText string
				if len(textLines) > 1 && ContainsJapaneseScript(textLines[0]) && !ContainsJapaneseScript(textLines[1]) {
					// Bilingual SRT block: Line 1 is Japanese, Line 2+ is Translation
					jaText = textLines[0]
					enText = strings.Join(textLines[1:], " ")
				} else {
					// Monolingual multi-line SRT block (e.g. "Waduh, kayaknya\nponselku basah!"): join all lines!
					jaText = strings.Join(textLines, " ")
				}
				if !foundTime {
					startMs = int64((idx - 1) * 4000)
					endMs = startMs + 3800
				}
				matched, n4n3, cov := u.MatchSentenceVocab(jaText)
				cues = append(cues, domain.MediaCue{
					Index:          idx,
					StartTimeMs:    startMs,
					EndTimeMs:      endMs,
					TimestampLabel: fmt.Sprintf("%s - %s", msToClock(startMs), msToClock(endMs)),
					Sentence:       jaText,
					Translation:    enText,
					MatchedVocab:   matched,
					N4N3Count:      n4n3,
					CoverageRate:   cov,
				})
				idx++
			}
		}

	case "lrc":
		lines := strings.Split(trimmed, "\n")
		idx := 1
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if m := lrcTimeRegex.FindStringSubmatch(line); len(m) == 5 {
				min, _ := strconv.ParseInt(m[1], 10, 64)
				sec, _ := strconv.ParseInt(m[2], 10, 64)
				fracStr := m[3]
				var ms int64
				if len(fracStr) == 2 {
					v, _ := strconv.ParseInt(fracStr, 10, 64)
					ms = v * 10
				} else if len(fracStr) == 3 {
					ms, _ = strconv.ParseInt(fracStr, 10, 64)
				}
				startMs := (min*60+sec)*1000 + ms
				textPart := strings.TrimSpace(m[4])
				if textPart == "" {
					continue
				}
				jaText := textPart
				enText := ""
				if parts := strings.SplitN(textPart, " // ", 2); len(parts) == 2 {
					jaText = strings.TrimSpace(parts[0])
					enText = strings.TrimSpace(parts[1])
				}
				matched, n4n3, cov := u.MatchSentenceVocab(jaText)
				cues = append(cues, domain.MediaCue{
					Index:          idx,
					StartTimeMs:    startMs,
					EndTimeMs:      startMs + 4500,
					TimestampLabel: fmt.Sprintf("%s - %s", msToClock(startMs), msToClock(startMs+4500)),
					Sentence:       jaText,
					Translation:    enText,
					MatchedVocab:   matched,
					N4N3Count:      n4n3,
					CoverageRate:   cov,
				})
				idx++
			}
		}
		// Adjust end times based on next lyric cue
		for i := 0; i < len(cues)-1; i++ {
			if cues[i+1].StartTimeMs > cues[i].StartTimeMs {
				cues[i].EndTimeMs = cues[i+1].StartTimeMs
				cues[i].TimestampLabel = fmt.Sprintf("%s - %s", msToClock(cues[i].StartTimeMs), msToClock(cues[i].EndTimeMs))
			}
		}

	default:
		lines := strings.Split(trimmed, "\n")
		idx := 1
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			startMs := int64((idx - 1) * 4500)
			endMs := startMs + 4200
			matched, n4n3, cov := u.MatchSentenceVocab(line)
			cues = append(cues, domain.MediaCue{
				Index:          idx,
				StartTimeMs:    startMs,
				EndTimeMs:      endMs,
				TimestampLabel: fmt.Sprintf("%s - %s", msToClock(startMs), msToClock(endMs)),
				Sentence:       line,
				MatchedVocab:   matched,
				N4N3Count:      n4n3,
				CoverageRate:   cov,
			})
			idx++
		}
	}

	if cues == nil {
		cues = []domain.MediaCue{}
	}
	return cues
}

// GetMediaPresets returns pre-loaded mock Anime (.srt) and J-Pop Music (.lrc) scripts
// with real OpenJLPT vocabulary matching so learners can test sentence mining immediately offline.
func (u *MiningUsecase) GetMediaPresets() []domain.MediaPreset {
	presets := []domain.MediaPreset{
		{
			ID:          "frieren-ep01",
			Title:       "葬送のフリーレン (Sousou no Frieren)",
			Episode:     "Episode 01 - The Journey's End (.srt)",
			MediaType:   "anime",
			Format:      "srt",
			CoverAccent: "from-emerald-500/20 to-teal-500/10",
			RawContent: `1
00:00:14,200 --> 00:00:19,800
人間の寿命は短いってわかっていたのに、どうして私はもっと知ろうとしなかったんだろう。
Even though I knew human lifespans are short, why didn't I try to get to know him better?

2
00:00:21,000 --> 00:00:26,400
これから私は、人間をもっと知るための旅に出るよ。
From now on, I am setting out on a journey to understand humans better.

3
00:00:28,100 --> 00:00:33,500
魔法をّ集めるのが私の趣味だから、少し遠回りしても大丈夫だよ。
Collecting magic is my hobby, so it's alright even if we take a small detour.

4
00:00:35,000 --> 00:00:40,200
昔の約束を覚えているうちに、もう一度あの場所へ戻ろう。
While I still remember our old promise, let's return to that place once more.`,
		},
		{
			ID:          "bocchi-ep08",
			Title:       "ぼっち・ざ・ろっく！ (Bocchi the Rock!)",
			Episode:     "Episode 08 - Starry Live House (.srt)",
			MediaType:   "anime",
			Format:      "srt",
			CoverAccent: "from-pink-500/20 to-purple-500/10",
			RawContent: `1
00:01:12,000 --> 00:01:16,500
今日のライブは台風の影響でお客さんが少ないかもしれない。
Today's live show might have fewer guests because of the typhoon's influence.

2
00:01:18,200 --> 00:01:23,400
でも諦めなければ、きっと私たちの音楽がみんなに届くはずだよ！
But if we don't give up, our music will surely reach everyone!

3
00:01:25,000 --> 00:01:30,100
毎日一生懸命練習した経験は絶対に嘘をつかないから。
Because the experience of practicing with all our might every day never lies.`,
		},
		{
			ID:          "yoasobi-yoru",
			Title:       "YOASOBI - 夜に駆ける (Yoru ni Kakeru)",
			Episode:     "Official Synced Japanese Lyrics (.lrc)",
			MediaType:   "music",
			Format:      "lrc",
			CoverAccent: "from-indigo-500/20 to-rose-500/10",
			RawContent: `[00:08.50]沈むように溶けてゆくように、二人だけの空が広がる夜に。 // Like sinking, like melting away, into the night where a sky just for the two of us spreads out.
[00:15.80]「さよなら」だけだった、その一言で全てが分かった。 // It was just "goodbye", with that single word I understood everything.
[00:23.40]日が沈み出した空と君の姿、フェンス越しに重なっていた。 // The sky where the sun began to set and your figure overlapped beyond the fence.
[00:31.00]初めて会った日から、僕の心の全てを奪った。 // From the day we first met, you stole all of my heart.`,
		},
		{
			ID:          "kimi-no-nawa",
			Title:       "君の名は。 (Kimi no Na wa / Your Name)",
			Episode:     "Twilight Kataware-doki Scene (.srt)",
			MediaType:   "anime",
			Format:      "srt",
			CoverAccent: "from-sky-500/20 to-amber-500/10",
			RawContent: `1
01:18:15,000 --> 01:18:21,200
目が覚めても忘れないように、お互いの名前を書こう。
So we don't forget even after waking up, let's write each other's names.

2
01:18:22,000 --> 01:18:27,400
大事なのは忘れないこと。君の名前を絶対に忘れたくない！
What's important is not forgetting. I never want to forget your name!`,
		},
	}

	for i := range presets {
		presets[i].Cues = u.ParseMediaText(presets[i].RawContent, presets[i].Format)
	}
	return presets
}

func (u *MiningUsecase) ListMinedSentences(userID int64) ([]domain.MinedSentence, error) {
	return u.MiningRepo.ListMinedSentences(userID)
}

// SaveMinedSentenceToDeck saves a mined sentence with exact timestamps AND creates an SRS Flashcard
// linked to that timestamp for one-click audio playback during reviews.
func (u *MiningUsecase) SaveMinedSentenceToDeck(userID int64, req *domain.MinedSentence, targetWord string, deckID int64) (*domain.MinedSentence, *domain.Flashcard, error) {
	req.UserID = userID
	matched, _, _ := u.MatchSentenceVocab(req.Sentence)
	if len(req.MatchedVocab) == 0 {
		req.MatchedVocab = matched
	}
	if req.TimestampLabel == "" {
		req.TimestampLabel = fmt.Sprintf("%s - %s", msToClock(req.StartTimeMs), msToClock(req.EndTimeMs))
	}

	// Pick target vocabulary token from sentence (N4/N3 prioritized)
	var chosen domain.MatchedVocabToken
	found := false
	for _, tok := range req.MatchedVocab {
		if targetWord != "" && (tok.Word == targetWord || tok.Surface == targetWord) {
			chosen = tok
			found = true
			break
		}
	}
	if !found {
		for _, tok := range req.MatchedVocab {
			if tok.Level == "N4" || tok.Level == "N3" {
				chosen = tok
				found = true
				break
			}
		}
	}
	if !found && len(req.MatchedVocab) > 0 {
		chosen = req.MatchedVocab[0]
		found = true
	}
	if !found {
		chosen = domain.MatchedVocabToken{
			Word:     req.Sentence,
			Reading:  req.Reading,
			Meanings: []string{req.Translation},
			Level:    "N4",
		}
	}

	req.TargetLevel = chosen.Level
	if deckID == 0 {
		decks, _ := u.SRSRepo.ListDecks(userID)
		for _, d := range decks {
			if d.Category == "media-mining" {
				deckID = d.ID
				break
			}
		}
		if deckID == 0 && len(decks) > 0 {
			deckID = decks[0].ID
		}
	}
	req.SavedToDeckID = deckID

	if err := u.MiningRepo.CreateMinedSentence(req); err != nil {
		return nil, nil, err
	}

	card := &domain.Flashcard{
		DeckID:          deckID,
		UserID:          userID,
		FrontKanji:      chosen.Word,
		FuriganaReading: chosen.Reading,
		Romaji:          chosen.Romaji,
		BackMeaning:     strings.Join(chosen.Meanings, "; "),
		JLPTLevel:       chosen.Level,
		POS:             strings.Join(chosen.POS, ", "),
		ContextSentence: req.Sentence,
		ContextReading:  req.Reading,
		ContextMeaning:  req.Translation,
		MediaSourceID:   req.ID,
		MediaTitle:      req.MediaTitle,
		MediaType:       req.MediaType,
		MediaAudioURL:   req.AudioURL,
		StartTimeMs:     req.StartTimeMs,
		EndTimeMs:       req.EndTimeMs,
		TimestampLabel:  req.TimestampLabel,
		EaseFactor:      2.5,
	}
	if card.BackMeaning == "" {
		card.BackMeaning = req.Translation
	}
	_ = u.SRSRepo.CreateCard(card)

	return req, card, nil
}

type VideoExtractionResult struct {
	MediaFilename    string            `json:"media_filename"`
	MediaTitle       string            `json:"media_title"`
	MediaURL         string            `json:"media_url"`
	ExtractionMethod string            `json:"extraction_method"`
	SRTContent       string            `json:"srt_content"`
	Cues             []domain.MediaCue `json:"cues"`
}

// ExtractVideoToTimestampCues extracts per-sentence timestamps and subtitles from an uploaded video/audio file:
// 1. Tries extracting an embedded subtitle track (.mkv/.mp4 softsubs) via ffmpeg (<1s).
// 2. If no embedded subtitle exists, extracts 16kHz mono WAV via ffmpeg and transcribes Japanese sentences
//    with exact timestamps using local offline Faster-Whisper (/home/hideri/coding/Open-LLM-VTuber/models/whisper_base).
// 3. Attaches a direct original-video audio slice URL (ClipAudioURL) to every sentence cue so study playback uses the ORIGINAL video voice!
func (u *MiningUsecase) ExtractVideoToTimestampCues(savedVideoPath string, mediaFilename string, title string) (*VideoExtractionResult, error) {
	uploadsDir := filepath.Dir(savedVideoPath)
	_ = os.MkdirAll(filepath.Join(uploadsDir, "clips"), 0755)

	tmpSrt := filepath.Join(uploadsDir, mediaFilename+".extracted.srt")
	defer os.Remove(tmpSrt)

	var srtContent string
	extractionMethod := "embedded-subtitle-stream"

	// 1. Check for embedded soft-subtitle track in video container (.mkv / .mp4)
	cmdSub := exec.Command("ffmpeg", "-y", "-i", savedVideoPath, "-map", "0:s:0", "-f", "srt", tmpSrt)
	if err := cmdSub.Run(); err == nil {
		if b, err := os.ReadFile(tmpSrt); err == nil && len(strings.TrimSpace(string(b))) > 10 {
			srtContent = string(b)
		}
	}

	// 2. If no embedded subtitle track, run local Faster-Whisper ASR on the video's original audio track
	if strings.TrimSpace(srtContent) == "" {
		extractionMethod = "faster-whisper-offline-asr"
		tmpWav := filepath.Join(uploadsDir, mediaFilename+".16k.wav")
		defer os.Remove(tmpWav)

		cmdAudio := exec.Command("ffmpeg", "-y", "-i", savedVideoPath, "-vn", "-ac", "1", "-ar", "16000", "-f", "wav", tmpWav)
		if out, err := cmdAudio.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to extract audio track from video via ffmpeg: %v (%s)", err, string(out))
		}

		pythonBin := "/home/hideri/coding/Open-LLM-VTuber/.venv/bin/python3"
		if _, err := os.Stat(pythonBin); err != nil {
			pythonBin = "python3"
		}
		scriptPath, _ := filepath.Abs("./scripts/whisper_extract.py")
		cmdWhisper := exec.Command(pythonBin, scriptPath, "--input", tmpWav, "--lang", "ja")
		wOut, err := cmdWhisper.Output()
		if err != nil {
			return nil, fmt.Errorf("faster-whisper transcription failed: %w", err)
		}

		var whisperRes struct {
			SRT   string `json:"srt"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(wOut, &whisperRes); err == nil && whisperRes.SRT != "" {
			srtContent = whisperRes.SRT
		} else if whisperRes.Error != "" {
			return nil, fmt.Errorf("whisper error: %s", whisperRes.Error)
		}
	}

	cues := u.ParseMediaText(srtContent, "srt")
	for i := range cues {
		cues[i].ClipAudioURL = fmt.Sprintf(
			"/api/v1/mining/clip?media=%s&start_ms=%d&end_ms=%d",
			url.QueryEscape(mediaFilename),
			cues[i].StartTimeMs,
			cues[i].EndTimeMs,
		)
	}

	if title == "" {
		title = mediaFilename
	}

	return &VideoExtractionResult{
		MediaFilename:    mediaFilename,
		MediaTitle:       title,
		MediaURL:         "/api/v1/mining/media/" + mediaFilename,
		ExtractionMethod: extractionMethod,
		SRTContent:       srtContent,
		Cues:             cues,
	}, nil
}

// SliceOriginalAudioClip uses FFmpeg to cut the exact [startMs, endMs] audio segment from the uploaded video
// so that flashcards and sentence reviews play the 100% ORIGINAL voice from the video (not VOICEVOX).
func (u *MiningUsecase) SliceOriginalAudioClip(uploadsDir string, mediaFilename string, startMs int64, endMs int64) (string, string, error) {
	cleanMedia := filepath.Base(mediaFilename)
	sourceVideoPath := filepath.Join(uploadsDir, cleanMedia)
	if _, err := os.Stat(sourceVideoPath); err != nil {
		return "", "", fmt.Errorf("source media file not found: %s", cleanMedia)
	}

	if endMs <= startMs {
		endMs = startMs + 3000
	}
	// Add 120ms padding before/after so consonant starts/ends aren't clipped too tightly
	padStartMs := startMs - 100
	if padStartMs < 0 {
		padStartMs = 0
	}
	padEndMs := endMs + 180

	hashInput := fmt.Sprintf("%s|%d|%d", cleanMedia, padStartMs, padEndMs)
	h := sha1.Sum([]byte(hashInput))
	clipFilename := fmt.Sprintf("clip_%s_%d_%d.wav", hex.EncodeToString(h[:])[:10], startMs, endMs)
	clipsDir := filepath.Join(uploadsDir, "clips")
	_ = os.MkdirAll(clipsDir, 0755)
	clipFullPath := filepath.Join(clipsDir, clipFilename)
	publicURL := "/api/v1/mining/media/clips/" + clipFilename

	if info, err := os.Stat(clipFullPath); err == nil && info.Size() > 44 {
		return clipFullPath, publicURL, nil
	}

	startSec := fmt.Sprintf("%.3f", float64(padStartMs)/1000.0)
	durationSec := fmt.Sprintf("%.3f", float64(padEndMs-padStartMs)/1000.0)
	audioMap, _, _ := u.DetectBestMediaStreams(context.Background(), sourceVideoPath)

	cmd := exec.Command(
		"ffmpeg", "-y",
		"-ss", startSec,
		"-i", sourceVideoPath,
		"-t", durationSec,
		"-map", audioMap,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "24000",
		"-ac", "1",
		clipFullPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("ffmpeg clip slice failed: %v (%s)", err, string(out))
	}

	return clipFullPath, publicURL, nil
}

func hmsToMs(hStr, mStr, sStr, msStr string) int64 {
	h, _ := strconv.ParseInt(hStr, 10, 64)
	m, _ := strconv.ParseInt(mStr, 10, 64)
	s, _ := strconv.ParseInt(sStr, 10, 64)
	if len(msStr) == 2 {
		msStr += "0"
	}
	ms, _ := strconv.ParseInt(msStr, 10, 64)
	return ((h*3600 + m*60 + s) * 1000) + ms
}

func (u *MiningUsecase) FormatTimestampRange(startMs, endMs int64) string {
	return fmt.Sprintf("%s - %s", msToClock(startMs), msToClock(endMs))
}

func ContainsJapaneseScript(s string) bool {
	for _, r := range s {
		if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) || (r >= 0x4E00 && r <= 0x9FFF) {
			return true
		}
	}
	return false
}

// DetectBestMediaStreams inspects an MKV/MP4 container via ffprobe to handle:
// - Dual-Audio releases (selecting the Japanese audio stream over English dubs)
// - Multi-Sub releases (selecting Japanese softsubs and/or Indonesian softsubs across any fansub)
func (u *MiningUsecase) DetectBestMediaStreams(ctx context.Context, videoPath string) (audioMap string, jpSubMap string, idSubMap string) {
	audioMap = "0:a:0"
	cmd := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "stream=index,codec_type,codec_name:stream_tags=language,title",
		"-of", "json",
		videoPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return audioMap, "", "0:s:0"
	}

	var probe struct {
		Streams []struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Tags      struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return audioMap, "", "0:s:0"
	}

	firstAudio := ""
	firstTextSub := ""

	for _, st := range probe.Streams {
		lang := strings.ToLower(strings.TrimSpace(st.Tags.Language))
		title := strings.ToLower(strings.TrimSpace(st.Tags.Title))

		switch st.CodecType {
		case "audio":
			mapSpec := fmt.Sprintf("0:%d", st.Index)
			if firstAudio == "" {
				firstAudio = mapSpec
			}
			if lang == "jpn" || lang == "ja" || strings.Contains(title, "jap") || strings.Contains(title, "jp") {
				audioMap = mapSpec
			}

		case "subtitle":
			// Skip bitmap subtitles (PGS/VobSub) that cannot be converted to ASS/SRT text
			if st.CodecName == "hdmv_pgs_subtitle" || st.CodecName == "dvd_subtitle" || st.CodecName == "dvb_subtitle" {
				continue
			}
			mapSpec := fmt.Sprintf("0:%d", st.Index)
			if firstTextSub == "" {
				firstTextSub = mapSpec
			}
			if lang == "jpn" || lang == "ja" || strings.Contains(title, "jap") || strings.Contains(title, "jp") {
				if jpSubMap == "" {
					jpSubMap = mapSpec
				}
			} else if lang == "ind" || lang == "id" || lang == "may" || lang == "msa" || strings.Contains(title, "indo") || strings.Contains(title, "id") {
				if idSubMap == "" {
					idSubMap = mapSpec
				}
			}
		}
	}

	if audioMap == "0:a:0" && firstAudio != "" {
		audioMap = firstAudio
	}
	if idSubMap == "" && firstTextSub != "" && firstTextSub != jpSubMap {
		idSubMap = firstTextSub
	}
	return audioMap, jpSubMap, idSubMap
}

var (
	assTagBlockRegex = regexp.MustCompile(`\{[^}]*\}`)
	assTimeRegex     = regexp.MustCompile(`^(\d+):(\d{2}):(\d{2})\.(\d{2})$`)
)

// ExtractCleanDialogueSoftsubs extracts a subtitle track in native ASS format and filters out
// Aegisub Karaoke FX (OP/ED/Effector), Vector Drawings (\p1), and Screen Signs/Typesetting (\pos, \move, Style: sign/TS),
// while joining multi-line \N breaks with spaces so fansub sentences are never truncated or polluted.
func (u *MiningUsecase) ExtractCleanDialogueSoftsubs(
	ctx context.Context,
	videoPath string,
	subMap string,
	startSec int,
	durationSec int,
) []domain.MediaCue {
	if subMap == "" {
		return nil
	}
	tmpAss := fmt.Sprintf("%s.%s.%d_%d.tmp.ass", videoPath, strings.ReplaceAll(subMap, ":", "_"), startSec, durationSec)
	defer os.Remove(tmpAss)

	args := []string{"-y"}
	if startSec > 0 {
		args = append(args, "-ss", strconv.Itoa(startSec))
	}
	args = append(args, "-i", videoPath)
	if durationSec > 0 {
		args = append(args, "-t", strconv.Itoa(durationSec))
	}
	args = append(args, "-map", subMap, "-f", "ass", tmpAss)

	if err := exec.CommandContext(ctx, "ffmpeg", args...).Run(); err != nil {
		return nil
	}
	rawBytes, err := os.ReadFile(tmpAss)
	if err != nil || len(rawBytes) == 0 {
		return nil
	}

	offsetMs := int64(startSec * 1000)
	lines := strings.Split(string(rawBytes), "\n")
	var cues []domain.MediaCue
	idx := 1

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "Dialogue:"))
		// Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
		parts := strings.SplitN(rest, ",", 10)
		if len(parts) < 10 {
			continue
		}

		startStr := strings.TrimSpace(parts[1])
		endStr := strings.TrimSpace(parts[2])
		style := strings.ToLower(strings.TrimSpace(parts[3]))
		actorName := strings.ToLower(strings.TrimSpace(parts[4]))
		effect := strings.ToLower(strings.TrimSpace(parts[8]))
		rawText := parts[9]

		// 1. Filter out Karaoke FX, Opening/Ending lyrics, and Aegisub template effects
		if effect != "" && (strings.Contains(effect, "fx") || strings.Contains(effect, "kara") || strings.Contains(effect, "template") || strings.Contains(effect, "code")) {
			continue
		}
		if isNonDialogueAssStyle(style) || isNonDialogueAssStyle(actorName) {
			continue
		}

		// 2. Filter out vector drawings (\p1..\p9) and screen sign typesetting (\pos, \move, \org)
		lowerRaw := strings.ToLower(rawText)
		if strings.Contains(lowerRaw, `\p1`) || strings.Contains(lowerRaw, `\p2`) ||
			strings.Contains(lowerRaw, `\pos(`) || strings.Contains(lowerRaw, `\move(`) || strings.Contains(lowerRaw, `\org(`) {
			continue
		}

		// 3. Join Aegisub multi-line breaks (\N, \n, \h) with spaces BEFORE stripping tags
		cleanText := strings.ReplaceAll(rawText, `\N`, " ")
		cleanText = strings.ReplaceAll(cleanText, `\n`, " ")
		cleanText = strings.ReplaceAll(cleanText, `\h`, " ")
		cleanText = assTagBlockRegex.ReplaceAllString(cleanText, "")
		cleanText = StripSubtitleMarkup(cleanText)
		cleanText = strings.Join(strings.Fields(cleanText), " ")
		if cleanText == "" {
			continue
		}

		startMs := parseAssTimeMs(startStr) + offsetMs
		endMs := parseAssTimeMs(endStr) + offsetMs
		if endMs <= startMs {
			endMs = startMs + 1500
		}

		// 4. Deduplicate layered border/shadow duplicate dialogue lines at identical timestamps
		if len(cues) > 0 {
			last := &cues[len(cues)-1]
			if last.Sentence == cleanText && absDiffInt64(last.StartTimeMs, startMs) <= 200 {
				continue
			}
		}

		cues = append(cues, domain.MediaCue{
			Index:          idx,
			StartTimeMs:    startMs,
			EndTimeMs:      endMs,
			TimestampLabel: fmt.Sprintf("%s - %s", msToClock(startMs), msToClock(endMs)),
			Sentence:       cleanText,
		})
		idx++
	}

	return cues
}

func isNonDialogueAssStyle(style string) bool {
	if style == "" {
		return false
	}
	nonDialogueTokens := []string{
		"op-", "ed-", "op_", "ed_", "opening", "ending", "kara",
		"sign", "ts", "typeset", "title", "título", "judul",
		"credit", "logo", "eyecatch", "next", "preview", "song", "lyric",
	}
	for _, tok := range nonDialogueTokens {
		if style == tok || strings.HasPrefix(style, tok) || strings.Contains(style, tok) {
			return true
		}
	}
	return false
}

func parseAssTimeMs(t string) int64 {
	m := assTimeRegex.FindStringSubmatch(strings.TrimSpace(t))
	if len(m) != 5 {
		return 0
	}
	h, _ := strconv.ParseInt(m[1], 10, 64)
	min, _ := strconv.ParseInt(m[2], 10, 64)
	sec, _ := strconv.ParseInt(m[3], 10, 64)
	cs, _ := strconv.ParseInt(m[4], 10, 64)
	return ((h*3600 + min*60 + sec) * 1000) + (cs * 10)
}

func absDiffInt64(a, b int64) int64 {
	if a < b {
		return b - a
	}
	return a - b
}

func msToClock(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSecs := ms / 1000
	remMs := (ms % 1000) / 10
	mins := totalSecs / 60
	secs := totalSecs % 60
	return fmt.Sprintf("%02d:%02d.%02d", mins, secs, remMs)
}

func isPunctuationOrSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '。', '、', '！', '？', '「', '」', '『', '』', '（', '）', '!', '?', ',', '.', '・', '…', '〜':
		return true
	}
	return false
}
