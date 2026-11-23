package main

import (
	"fmt"
	"log"

	"jlptmaster/backend/internal/config"
	"jlptmaster/backend/internal/repository/sqlite"
)

func main() {
	log.Println("[OpenJLPT Importer] Initializing OpenJLPT offline database sync (https://github.com/evanclan/OpenJLPT)...")
	cfg, err := config.LoadConfig("./config/config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := sqlite.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to open SQLite database: %v", err)
	}
	defer db.DB.Close()

	stats, err := db.SyncFromOpenJLPTSource()
	if err != nil {
		log.Fatalf("OpenJLPT sync failed: %v", err)
	}

	fmt.Println("====================================================")
	fmt.Println(" OpenJLPT Offline SQLite Database Sync Complete")
	fmt.Println("====================================================")
	fmt.Printf(" Total Vocabulary : %d (N4: %d, N3: %d)\n", stats.TotalVocab, stats.VocabByLevel["N4"], stats.VocabByLevel["N3"])
	fmt.Printf(" Total Kanji      : %d (N4: %d, N3: %d)\n", stats.TotalKanji, stats.KanjiByLevel["N4"], stats.KanjiByLevel["N3"])
	fmt.Printf(" Total Grammar    : %d (N4: %d, N3: %d)\n", stats.TotalGrammar, stats.GrammarByLevel["N4"], stats.GrammarByLevel["N3"])
	fmt.Println("====================================================")
}
