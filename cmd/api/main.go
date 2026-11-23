package main

import (
	"fmt"
	"log"

	"jlptmaster/backend/internal/config"
	deliveryhttp "jlptmaster/backend/internal/delivery/http"
	"jlptmaster/backend/internal/repository/sqlite"
	"jlptmaster/backend/internal/usecase"
)

func main() {
	cfg, err := config.LoadConfig("./config/config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := sqlite.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite database: %v", err)
	}
	defer db.DB.Close()

	// Repositories
	userRepo := sqlite.NewUserRepository(db.DB)
	jlptRepo := sqlite.NewJLPTRepository(db.DB)
	srsRepo := sqlite.NewSRSRepository(db.DB)
	miningRepo := sqlite.NewMiningRepository(db.DB)
	tutorRepo := sqlite.NewTutorRepository(db.DB)

	// Pre-warm OpenJLPT vocabulary matcher index for <1ms sentence mining
	if idx, err := jlptRepo.GetVocabMatcherIndex(); err == nil {
		log.Printf("[OpenJLPT Matcher] Pre-warmed %d vocabulary entries for offline sentence mining", len(idx))
	}

	// Usecases
	authUC := usecase.NewAuthUsecase(userRepo)
	jlptUC := usecase.NewJLPTUsecase(db, jlptRepo)
	srsUC := usecase.NewSRSUsecase(srsRepo, jlptRepo)
	miningUC := usecase.NewMiningUsecase(miningRepo, srsRepo, jlptRepo)
	progressUC := usecase.NewProgressUsecase(db.DB)
	ttsUC := usecase.NewTTSUsecase(cfg)
	translateUC := usecase.NewTranslateUsecase(db.DB)
	translateUC.SetConfig(cfg)
	tutorUC := usecase.NewTutorUsecase(cfg, tutorRepo, jlptRepo, ttsUC, translateUC)

	// Delivery
	handler := deliveryhttp.NewHandler(cfg, authUC, jlptUC, srsUC, miningUC, progressUC, ttsUC, tutorUC, translateUC)
	router := deliveryhttp.NewRouter(handler)

	winIP := config.GetWindowsHostIP()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("[JLPTMaster API] Starting Go Gin Clean Architecture Server on http://%s (WSL2->Windows Host Gateway IP: %s)", addr, winIP)
	if err := router.Run(addr); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}
