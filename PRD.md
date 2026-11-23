# Product Requirements Document: JLPTMaster - Personal Study Web Application

## Problem Statement
Beginner Japanese learners preparing for the JLPT N4 and N3 exams often struggle with retaining vocabulary and kanji due to the lack of engaging, real-world context in traditional study materials. Static textbook drills are insufficient for building long-term memory and practical language skills. There is a need for a platform that bridges structured vocabulary mastery with immersive, real-world Japanese media.

## Goals & Success Metrics
### Goals
1. **User Engagement:** Increase daily active users through interactive and engaging study methods.
2. **Vocabulary Mastery:** Help users achieve 90% coverage of JLPT N4 and N3 vocabulary within 6 months.
3. **Media Integration:** Enable users to integrate real-world media (anime, music) into their study routines.
4. **Offline Usability:** Ensure the application is functional even without an internet connection.

### Success Metrics
1. **User Retention:** 70% of users return daily for at least 30 minutes.
2. **Vocabulary Coverage:** 85% of users achieve 90% vocabulary coverage within 6 months.
3. **Media Usage:** 60% of users incorporate media-based sentences into their study decks.
4. **Offline Usage:** 50% of study sessions are conducted offline.

## Technical Architecture
- **Backend:** Go (Gin) following Modular Clean Architecture (`domain`, `repository`, `usecase`, `delivery/http`, `config`), offline-first SQLite database.
- **Dataset & Scraper:** OpenJLPT (`https://github.com/evanclan/OpenJLPT`) full SQLite & JSON dataset integration for N5–N1 vocabulary, kanji, and grammar.
- **TTS Engine:** Windows Host VOICEVOX Engine integration from WSL2 via auto-detected Windows Gateway IP (`:50021`) with dynamic speaker/voice switching and offline WAV caching + synthetic fallback.
- **Local AI Tutor & Live2D:** Local LLM (Ollama / OpenAI-compatible API via WSL2-to-Windows auto-gateway + offline OpenJLPT Sensei fallback) paired with interactive Live2D models (`Mao Pro`, `Shiroko`, `Shizuku`) with real-time RMS audio lip-sync and emotion triggers.
- **Frontend:** React + TypeScript + Vite + Tailwind CSS based on `satnaing/shadcn-admin`.
