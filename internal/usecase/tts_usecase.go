package usecase

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/domain"
)

type TTSUsecase struct {
	Cfg            *config.AppConfig
	mu             sync.RWMutex
	cachedSpeakers []domain.VoicevoxSpeaker
}

var defaultVoicevoxSpeakers = []domain.VoicevoxSpeaker{
	{ID: "3", Name: "ずんだもん (Zundamon) - ノーマル (Normal)", Character: "ずんだもん", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "1", Name: "ずんだもん (Zundamon) - あまあま (Sweet)", Character: "ずんだもん", Style: "あまあま", Lang: "ja-JP"},
	{ID: "7", Name: "ずんだもん (Zundamon) - ツンツン (Tsundere)", Character: "ずんだもん", Style: "ツンツン", Lang: "ja-JP"},
	{ID: "5", Name: "ずんだもん (Zundamon) - セクシー (Sexy)", Character: "ずんだもん", Style: "セクシー", Lang: "ja-JP"},
	{ID: "2", Name: "四国めたん (Shikoku Metan) - ノーマル (Normal)", Character: "四国めたん", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "0", Name: "四国めたん (Shikoku Metan) - あまあま (Sweet)", Character: "四国めたん", Style: "あまあま", Lang: "ja-JP"},
	{ID: "6", Name: "四国めたん (Shikoku Metan) - ツンツン (Tsundere)", Character: "四国めたん", Style: "ツンツン", Lang: "ja-JP"},
	{ID: "8", Name: "春日部つむぎ (Kasukabe Tsumugi) - ノーマル (Normal)", Character: "春日部つむぎ", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "14", Name: "冥鳴ひまり (Meimei Himari) - ノーマル (Normal)", Character: "冥鳴ひまり", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "10", Name: "雨晴はう (Amehare Hau) - ノーマル (Normal)", Character: "雨晴はう", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "11", Name: "玄野武宏 (Kurono Takehiro) - ノーマル (Normal)", Character: "玄野武宏", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "20", Name: "もち子さん (Mochiko-san) - ノーマル (Normal)", Character: "もち子さん", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "23", Name: "WhiteCUL - ノーマル (Normal)", Character: "WhiteCUL", Style: "ノーマル", Lang: "ja-JP"},
	{ID: "47", Name: "ナースロボ＿タイプＴ (Nurseall Type-T) - ノーマル", Character: "ナースロボ＿タイプＴ", Style: "ノーマル", Lang: "ja-JP"},
}

func NewTTSUsecase(cfg *config.AppConfig) *TTSUsecase {
	return &TTSUsecase{
		Cfg:            cfg,
		cachedSpeakers: defaultVoicevoxSpeakers,
	}
}

// GetStatus resolves the Windows Host VOICEVOX URL from WSL2, checks engine reachability,
// and fetches the live speaker list if VOICEVOX is running on Windows.
func (u *TTSUsecase) GetStatus() *domain.TTSStatus {
	resolvedURL, online, hostIP := config.ResolveServiceURL(u.Cfg.TTS.VoicevoxURL, "version", 900*time.Millisecond)
	voices := u.getVoices(resolvedURL, online)

	return &domain.TTSStatus{
		Provider:        "voicevox-windows",
		ConfiguredURL:   u.Cfg.TTS.VoicevoxURL,
		ResolvedURL:     resolvedURL,
		WindowsHostIP:   hostIP,
		EngineOnline:    online,
		ActiveSpeakerID: u.Cfg.TTS.DefaultSpeaker,
		FallbackMode:    "offline-japanese-acoustic-wav + web-speech-api",
		Voices:          voices,
	}
}

func (u *TTSUsecase) getVoices(resolvedURL string, online bool) []domain.VoicevoxSpeaker {
	if online {
		client := &http.Client{Timeout: 2500 * time.Millisecond}
		if resp, err := client.Get(resolvedURL + "/speakers"); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var rawSpeakers []struct {
					Name   string `json:"name"`
					Styles []struct {
						ID   int    `json:"id"`
						Name string `json:"name"`
					} `json:"styles"`
				}
				if json.NewDecoder(resp.Body).Decode(&rawSpeakers) == nil && len(rawSpeakers) > 0 {
					var list []domain.VoicevoxSpeaker
					for _, sp := range rawSpeakers {
						for _, st := range sp.Styles {
							list = append(list, domain.VoicevoxSpeaker{
								ID:        fmt.Sprintf("%d", st.ID),
								Name:      fmt.Sprintf("%s - %s", sp.Name, st.Name),
								Character: sp.Name,
								Style:     st.Name,
								Lang:      "ja-JP",
							})
						}
					}
					if len(list) > 0 {
						u.mu.Lock()
						u.cachedSpeakers = list
						u.mu.Unlock()
						return list
					}
				}
			}
		}
	}
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.cachedSpeakers
}

func (u *TTSUsecase) SetDefaultVoice(speakerID string, voicevoxURL string, speed float64) error {
	return u.Cfg.Update(func(c *config.AppConfig) {
		if speakerID != "" {
			c.TTS.DefaultSpeaker = speakerID
		}
		if voicevoxURL != "" {
			c.TTS.VoicevoxURL = strings.TrimRight(strings.TrimSpace(voicevoxURL), "/")
		}
		if speed >= 0.5 && speed <= 2.0 {
			c.TTS.SpeedScale = speed
		}
	})
}

// Synthesize calls the Windows VOICEVOX Engine (/audio_query -> /synthesis) with caching.
// If Windows VOICEVOX is closed/offline, it synthesizes an offline Japanese mora-timed formant WAV
// so audio playback & Live2D lip-sync always work 100% offline with <50ms latency.
func (u *TTSUsecase) Synthesize(req domain.TTSSpeakRequest) (*domain.TTSSpeakResponse, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		text = "こんにちは"
	}
	speakerID := strings.TrimSpace(req.SpeakerID)
	if speakerID == "" {
		speakerID = u.Cfg.TTS.DefaultSpeaker
	}
	if speakerID == "" {
		speakerID = "3"
	}
	speed := req.Speed
	if speed <= 0 {
		speed = u.Cfg.TTS.SpeedScale
	}
	if speed <= 0 {
		speed = 1.0
	}

	resolvedURL, online, _ := config.ResolveServiceURL(u.Cfg.TTS.VoicevoxURL, "version", 750*time.Millisecond)
	engineTag := "offline-acoustic-synth"
	if online {
		engineTag = "voicevox-windows"
	}

	// Deterministic cache filename per (engine, speaker, speed, emotion, text)
	hashInput := fmt.Sprintf("%s|%s|%.2f|%s|%s", engineTag, speakerID, speed, req.Emotion, text)
	h := sha1.Sum([]byte(hashInput))
	fileName := fmt.Sprintf("tts_%s_%s_%s.wav", engineTag, speakerID, hex.EncodeToString(h[:])[:12])
	filePath := filepath.Join(u.Cfg.TTS.CacheDir, fileName)

	var wavBytes []byte
	if existing, err := os.ReadFile(filePath); err == nil && len(existing) > 44 {
		wavBytes = existing
	} else if online {
		if synthBytes, err := u.callWindowsVoicevox(resolvedURL, text, speakerID, speed, req.Emotion); err == nil && len(synthBytes) > 44 {
			wavBytes = synthBytes
			_ = os.WriteFile(filePath, wavBytes, 0644)
		}
	}

	// Offline Acoustic Japanese Formant WAV fallback if Windows VOICEVOX is not running
	if len(wavBytes) <= 44 {
		engineTag = "offline-acoustic-synth"
		wavBytes = generateOfflineJapaneseVoiceWAV(text, speakerID, speed)
		_ = os.WriteFile(filePath, wavBytes, 0644)
	}

	volumes, durationMs := computeWAVRMSVolumes(wavBytes, 20)

	return &domain.TTSSpeakResponse{
		AudioURL:   "/api/v1/tts/audio/" + fileName,
		SpeakerID:  speakerID,
		EngineUsed: engineTag,
		Volumes:    volumes,
		ChunkMs:    20,
		DurationMs: durationMs,
	}, nil
}

func (u *TTSUsecase) callWindowsVoicevox(baseURL, text, speakerID string, speed float64, emotion string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	qURL := fmt.Sprintf("%s/audio_query?text=%s&speaker=%s", baseURL, url.QueryEscape(text), url.QueryEscape(speakerID))
	qReq, err := http.NewRequest(http.MethodPost, qURL, nil)
	if err != nil {
		return nil, err
	}
	qResp, err := client.Do(qReq)
	if err != nil {
		return nil, err
	}
	defer qResp.Body.Close()
	if qResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voicevox audio_query status %d", qResp.StatusCode)
	}

	var queryMap map[string]interface{}
	if err := json.NewDecoder(qResp.Body).Decode(&queryMap); err != nil {
		return nil, err
	}
	queryMap["speedScale"] = speed

	switch strings.ToLower(emotion) {
	case "joy", "happy":
		queryMap["pitchScale"] = 0.04
		queryMap["intonationScale"] = 1.15
	case "blush", "surprise":
		queryMap["pitchScale"] = 0.05
		queryMap["intonationScale"] = 1.2
	case "sadness":
		queryMap["pitchScale"] = -0.04
		queryMap["speedScale"] = speed * 0.92
	case "anger":
		queryMap["pitchScale"] = 0.06
		queryMap["speedScale"] = speed * 1.08
	}

	queryJSON, _ := json.Marshal(queryMap)
	sURL := fmt.Sprintf("%s/synthesis?speaker=%s", baseURL, url.QueryEscape(speakerID))
	sReq, err := http.NewRequest(http.MethodPost, sURL, bytes.NewReader(queryJSON))
	if err != nil {
		return nil, err
	}
	sReq.Header.Set("Content-Type", "application/json")

	sResp, err := client.Do(sReq)
	if err != nil {
		return nil, err
	}
	defer sResp.Body.Close()
	if sResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voicevox synthesis status %d", sResp.StatusCode)
	}
	return io.ReadAll(sResp.Body)
}

// generateOfflineJapaneseVoiceWAV generates a pleasant, mora-syllable enveloped harmonic WAV file
// when Windows VOICEVOX is not reachable, ensuring audio playback and Live2D mouth lip-sync work 100% offline.
func generateOfflineJapaneseVoiceWAV(text string, speakerID string, speed float64) []byte {
	const sampleRate = 24000
	runes := []rune(text)
	moraCount := len(runes)
	if moraCount < 3 {
		moraCount = 3
	}
	if moraCount > 45 {
		moraCount = 45
	}

	basePitch := 245.0 // Female anime voice fundamental frequency (Hz)
	switch speakerID {
	case "3", "1", "7": // Zundamon (higher cute pitch)
		basePitch = 285.0
	case "2", "0", "6": // Shikoku Metan
		basePitch = 255.0
	case "8": // Kasukabe Tsumugi
		basePitch = 270.0
	case "14": // Meimei Himari
		basePitch = 235.0
	case "11": // Kurono Takehiro (Male voice)
		basePitch = 135.0
	}

	moraDurSec := 0.115 / speed
	totalSamples := int(float64(moraCount) * moraDurSec * float64(sampleRate))
	if totalSamples < sampleRate/2 {
		totalSamples = sampleRate / 2
	}

	pcm := make([]int16, totalSamples)
	samplesPerMora := totalSamples / moraCount

	for m := 0; m < moraCount; m++ {
		r := runes[m%len(runes)]
		if r == '、' || r == '。' || r == ' ' || r == '！' || r == '？' {
			continue
		}
		// Pitch accent contour (Japanese pitch rise after 1st mora, gentle fall toward end)
		pitchMult := 1.0
		if m == 0 {
			pitchMult = 0.94
		} else if m < 3 {
			pitchMult = 1.06
		} else {
			pitchMult = 1.04 - 0.14*(float64(m)/float64(moraCount))
		}
		f0 := basePitch * pitchMult
		// Formant harmonics based on rune hash to simulate distinct Japanese vowels (a/i/u/e/o)
		vowelSelector := int(r) % 5
		f1 := 520.0
		f2 := 1480.0
		switch vowelSelector {
		case 0: // 'a'
			f1, f2 = 750, 1250
		case 1: // 'i'
			f1, f2 = 320, 2250
		case 2: // 'u'
			f1, f2 = 360, 1150
		case 3: // 'e'
			f1, f2 = 510, 1850
		case 4: // 'o'
			f1, f2 = 480, 920
		}

		startSample := m * samplesPerMora
		endSample := startSample + samplesPerMora
		if endSample > totalSamples {
			endSample = totalSamples
		}
		for s := startSample; s < endSample; s++ {
			localPos := float64(s-startSample) / float64(samplesPerMora)
			// Syllable envelope (smooth attack & release for natural Live2D lip-sync)
			env := math.Sin(localPos * math.Pi)
			t := float64(s) / float64(sampleRate)
			sig := 0.55*math.Sin(2*math.Pi*f0*t) +
				0.28*math.Sin(2*math.Pi*f1*t) +
				0.14*math.Sin(2*math.Pi*f2*t)
			val := sig * env * 9500.0
			pcm[s] = int16(math.Max(-32000, math.Min(32000, val)))
		}
	}

	return encodePCM16ToWAV(pcm, sampleRate)
}

func encodePCM16ToWAV(pcm []int16, sampleRate int) []byte {
	dataSize := len(pcm) * 2
	buf := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(buf, binary.LittleEndian, uint16(1)) // Mono
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	for _, sample := range pcm {
		_ = binary.Write(buf, binary.LittleEndian, sample)
	}
	return buf.Bytes()
}

// computeWAVRMSVolumes extracts normalized 0.0-1.0 RMS volume chunks every chunkMs (20ms)
// for synchronized Live2D mouth lip-syncing.
func computeWAVRMSVolumes(wav []byte, chunkMs int) ([]float64, int) {
	if len(wav) <= 44 {
		return []float64{0.2, 0.6, 0.8, 0.4, 0.0}, 100
	}
	sampleRate := int(binary.LittleEndian.Uint32(wav[24:28]))
	if sampleRate <= 0 || sampleRate > 96000 {
		sampleRate = 24000
	}
	pcm := wav[44:]
	totalSamples := len(pcm) / 2
	samplesPerChunk := (sampleRate * chunkMs) / 1000
	if samplesPerChunk <= 0 {
		samplesPerChunk = 480
	}

	var volumes []float64
	for offset := 0; offset+1 < len(pcm); offset += samplesPerChunk * 2 {
		end := offset + samplesPerChunk*2
		if end > len(pcm) {
			end = len(pcm)
		}
		var sumSq float64
		count := 0
		for i := offset; i+1 < end; i += 2 {
			sample := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
			norm := float64(sample) / 32768.0
			sumSq += norm * norm
			count++
		}
		if count > 0 {
			rms := math.Sqrt(sumSq / float64(count))
			scaled := math.Min(1.0, rms*4.2)
			volumes = append(volumes, math.Round(scaled*100)/100)
		}
	}
	durationMs := (totalSamples * 1000) / sampleRate
	return volumes, durationMs
}
