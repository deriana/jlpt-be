# JLPTMaster Backend — Go Gin Clean Architecture + OpenJLPT SQLite + Windows VOICEVOX

Offline-first backend service for **JLPTMaster (JLPT N4 & N3 Personal Study Web Application)** built with **Go (Gin)**, **Modular Clean Architecture**, **SQLite**, **OpenJLPT Dataset (`evanclan/OpenJLPT`)**, **Windows Host VOICEVOX TTS Bridge (from WSL2)**, and **Local AI Tutor (Ollama + Offline OpenJLPT Sensei Fallback)**.

---

## 🏗️ Modular Clean Architecture

```text
backend/
├── cmd/
│   ├── api/main.go               # Gin REST API server (:8090)
│   └── scraper/main.go           # OpenJLPT SQLite/JSON dataset importer & seeder
├── config/
│   └── config.json               # Server, Windows VOICEVOX, Local AI (Ollama), Live2D config
├── data/
│   ├── openjlpt/                 # Official OpenJLPT SQLite & JSON dataset (7,811 Vocab, 2,383 Kanji, 526 Grammar)
│   ├── jlptmaster.db             # Local offline-first SQLite study database
│   └── audio_cache/              # Cached VOICEVOX WAV files for offline replay
└── internal/
    ├── config/                   # AppConfig + WSL2-to-Windows Host Gateway Auto-Resolver
    ├── domain/                   # Clean Architecture Domain Entities
    ├── repository/sqlite/        # SQLite Repositories (User, OpenJLPT, SRS, Mining, Tutor)
    ├── usecase/                  # Business Logic (SM-2 SRS, SRT/LRC Miner, OpenJLPT Matcher, VOICEVOX, AI Tutor)
    └── delivery/http/            # Gin Middleware, REST Handlers & Router
```

---

## 🚀 Quick Start

### 1. Sync / Re-Scrape OpenJLPT Dataset into SQLite
```bash
go run ./cmd/scraper
```

### 2. Run the API Server
```bash
go run ./cmd/api
# or run compiled binary:
./bin/jlptmaster-api
```
Server listens at **`http://localhost:8090`**.
