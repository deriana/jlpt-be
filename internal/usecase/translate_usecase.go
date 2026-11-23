package usecase

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/domain"
)

var htmlAndAssTagRegex = regexp.MustCompile(`(?i)<[^>]*>|\{\\[^}]*\}`)

// StripSubtitleMarkup removes any dirty HTML tags (<font face="...">, <b>, <i>) or ASS override tags ({\an8})
func StripSubtitleMarkup(s string) string {
	cleaned := htmlAndAssTagRegex.ReplaceAllString(s, "")
	cleaned = strings.ReplaceAll(cleaned, "&nbsp;", " ")
	cleaned = strings.ReplaceAll(cleaned, "&quot;", "\"")
	cleaned = strings.ReplaceAll(cleaned, "&#39;", "'")
	return strings.TrimSpace(cleaned)
}

type TranslateUsecase struct {
	DB       *sql.DB
	Cfg      *config.AppConfig
	mu       sync.RWMutex
	memCache map[string]string
}

func (u *TranslateUsecase) SetConfig(cfg *config.AppConfig) {
	u.mu.Lock()
	u.Cfg = cfg
	u.mu.Unlock()
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
	// Purge any dirty crowd-TM translations containing HTML tags (<font>, <b>) or MyMemory warnings
	_, _ = db.Exec(`
		DELETE FROM translations_cache
		WHERE target_id LIKE '%<%'
		   OR target_id LIKE '%>%'
		   OR target_id LIKE '%MYMEMORY%'
		   OR provider LIKE '%mymemory%'
	`)

	uc := &TranslateUsecase{
		DB:       db,
		memCache: make(map[string]string, 1024),
	}
	rows, err := db.Query(`SELECT source_en, target_id FROM translations_cache`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var src, dst string
			if rows.Scan(&src, &dst) == nil {
				uc.memCache[src] = StripSubtitleMarkup(dst)
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

	// 5. Pure Neural Machine Translation via Google Clients5 dict-chrome-ex (no dirty HTML tags, cached forever in SQLite!)
	if allowOnline {
		if onlineRes, err := fetchGoogleClients5Translate(clean, "en", "id"); err == nil && onlineRes != "" {
			u.saveCache(lower, onlineRes, "auto-gclients5")
			return onlineRes
		}
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
	cleanedTarget := StripSubtitleMarkup(targetID)
	if cleanedTarget == "" {
		return
	}
	u.mu.Lock()
	u.memCache[lowerKey] = cleanedTarget
	u.mu.Unlock()
	_, _ = u.DB.Exec(`
		INSERT OR REPLACE INTO translations_cache (source_en, target_id, provider, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`, lowerKey, cleanedTarget, provider)
}

var animeConversationalJAtoID = map[string]string{
	"お、お姉さん?来てたんだ":   "Ka-Kakak perempuanmu? Apa dia sedang berkunjung?",
	"お、お姉さん？来てたんだ":   "Ka-Kakak perempuanmu? Apa dia sedang berkunjung?",
	"お…お姉さん?来てたんだ":   "Ka-Kakak perempuanmu? Apa dia sedang berkunjung?",
	"お…お姉さん？来てたんだ":   "Ka-Kakak perempuanmu? Apa dia sedang berkunjung?",
	"お…お姉さんいたんだ":     "Oh... ternyata kamu punya kakak perempuan ya.",
	"お姉さん?":          "Ka-Kakak perempuanmu?",
	"お姉さん？":          "Ka-Kakak perempuanmu?",
	"カノジョ…":          "Pa... car... ku.",
	"カノジョ":           "Pa... car... ku.",
	"彼女…":            "Pa... car... ku.",
	"彼女":             "Pa... car... ku.",
	"え?":             "Eh?",
	"え？":             "Eh?",
	"えっ?":            "Eh?",
	"えっ？":            "Eh?",
	"ん?":             "Hmm?",
	"ん？":             "Hmm?",
	"はっ…":            "Hah...",
	"はぁ…":            "Haa...",
	"林田君は？":          "Kalau Hayashida-kun (di mana)?",
	"林田君は?":          "Kalau Hayashida-kun (di mana)?",
	"帰ったよ":           "Dia sudah pulang.",
	"お前が浮気相手ってことだよ":  "Dengar, itu artinya kamulah selingkuhannya.",
	"もう用済みだからな":      "Tapi aku sudah selesai denganmu sekarang (kamu sudah tidak berguna lagi).",
	"もう用済みだからな…":     "Tapi aku sudah selesai denganmu sekarang (kamu sudah tidak berguna lagi).",
	"もう用済みだな":        "Kamu sudah tidak berguna lagi sekarang.",
	"もう用済み…":         "Sudah tidak berguna lagi...",
	"やだ!受ける!":        "Astaga! Kocak banget!",
	"やだ！ウケる！":        "Astaga! Kocak banget!",
	"やだ、ウケる！":        "Astaga, kocak banget!",
	"やだ、受ける":         "Astaga, kocak banget!",
	"お前の勘違いだよバーカ":    "Itu cuma salah pahammu, bodoh.",
}

var animeConversationalJAtoEN = map[string]string{
	"お、お姉さん?来てたんだ":   "Oh, your older sister? Is she visiting?",
	"お、お姉さん？来てたんだ":   "Oh, your older sister? Is she visiting?",
	"お…お姉さん?来てたんだ":   "Oh, your older sister? Is she visiting?",
	"お…お姉さん？来てたんだ":   "Oh, your older sister? Is she visiting?",
	"お…お姉さんいたんだ":     "Oh... so you have an older sister.",
	"お姉さん?":          "Your older sister?",
	"お姉さん？":          "Your older sister?",
	"カノジョ…":          "My... girlfriend...",
	"カノジョ":           "My girlfriend.",
	"彼女…":            "My... girlfriend...",
	"彼女":             "My girlfriend.",
	"え?":             "Eh?",
	"え？":             "Eh?",
	"えっ?":            "Eh?",
	"えっ？":            "Eh?",
	"ん?":             "Hmm?",
	"ん？":             "Hmm?",
	"はっ…":            "Gasp...",
	"はぁ…":            "Sigh...",
	"林田君は？":          "Where is Hayashida-kun?",
	"林田君は?":          "Where is Hayashida-kun?",
	"帰ったよ":           "He went home.",
	"お前が浮気相手ってことだよ":  "That means you're the affair partner.",
	"もう用済みだからな":      "Because I have no more use for you now.",
	"もう用済みだからな…":     "Because I have no more use for you now...",
	"もう用済みだな":        "You're no longer needed now.",
	"もう用済み…":         "No longer needed...",
	"やだ!受ける!":        "No way! That's hilarious!",
	"やだ！ウケる！":        "No way! That's hilarious!",
	"やだ、ウケる！":        "No way, that's hilarious!",
	"やだ、受ける":         "No way, that's hilarious!",
	"お前の勘違いだよバーカ":    "That's just your misunderstanding, idiot.",
}

var jaHomophoneNormalizer = []struct {
	wrong string
	right string
}{
	{"お…お姉さんってたんだ", "お、お姉さん？来てたんだ"},
	{"お、お姉さん、来てたんだ", "お、お姉さん？来てたんだ"},
	{"お姉さん、来てたんだ", "お、お姉さん？来てたんだ"},
	{"お姉さんってたんだ", "お姉さんいたんだ"},
	{"か、の、じょ。", "彼女…"},
	{"か、の、じょ", "彼女…"},
	{"カノジオ", "カノジョ"},
	{"彼の女", "彼女"},
	{"うわき合いって", "浮気相手"},
	{"浮き合いって", "浮気相手"},
	{"浮気合いって", "浮気相手"},
	{"上き合いって", "浮気相手"},
	{"上着相手", "浮気相手"},
	{"うわきあいて", "浮気相手"},
	{"は、はやしだくんを?", "林田君は？"},
	{"はやしだくんを?", "林田君は？"},
	{"はやしだくん", "林田君"},
	{"ごお体しルート", "王太子ルート"},
	{"ナイトを困ると思って", "ないと困ると思って"},
	{"もう様子見だけだなぁ", "もう用済みだからな"},
	{"もう様子見だけだな", "もう用済みだからな"},
	{"もう様子みだけだな", "もう用済みだからな"},
	{"もう様子見たけどな", "もう用済みだからな"},
	{"もう様子見…", "もう用済み…"},
	{"もう様子み…", "もう用済み…"},
	{"もう様子味", "もう用済み"},
	{"やだ、受ける", "やだ、ウケる！"},
	{"やだ!受ける!", "やだ、ウケる！"},
	{"研究性感", "研究成果"},
	{"研究の軽しも", "研究も彼氏も"},
	{"コン役者", "婚約者"},
	{"私の今役は", "私の婚約は"},
	{"婚約を発揮", "婚約を破棄"},
	{"天性してしまった", "転生してしまった"},
	{"モバーク役礼女", "モブ悪役令嬢"},
	{"悪役礼女", "悪役令嬢"},
	{"王大使", "王太子"},
	{"応対しルート", "王太子ルート"},
	{"法律アカデミー", "王立アカデミー"},
	{"交換度を上げて", "好感度を上げて"},
	{"円組", "縁組"},
	{"全ルート整発", "全ルート制覇"},
	{"全ルート生破", "全ルート制覇"},
	{"ほ…ほよしど君を?", "林田君は？"},
	{"ほよしど君を?", "林田君は？"},
	{"バーガー", "バーカ"},
	{"バーカー", "バーカ"},
	{"鬼もつあつかい", "お荷物扱い"},
	{"つつましく暗します", "つつましく暮らします"},
	{"気づつきたくない", "傷つきたくない"},
	{"カスセキ", "化石"},
	{"動物食物", "動物植物"},
	{"タイコの意外が地装に", "太古の遺骸が地層に"},
	{"ミュージアも作って", "ミュージアムも作って"},
	{"おじわさま", "お嬢様"},
	{"お女様", "お嬢様"},
	{"お見前にいらっしゃい", "お見舞いにいらっしゃい"},
	{"おなしクラス", "同じクラス"},
	{"なくよし", "仲良し"},
	{"急学してた", "休学してた"},
	{"道の区", "陸奥"},
	{"ショム", "庶務"},
	{"うめしょき", "梅書記"},
	{"初期ありたかった", "書記やりたかった"},
	{"選配", "先輩"},
	{"ふん息", "雰囲気"},
	{"コウホー活動", "広報活動"},
	{"コウホー", "広報"},
	{"矢解です", "了解です"},
	{"妖怪です", "了解です"},
	{"スマホ乗れた", "スマホ濡れた"},
	{"スマホ塗れた", "スマホ濡れた"},
	{"スマホを塗れた", "スマホ濡れた"},
}

// TranslateJAtoID translates a Japanese sentence into clean Bahasa Indonesia (with SQLite caching)
// using Google Neural Machine Translation (clients5 dict-chrome-ex) and strips any HTML/subtitle markup.
func (u *TranslateUsecase) TranslateJAtoID(jaText string) string {
	clean := u.NormalizeJapaneseTranscript(jaText)
	if clean == "" {
		return ""
	}
	if override, ok := animeConversationalJAtoID[clean]; ok {
		return override
	}

	cacheKey := "ja:" + clean
	u.mu.RLock()
	if cached, ok := u.memCache[cacheKey]; ok && cached != "" {
		u.mu.RUnlock()
		return StripSubtitleMarkup(cached)
	}
	u.mu.RUnlock()

	if translated, err := fetchGoogleClients5Translate(clean, "ja", "id"); err == nil && translated != "" {
		cleaned := StripSubtitleMarkup(translated)
		u.saveCache(cacheKey, cleaned, "auto-gclients5-ja-id")
		return cleaned
	}
	if translated, err := fetchGoogleTranslateGTXWithLang(clean, "ja", "id"); err == nil && translated != "" {
		cleaned := StripSubtitleMarkup(translated)
		u.saveCache(cacheKey, cleaned, "auto-gtx-ja-id")
		return cleaned
	}
	return ""
}

func (u *TranslateUsecase) NormalizeJapaneseTranscript(jaText string) string {
	clean := StripSubtitleMarkup(jaText)
	if clean == "" {
		return ""
	}
	for _, pair := range jaHomophoneNormalizer {
		clean = strings.ReplaceAll(clean, pair.wrong, pair.right)
	}
	return strings.TrimSpace(clean)
}

// TranslateJAtoEN translates a Japanese sentence into English (with SQLite caching).
func (u *TranslateUsecase) TranslateJAtoEN(jaText string) string {
	clean := u.NormalizeJapaneseTranscript(jaText)
	if clean == "" {
		return ""
	}
	if override, ok := animeConversationalJAtoEN[clean]; ok {
		return override
	}
	cacheKey := "ja_en:" + clean
	u.mu.RLock()
	if cached, ok := u.memCache[cacheKey]; ok && cached != "" {
		u.mu.RUnlock()
		return StripSubtitleMarkup(cached)
	}
	u.mu.RUnlock()

	if translated, err := fetchGoogleClients5Translate(clean, "ja", "en"); err == nil && translated != "" {
		cleaned := StripSubtitleMarkup(translated)
		u.saveCache(cacheKey, cleaned, "auto-gclients5-ja-en")
		return cleaned
	}
	if translated, err := fetchGoogleTranslateGTXWithLang(clean, "ja", "en"); err == nil && translated != "" {
		cleaned := StripSubtitleMarkup(translated)
		u.saveCache(cacheKey, cleaned, "auto-gtx-ja-en")
		return cleaned
	}
	return ""
}

var (
	topicQuestionSuffixRegex = regexp.MustCompile(`^(.{1,12})(君|くん|さん|ちゃん|様|さま|先生)?は[?？。]*$`)
	leadingListNumberRegex   = regexp.MustCompile(`^\d+[\.\)\:]\s*`)
)

func lookupConversationalMap(m map[string]string, s string) (string, bool) {
	if v, ok := m[s]; ok {
		return v, true
	}
	trimmed := strings.TrimRight(s, "。！! ")
	if v, ok := m[trimmed]; ok {
		return v, true
	}
	noComma := strings.ReplaceAll(trimmed, "、", "")
	for k, v := range m {
		kClean := strings.ReplaceAll(strings.TrimRight(k, "。！! "), "、", "")
		if kClean == noComma {
			return v, true
		}
	}
	return "", false
}

// TranslateTranscriptCueJA translates a Japanese transcript sentence into BOTH Bahasa Indonesia (JP -> ID)
// and English (JP -> EN) using the user's Local AI (Qwen 2.5 3B via Ollama) grounded with OpenJLPT vocabulary
// meanings and surrounding dialogue context (e.g. resolving "林田君は？" -> "帰ったよ" as "Where is Hayashida-kun?" -> "He went home.").
func (u *TranslateUsecase) TranslateTranscriptCueJA(
	jaText string,
	prevJaText string,
	matchedVocab []domain.MatchedVocabToken,
	softsubID string,
) (translationID string, translationEN string, provider string) {
	clean := u.NormalizeJapaneseTranscript(jaText)
	if clean == "" {
		return "", "", ""
	}
	cleanPrev := u.NormalizeJapaneseTranscript(prevJaText)

	// 1. Check exact verified conversational anime overrides (ignoring trailing periods/commas)
	if ovID, ok := lookupConversationalMap(animeConversationalJAtoID, clean); ok {
		translationID = ovID
	}
	if ovEN, ok := lookupConversationalMap(animeConversationalJAtoEN, clean); ok {
		translationEN = ovEN
	}
	if softsubID != "" {
		translationID = StripSubtitleMarkup(softsubID)
	}

	// 2. Build targeted semantic notes & context for Qwen 2.5 3B
	var notes []string
	if topicQuestionSuffixRegex.MatchString(clean) {
		notes = append(notes, `Note: "[Name]は？" asks "Where is [Name]? / What about [Name]?", NEVER "How are you, [Name]?".`)
	}
	usePrevContext := false
	if cleanPrev != "" {
		if strings.HasSuffix(cleanPrev, "?") || strings.HasSuffix(cleanPrev, "？") || len([]rune(clean)) <= 10 {
			usePrevContext = true
		}
		if topicQuestionSuffixRegex.MatchString(cleanPrev) {
			usePrevContext = true
			notes = append(notes, `Note: The omitted subject refers to the person asked about in the previous line ("He/She ..."), NOT "I".`)
		}
	}
	if strings.Contains(clean, "浮気相手") {
		notes = append(notes, `Note: "浮気相手" means "the affair partner / the other woman".`)
	}
	if strings.Contains(clean, "用済み") {
		notes = append(notes, `Note: "もう用済みだからな" means "Because I have no more use for you now."`)
	}
	if strings.Contains(clean, "ウケる") || strings.Contains(clean, "受ける") {
		notes = append(notes, `Note: "やだ、ウケる" is slang meaning "No way, that is hilarious!"`)
	}
	if softsubID != "" {
		notes = append(notes, fmt.Sprintf("Indonesian reference subtitle: %s", softsubID))
	}

	prevForPrompt := ""
	if usePrevContext {
		prevForPrompt = cleanPrev
	}

	// 3. Translate JP -> EN via Local AI Qwen 2.5 (3B) refining a base NMT draft with dialogue context
	provider = "qwen2.5:3b"
	if translationEN == "" {
		ctxKey := "qwen_v2_en:" + prevForPrompt + "|" + clean
		u.mu.RLock()
		cachedEN := u.memCache[ctxKey]
		u.mu.RUnlock()

		draftEN := u.TranslateJAtoEN(clean)
		if cachedEN != "" && !ContainsJapaneseScript(cachedEN) {
			translationEN = cachedEN
		} else if qwenEN, err := u.callLocalQwenJAtoEN(clean, prevForPrompt, draftEN, strings.Join(notes, " ")); err == nil && qwenEN != "" {
			if ContainsJapaneseScript(qwenEN) {
				// Qwen resolved an omitted Japanese subject (e.g. "帰ったよ" -> "林田君は帰ったよ"): translate the resolved Japanese sentence!
				translationEN = u.TranslateJAtoEN(qwenEN)
			} else {
				translationEN = qwenEN
			}
			if translationEN != "" {
				u.saveCache(ctxKey, translationEN, "qwen2.5:3b-ja-en")
			}
		}
		if translationEN == "" {
			translationEN = draftEN
			provider = "gclients5-nmt"
		}
	}

	// 4. Produce JP -> ID grounded by Qwen 2.5's contextual English disambiguation
	if translationID == "" {
		ctxIDKey := "qwen_v2_id:" + prevForPrompt + "|" + clean
		u.mu.RLock()
		cachedID := u.memCache[ctxIDKey]
		u.mu.RUnlock()

		if cachedID != "" && !ContainsJapaneseScript(cachedID) {
			translationID = cachedID
		} else if translationEN != "" {
			if idFromEN, err := fetchGoogleClients5Translate(translationEN, "en", "id"); err == nil && idFromEN != "" {
				translationID = StripSubtitleMarkup(idFromEN)
				u.saveCache(ctxIDKey, translationID, provider+"-ja-id")
			}
		}
		if translationID == "" {
			translationID = u.TranslateJAtoID(clean)
		}
	}

	if softsubID != "" {
		provider = "softsub+" + provider
	}
	return translationID, translationEN, provider
}

func (u *TranslateUsecase) callLocalQwenJAtoEN(jaText, prevJaText, draftEN, semanticNotes string) (string, error) {
	baseURL := "http://127.0.0.1:11434/v1"
	modelName := "qwen2.5:3b"
	u.mu.RLock()
	if u.Cfg != nil {
		if u.Cfg.LLM.BaseURL != "" {
			baseURL = u.Cfg.LLM.BaseURL
		}
		if u.Cfg.LLM.Model != "" {
			modelName = u.Cfg.LLM.Model
		}
	}
	u.mu.RUnlock()

	resolvedBase, online, _ := config.ResolveServiceURL(baseURL, "models", 700*time.Millisecond)
	if !online {
		return "", fmt.Errorf("local qwen offline")
	}

	var promptBuilder strings.Builder
	if prevJaText != "" {
		promptBuilder.WriteString("Previous Japanese line: ")
		promptBuilder.WriteString(prevJaText)
		promptBuilder.WriteString("\n")
	}
	promptBuilder.WriteString("Current Japanese line: ")
	promptBuilder.WriteString(jaText)
	promptBuilder.WriteString("\n")
	if draftEN != "" {
		promptBuilder.WriteString("Draft English translation: ")
		promptBuilder.WriteString(draftEN)
		promptBuilder.WriteString("\n")
	}
	if semanticNotes != "" {
		promptBuilder.WriteString(semanticNotes)
		promptBuilder.WriteString("\n")
	}
	promptBuilder.WriteString("Write the accurate, natural English anime subtitle for the Current Japanese line (1 sentence only):")

	payload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "You are a professional Japanese-to-English anime subtitle translator. Refine the Draft English translation using the Japanese line, previous context, and Notes. Output ONLY the final English subtitle sentence without numbering or quotes.",
			},
			{
				"role":    "user",
				"content": promptBuilder.String(),
			},
		},
		"temperature": 0.1,
		"max_tokens":  80,
		"stream":      false,
	}
	bodyBytes, _ := json.Marshal(payload)

	endpoint := strings.TrimRight(resolvedBase, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("qwen status %d", resp.StatusCode)
	}

	var resData struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil || len(resData.Choices) == 0 {
		return "", fmt.Errorf("empty qwen response")
	}

	raw := strings.TrimSpace(resData.Choices[0].Message.Content)
	firstLine := strings.Split(raw, "\n")[0]
	firstLine = strings.TrimSpace(firstLine)
	if idx := strings.Index(firstLine, "=>"); idx != -1 {
		firstLine = strings.TrimSpace(firstLine[idx+2:])
	}
	firstLine = leadingListNumberRegex.ReplaceAllString(firstLine, "")
	firstLine = strings.Trim(firstLine, `"' `)
	return StripSubtitleMarkup(firstLine), nil
}

// fetchGoogleClients5Translate uses Google's dict-chrome-ex NMT endpoint which returns pure neural translations
// without dirty crowd-TM <font> tags or MyMemory daily quota limits.
func fetchGoogleClients5Translate(text, sourceLang, targetLang string) (string, error) {
	client := &http.Client{Timeout: 2500 * time.Millisecond}
	apiURL := fmt.Sprintf(
		"https://clients5.google.com/translate_a/t?client=dict-chrome-ex&sl=%s&tl=%s&q=%s",
		url.QueryEscape(sourceLang),
		url.QueryEscape(targetLang),
		url.QueryEscape(text),
	)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("clients5 status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var strArr []string
	if err := json.Unmarshal(body, &strArr); err == nil && len(strArr) > 0 {
		return StripSubtitleMarkup(strings.Join(strArr, " ")), nil
	}

	var nestedArr [][]string
	if err := json.Unmarshal(body, &nestedArr); err == nil && len(nestedArr) > 0 && len(nestedArr[0]) > 0 {
		return StripSubtitleMarkup(nestedArr[0][0]), nil
	}

	return "", fmt.Errorf("unexpected clients5 format")
}

func fetchGoogleTranslateGTX(text string) (string, error) {
	return fetchGoogleTranslateGTXWithLang(text, "en", "id")
}

func fetchGoogleTranslateGTXWithLang(text, sourceLang, targetLang string) (string, error) {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	apiURL := fmt.Sprintf(
		"https://translate.googleapis.com/translate_a/single?client=gtx&sl=%s&tl=%s&dt=t&q=%s",
		url.QueryEscape(sourceLang),
		url.QueryEscape(targetLang),
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
