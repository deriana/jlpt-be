package usecase

import (
	"math"
	"strings"
	"time"

	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/repository/sqlite"
)

type SRSUsecase struct {
	SRSRepo  *sqlite.SRSRepository
	JLPTRepo *sqlite.JLPTRepository
}

func NewSRSUsecase(srsRepo *sqlite.SRSRepository, jlptRepo *sqlite.JLPTRepository) *SRSUsecase {
	return &SRSUsecase{SRSRepo: srsRepo, JLPTRepo: jlptRepo}
}

func (u *SRSUsecase) ListDecks(userID int64) ([]domain.Deck, error) {
	return u.SRSRepo.ListDecks(userID)
}

func (u *SRSUsecase) CreateDeck(userID int64, name, description, jlptLevel, category string, autoSeedCount int) (*domain.Deck, error) {
	if jlptLevel == "" {
		jlptLevel = "N4"
	}
	if category == "" {
		category = "vocabulary"
	}
	deck := &domain.Deck{
		UserID:      userID,
		Name:        name,
		Description: description,
		JLPTLevel:   jlptLevel,
		Category:    category,
	}
	if err := u.SRSRepo.CreateDeck(deck); err != nil {
		return nil, err
	}

	// If autoSeedCount > 0, populate the new custom deck from the local OpenJLPT SQLite database
	if autoSeedCount > 0 {
		if autoSeedCount > 50 {
			autoSeedCount = 50
		}
		lvl := strings.ToUpper(jlptLevel)
		if lvl != "N4" && lvl != "N3" && lvl != "N5" && lvl != "N2" && lvl != "N1" {
			lvl = "N4"
		}
		vocabs, _, err := u.JLPTRepo.SearchVocab(userID, lvl, "", "", autoSeedCount, 0)
		if err == nil {
			for _, v := range vocabs {
				ctxJa := ""
				ctxEn := ""
				if len(v.Examples) > 0 {
					ctxJa = v.Examples[0].Japanese
					ctxEn = v.Examples[0].English
				}
				card := &domain.Flashcard{
					DeckID:          deck.ID,
					UserID:          userID,
					FrontKanji:      v.Word,
					FuriganaReading: v.Reading,
					Romaji:          v.Romaji,
					BackMeaning:     strings.Join(v.Meanings, "; "),
					JLPTLevel:       v.Level,
					POS:             strings.Join(v.POS, ", "),
					ContextSentence: ctxJa,
					ContextMeaning:  ctxEn,
					MediaType:       "openjlpt",
					EaseFactor:      2.5,
				}
				_ = u.SRSRepo.CreateCard(card)
			}
		}
	}

	return deck, nil
}

func (u *SRSUsecase) ListCards(userID int64, deckID int64, onlyDue bool) ([]domain.Flashcard, error) {
	return u.SRSRepo.ListCards(userID, deckID, onlyDue)
}

func (u *SRSUsecase) CreateCard(userID int64, card *domain.Flashcard) (*domain.Flashcard, error) {
	card.UserID = userID
	if card.DeckID == 0 {
		decks, _ := u.SRSRepo.ListDecks(userID)
		if len(decks) > 0 {
			card.DeckID = decks[0].ID
		}
	}
	if err := u.SRSRepo.CreateCard(card); err != nil {
		return nil, err
	}
	return card, nil
}

// SubmitReview executes the SM-2 Spaced Repetition Algorithm:
// Quality: 1 = Again (Fail), 2 = Hard, 3 = Good, 4 = Easy.
func (u *SRSUsecase) SubmitReview(userID int64, sub domain.ReviewSubmission) (*domain.Flashcard, error) {
	card, err := u.SRSRepo.GetCardByID(sub.CardID)
	if err != nil {
		return nil, err
	}

	q := sub.Quality
	if q < 1 {
		q = 1
	} else if q > 4 {
		q = 4
	}
	card.LastQuality = q

	now := time.Now()
	switch q {
	case 1: // Again
		card.Repetitions = 0
		card.IntervalDays = 0
		card.Lapses++
		card.EaseFactor = math.Max(1.3, card.EaseFactor-0.2)
		card.NextReviewAt = now.Add(10 * time.Minute)
	case 2: // Hard
		card.Repetitions++
		if card.IntervalDays == 0 {
			card.IntervalDays = 1
		} else {
			card.IntervalDays = int(math.Max(1, math.Round(float64(card.IntervalDays)*1.2)))
		}
		card.EaseFactor = math.Max(1.3, card.EaseFactor-0.15)
		card.NextReviewAt = now.Add(time.Duration(card.IntervalDays) * 24 * time.Hour)
	case 3: // Good
		card.Repetitions++
		if card.Repetitions == 1 {
			card.IntervalDays = 1
		} else if card.Repetitions == 2 {
			card.IntervalDays = 3
		} else {
			card.IntervalDays = int(math.Round(float64(card.IntervalDays) * card.EaseFactor))
		}
		card.NextReviewAt = now.Add(time.Duration(card.IntervalDays) * 24 * time.Hour)
	case 4: // Easy
		card.Repetitions++
		if card.IntervalDays == 0 {
			card.IntervalDays = 4
		} else {
			card.IntervalDays = int(math.Round(float64(card.IntervalDays) * card.EaseFactor * 1.3))
		}
		card.EaseFactor += 0.15
		card.NextReviewAt = now.Add(time.Duration(card.IntervalDays) * 24 * time.Hour)
	}

	card.IsDue = !card.NextReviewAt.After(now)
	if err := u.SRSRepo.UpdateCardSRS(card, sub.DurationSecs, sub.IsOffline); err != nil {
		return nil, err
	}

	// Sync mastery state into OpenJLPT user_item_mastery if >= 3 repetitions
	status := "learning"
	if q == 1 {
		status = "weak"
	} else if card.IntervalDays >= 4 {
		status = "mastered"
	}
	_ = u.JLPTRepo.ToggleMastery(userID, "vocab", card.FrontKanji, card.JLPTLevel, status)

	return card, nil
}
