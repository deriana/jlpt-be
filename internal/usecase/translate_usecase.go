package usecase

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type TranslateUsecase struct {
	DB       *sql.DB
	mu       sync.RWMutex
	memCache map[string]string
}

// Offline English -> Bahasa Indonesia core JLPT N4/N3 dictionary & phrase map
var offlineEnToIdDict = map[string]string{
	// Core seeded Flashcards & Weak Spots
	"promise, appointment, arrangement":                     "janji, janji temu, kesepakatan",
	"promise":                                               "janji",
	"appointment":                                           "janji temu",
	"arrangement":                                           "pengaturan, kesepakatan",
	"preparation, arrangements, getting ready":              "persiapan, pengaturan, bersiap-siap",
	"preparation, arrangements":                             "persiapan, pengaturan",
	"preparation":                                           "persiapan",
	"arrangements":                                          "pengaturan",
	"getting ready":                                         "bersiap-siap",
	"experience":                                            "pengalaman",
	"consultation, discussion":                              "konsultasi, diskusi",
	"consultation":                                          "konsultasi",
	"discussion":                                            "diskusi",
	"polite, courteous, careful":                            "sopan, santun, teliti",
	"polite":                                                "sopan",
	"courteous":                                             "santun, ramah",
	"careful":                                               "hati-hati, teliti",
	"custom, habit, practice":                               "kebiasaan, adat, praktik",
	"custom":                                                "adat, kebiasaan",
	"habit":                                                 "kebiasaan",
	"practice":                                              "latihan, praktik",
	"complex, complicated":                                  "rumit, kompleks",
	"complex":                                               "kompleks, rumit",
	"complicated":                                           "rumit",
	"limited express (train)":                               "kereta ekspres terbatas (tokkyuu)",
	"to give up, to abandon":                                "menyerah, mengurungkan, meninggalkan",
	"to give up":                                            "menyerah",
	"to abandon":                                            "meninggalkan, mengabaikan",
	"being deeply moved emotionally, excitement":            "terharu, tersentuh secara emosional, kagum",
	"being deeply moved emotionally":                        "sangat terharu",
	"atmosphere, mood, ambiance":                            "suasana, atmosfer, nuansa",
	"atmosphere":                                            "suasana, atmosfer",
	"mood":                                                  "suasana hati, mood",
	"ambiance":                                              "nuansa, suasana",
	"by chance, unexpectedly, coincidence":                  "secara kebetulan, tanpa diduga, kebetulan",
	"by chance, coincidence":                                "secara kebetulan, kebetulan",
	"by chance":                                             "secara kebetulan",
	"unexpectedly":                                          "tanpa diduga",
	"coincidence":                                           "kebetulan",
	"influence, effect":                                     "pengaruh, dampak, efek",
	"influence":                                             "pengaruh",
	"effect":                                                "efek, dampak",
	"dear, desired, missed, nostalgic":                      "rindu, penuh kenangan, nostalgik",
	"nostalgic":                                             "nostalgik, merindukan masa lalu",
	"short, brief":                                          "pendek, singkat",
	"short":                                                 "pendek, singkat",
	"brief":                                                 "singkat",
	"to reach, to arrive, to be delivered":                  "sampai, tiba, tersampaikan",
	"to reach":                                              "mencapai, sampai",
	"to arrive":                                             "tiba, sampai",
	"to spread out, to extend, to stretch":                  "meluas, membentang, menyebar",
	"to spread out":                                         "membentang, meluas",
	"to extend":                                             "memperluas, memanjang",
	"absolutely, unconditionally":                           "mutlak, pasti, sama sekali",
	"absolutely":                                            "pasti, mutlak",
	"definitely":                                            "pasti",
	"human being":                                           "manusia",
	"person":                                                "orang",
	"more":                                                  "lebih",
	"even more":                                             "lebih banyak lagi",
	"surely":                                                "pasti, tentu saja",
	"undoubtedly":                                           "tidak diragukan lagi",
	"to sink":                                               "tenggelam, terbenam",
	"to feel depressed":                                     "merasa murung",
	"to melt":                                               "meleleh, larut",
	"to dissolve":                                           "larut",
	"important":                                             "penting",
	"valuable":                                              "berharga",
	"meeting":                                               "pertemuan, rapat",
	"meet":                                                  "bertemu",
	"party":                                                 "pesta, perkumpulan",
	"association":                                           "asosiasi, perkumpulan",
	"interview":                                             "wawancara",
	"join":                                                  "bergabung",
	"same":                                                  "sama",
	"agree":                                                 "setuju",
	"equal":                                                 "setara, sama",
	"do something for someone (as a favor)":                 "melakukan sesuatu untuk seseorang (sebagai bantuan)",
	"give (to someone else); give (to me or my group); receive": "memberi (ke orang lain); memberi (kepada saya); menerima",

	// Example sentences
	"please don't forget tomorrow's promise.":                                                     "tolong jangan lupakan janji besok ya.",
	"have you finished preparing for the trip already?":                                           "apakah persiapan liburannya sudah selesai?",
	"working in japan will be a good experience.":                                                 "bekerja di Jepang akan menjadi pengalaman yang baik.",
	"i consulted with my teacher about my future.":                                                "saya berkonsultasi dengan guru mengenai masa depan saya.",
	"she always speaks polite japanese.":                                                          "dia selalu berbicara bahasa Jepang yang sopan.",
	"i have a habit of studying japanese for 30 minutes every morning.":                           "saya punya kebiasaan belajar bahasa Jepang 30 menit setiap pagi.",
	"the story of this anime is a little complicated.":                                            "cerita anime ini sedikit rumit.",
	"let's go to kyoto by limited express train.":                                                 "mari pergi ke Kyoto naik kereta ekspres terbatas.",
	"never give up until the very end!":                                                           "jangan pernah menyerah sampai akhir!",
	"i was truly moved by the final scene of that movie.":                                         "saya benar-benar terharu melihat adegan terakhir film itu.",
	"this cafe has a very calm atmosphere, doesn't it?":                                           "kafe ini memiliki suasana yang sangat tenang ya.",
	"i ran into an old friend by chance at the station.":                                          "saya tidak sengaja bertemu teman lama di stasiun.",
	"japanese anime has a huge influence all over the world.":                                     "anime Jepang memberikan pengaruh besar di seluruh dunia.",
	"listening to this song makes me nostalgic for my high school days.":                          "mendengarkan lagu ini membuat saya rindu masa SMA.",
	"even though i knew human lifespans are short, why didn't i try to get to know him better?":  "meskipun aku tahu umur manusia itu singkat, kenapa aku tidak berusaha lebih mengenalnya?",
	"from now on, i am setting out on a journey to understand humans better.":                     "mulai sekarang, aku akan melakukan perjalanan untuk lebih memahami manusia.",
	"collecting magic is my hobby, so it's alright even if we take a small detour.":               "mengumpulkan sihir adalah hobiku, jadi tidak masalah meski kita sedikit memutar jalan.",
	"while i still remember our old promise, let's return to that place once more.":               "selagi aku masih mengingat janji lama kita, ayo kembali ke tempat itu sekali lagi.",
	"today's live show might have fewer guests because of the typhoon's influence.":               "konser hari ini mungkin penontonnya sedikit karena pengaruh topan.",
	"if we don't give up, our music will surely reach everyone!":                                  "jika kita tidak menyerah, musik kita pasti akan sampai ke semua orang!",
	"but if we don't give up, our music will surely reach everyone!":                              "tapi jika kita tidak menyerah, musik kita pasti akan sampai ke semua orang!",
	"because the experience of practicing with all our might every day never lies.":               "karena pengalaman berlatih sekuat tenaga setiap hari tidak akan pernah mengkhianati hasil.",
	"like sinking, like melting away, into the night where a sky just for the two of us spreads out.": "seperti tenggelam, seperti meleleh, ke dalam malam di mana langit hanya untuk kita berdua membentang.",
	"it was just \"goodbye\", with that single word i understood everything.":                     "hanya kata \"selamat tinggal\", dengan satu kata itu aku mengerti segalanya.",
	"the sky where the sun began to set and your figure overlapped beyond the fence.":             "langit saat matahari mulai terbenam dan sosokmu berpadu di balik pagar.",
	"from the day we first met, you stole all of my heart.":                                       "sejak hari pertama kita bertemu, kamu telah mencuri seluruh hatiku.",
	"so we don't forget even after waking up, let's write each other's names.":                    "agar kita tidak lupa meski setelah terbangun, ayo tulis nama kita masing-masing.",
	"what's important is not forgetting. i never want to forget your name!":                       "yang terpenting adalah tidak melupakan. aku tidak ingin pernah melupakan namamu!",
	"let's prepare before we forget tomorrow's promise.":                                          "ayo bersiap-siap sebelum kita lupa janji besok.",
}

// Common English word/gloss replacements for offline heuristic translation
var offlineWordTokens = map[string]string{
	"to be": "menjadi", "to do": "melakukan", "to make": "membuat", "to go": "pergi",
	"to come": "datang", "to see": "melihat", "to look": "melihat", "to hear": "mendengar",
	"to listen": "mendengarkan", "to speak": "berbicara", "to talk": "berbicara",
	"to say": "mengatakan", "to tell": "memberitahu", "to eat": "makan", "to drink": "minum",
	"to read": "membaca", "to write": "menulis", "to buy": "membeli", "to sell": "menjual",
	"to wait": "menunggu", "to understand": "mengerti", "to know": "tahu",
	"to think": "berpikir", "to feel": "merasa", "to use": "menggunakan",
	"to help": "membantu", "to change": "mengubah", "to start": "memulai", "to begin": "mulai",
	"to finish": "menyelesaikan", "to end": "berakhir", "to stop": "berhenti",
	"to continue": "melanjutkan", "to return": "kembali", "to enter": "masuk",
	"to leave": "pergi / meninggalkan", "to open": "membuka", "to close": "menutup",
	"to forget": "melupakan", "to remember": "mengingat", "to decide": "memutuskan",
	"to worry": "khawatir", "to study": "belajar", "to teach": "mengajar",
	"to work": "bekerja", "to live": "hidup / tinggal", "to sleep": "tidur",
	"to wake up": "bangun", "to laugh": "tertawa", "to smile": "tersenyum", "to cry": "menangis",
	"school": "sekolah", "teacher": "guru", "student": "murid / mahasiswa",
	"friend": "teman", "family": "keluarga", "mother": "ibu", "father": "ayah",
	"water": "air", "food": "makanan", "time": "waktu", "today": "hari ini",
	"tomorrow": "besok", "yesterday": "kemarin", "morning": "pagi", "night": "malam",
	"train": "kereta", "station": "stasiun", "room": "kamar / ruangan", "house": "rumah",
	"company": "perusahaan", "money": "uang", "book": "buku", "music": "musik",
	"movie": "film", "weather": "cuaca", "rain": "hujan", "wind": "angin", "sky": "langit",
	"happy": "senang / bahagia", "sad": "sedih", "angry": "marah", "kind": "baik hati",
	"quiet": "tenang / sepi", "famous": "terkenal", "easy": "mudah", "difficult": "sulit",
	"fast": "cepat", "slow": "lambat", "new": "baru", "old": "lama / tua",
	"big": "besar", "small": "kecil", "good": "bagus / baik", "bad": "buruk",
	"dangerous": "berbahaya", "safe": "aman", "free": "gratis / bebas", "busy": "sibuk",
}

func NewTranslateUsecase(db *sql.DB) *TranslateUsecase {
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS translations_cache (
			source_en TEXT PRIMARY KEY,
			target_id TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT 'offline-dict',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	uc := &TranslateUsecase{
		DB:       db,
		memCache: make(map[string]string, 1024),
	}
	// Preload SQLite cached translations into memory for <0.1ms lookups
	rows, err := db.Query(`SELECT source_en, target_id FROM translations_cache`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var src, dst string
			if rows.Scan(&src, &dst) == nil {
				uc.memCache[src] = dst
			}
		}
	}
	return uc
}

func (u *TranslateUsecase) TranslateENtoID(text string, allowOnline bool) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	lower := strings.ToLower(clean)

	// 1. Check exact offline dictionary
	if val, ok := offlineEnToIdDict[lower]; ok {
		return preserveCapitalization(clean, val)
	}

	// 2. Check memory/SQLite cache
	u.mu.RLock()
	if cached, ok := u.memCache[lower]; ok && cached != "" {
		u.mu.RUnlock()
		return cached
	}
	u.mu.RUnlock()

	// 3. Try split glosses (e.g. "meeting; meet; party" or "short, brief")
	sep := ", "
	if strings.Contains(clean, "; ") {
		sep = "; "
	}
	parts := strings.Split(clean, sep)
	if len(parts) > 1 {
		var translatedParts []string
		allMatched := true
		for _, p := range parts {
			pTrim := strings.TrimSpace(strings.ToLower(p))
			if v, ok := offlineEnToIdDict[pTrim]; ok {
				translatedParts = append(translatedParts, v)
			} else if v, ok := offlineWordTokens[pTrim]; ok {
				translatedParts = append(translatedParts, v)
			} else {
				allMatched = false
				break
			}
		}
		if allMatched && len(translatedParts) > 0 {
			res := strings.Join(translatedParts, sep)
			u.saveCache(lower, res, "offline-gloss")
			return res
		}
	}

	// 4. Single word/gloss lookup in offlineWordTokens
	if v, ok := offlineWordTokens[lower]; ok {
		u.saveCache(lower, v, "offline-word")
		return v
	}

	// 5. Optional automatic online Google Translate GTX (cached forever in SQLite!)
	if allowOnline {
		if onlineRes, err := fetchGoogleTranslateGTX(clean); err == nil && onlineRes != "" {
			u.saveCache(lower, onlineRes, "auto-gtx")
			return onlineRes
		}
	}

	// 6. Heuristic offline gloss replacement for partial matches
	heuristic := lower
	replacedAny := false
	for enKey, idVal := range offlineWordTokens {
		if heuristic == enKey {
			heuristic = idVal
			replacedAny = true
			break
		}
		if strings.HasPrefix(heuristic, enKey+",") || strings.HasPrefix(heuristic, enKey+";") {
			heuristic = strings.Replace(heuristic, enKey, idVal, 1)
			replacedAny = true
		}
	}
	if replacedAny {
		u.saveCache(lower, heuristic, "offline-heuristic")
		return heuristic
	}

	return clean
}

func (u *TranslateUsecase) TranslateBatchENtoID(texts []string, allowOnline bool) map[string]string {
	result := make(map[string]string, len(texts))
	for _, t := range texts {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			continue
		}
		if _, exists := result[trimmed]; !exists {
			result[trimmed] = u.TranslateENtoID(trimmed, allowOnline)
		}
	}
	return result
}

func (u *TranslateUsecase) saveCache(lowerKey, targetID, provider string) {
	u.mu.Lock()
	u.memCache[lowerKey] = targetID
	u.mu.Unlock()
	_, _ = u.DB.Exec(`
		INSERT OR REPLACE INTO translations_cache (source_en, target_id, provider, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`, lowerKey, targetID, provider)
}

func fetchGoogleTranslateGTX(text string) (string, error) {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	apiURL := fmt.Sprintf(
		"https://translate.googleapis.com/translate_a/single?client=gtx&sl=en&tl=id&dt=t&q=%s",
		url.QueryEscape(text),
	)
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed []interface{}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed) == 0 {
		return "", fmt.Errorf("invalid gtx json")
	}
	sentences, ok := parsed[0].([]interface{})
	if !ok {
		return "", fmt.Errorf("missing sentences array")
	}
	var sb strings.Builder
	for _, s := range sentences {
		if seg, ok := s.([]interface{}); ok && len(seg) > 0 {
			if str, ok := seg[0].(string); ok {
				sb.WriteString(str)
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

func preserveCapitalization(original, translated string) string {
	if len(original) > 0 && len(translated) > 0 {
		firstOrig := original[0]
		if firstOrig >= 'A' && firstOrig <= 'Z' {
			return strings.ToUpper(translated[:1]) + translated[1:]
		}
	}
	return translated
}
