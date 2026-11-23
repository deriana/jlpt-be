package usecase

import (
	"jlptmaster/backend/internal/domain"
	"jlptmaster/backend/internal/repository/sqlite"
)

type JLPTUsecase struct {
	DB       *sqlite.Database
	JLPTRepo *sqlite.JLPTRepository
}

func NewJLPTUsecase(db *sqlite.Database, repo *sqlite.JLPTRepository) *JLPTUsecase {
	return &JLPTUsecase{DB: db, JLPTRepo: repo}
}

func (u *JLPTUsecase) SearchVocab(userID int64, level, query, status string, limit, offset int) ([]domain.VocabItem, int, error) {
	return u.JLPTRepo.SearchVocab(userID, level, query, status, limit, offset)
}

func (u *JLPTUsecase) SearchKanji(userID int64, level, query string, limit, offset int) ([]domain.KanjiItem, int, error) {
	return u.JLPTRepo.SearchKanji(userID, level, query, limit, offset)
}

func (u *JLPTUsecase) SearchGrammar(userID int64, level, query string, limit, offset int) ([]domain.GrammarItem, int, error) {
	return u.JLPTRepo.SearchGrammar(userID, level, query, limit, offset)
}

func (u *JLPTUsecase) ToggleMastery(userID int64, itemType, itemID, level, status string) error {
	return u.JLPTRepo.ToggleMastery(userID, itemType, itemID, level, status)
}

func (u *JLPTUsecase) GetStats() (*domain.OpenJLPTStats, error) {
	return u.DB.GetOpenJLPTStats()
}

func (u *JLPTUsecase) SyncFromOpenJLPT() (*domain.OpenJLPTStats, error) {
	return u.DB.SyncFromOpenJLPTSource()
}
