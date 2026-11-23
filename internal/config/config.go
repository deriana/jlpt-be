package config

import (
	"encoding/json"
	"os"
	"sync"
)

type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type DatabaseConfig struct {
	SQLitePath         string `json:"sqlite_path"`
	OpenJLPTSQLitePath string `json:"openjlpt_sqlite_path"`
	OpenJLPTJSONDir    string `json:"openjlpt_json_dir"`
}

type TTSConfig struct {
	Provider        string  `json:"provider"`
	VoicevoxURL     string  `json:"voicevox_url"`
	DefaultSpeaker  string  `json:"default_speaker_id"`
	SpeedScale      float64 `json:"speed_scale"`
	PitchScale      float64 `json:"pitch_scale"`
	IntonationScale float64 `json:"intonation_scale"`
	CacheDir        string  `json:"cache_dir"`
}

type LLMConfig struct {
	Provider    string  `json:"provider"`
	BaseURL     string  `json:"base_url"`
	APIKey      string  `json:"api_key"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	SenseiName  string  `json:"sensei_name"`
	Archetype   string  `json:"archetype"`
}

type Live2DModelInfo struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Path  string  `json:"path"`
	Scale float64 `json:"scale"`
}

type Live2DConfig struct {
	ActiveModel string            `json:"active_model"`
	Models      []Live2DModelInfo `json:"models"`
}

type AppConfig struct {
	mu       sync.RWMutex
	FilePath string         `json:"-"`
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
	TTS      TTSConfig      `json:"tts"`
	LLM      LLMConfig      `json:"llm"`
	Live2D   Live2DConfig   `json:"live2d"`
}

func LoadConfig(path string) (*AppConfig, error) {
	cfg := &AppConfig{
		FilePath: path,
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8090,
		},
		Database: DatabaseConfig{
			SQLitePath:         "./data/jlptmaster.db",
			OpenJLPTSQLitePath: "./data/openjlpt/openjlpt.sqlite",
			OpenJLPTJSONDir:    "./data/openjlpt/json",
		},
		TTS: TTSConfig{
			Provider:        "voicevox",
			VoicevoxURL:     "http://127.0.0.1:50021",
			DefaultSpeaker:  "3",
			SpeedScale:      1.0,
			PitchScale:      0.0,
			IntonationScale: 1.0,
			CacheDir:        "./data/audio_cache",
		},
		LLM: LLMConfig{
			Provider:    "ollama",
			BaseURL:     "http://127.0.0.1:11434/v1",
			APIKey:      "ollama",
			Model:       "qwen2.5:3b",
			Temperature: 0.7,
			SenseiName:  "Ryza-sensei",
			Archetype:   "tsundere",
		},
		Live2D: Live2DConfig{
			ActiveModel: "mao_pro",
			Models: []Live2DModelInfo{
				{ID: "mao_pro", Name: "Mao Pro (Sensei)", Path: "/live2d-models/mao_pro/runtime/mao_pro.model3.json", Scale: 0.14},
				{ID: "shiroko", Name: "Sunaookami Shiroko", Path: "/live2d-models/shiroko/shiroko.model3.json", Scale: 0.24},
				{ID: "shizuku", Name: "Shizuku (Classic Tutor)", Path: "/live2d-models/shizuku/runtime/shizuku.model3.json", Scale: 0.35},
			},
		},
	}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, cfg)
	}
	return cfg, nil
}

func (c *AppConfig) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FilePath == "" {
		return nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.FilePath, data, 0644)
}

func (c *AppConfig) Update(updater func(*AppConfig)) error {
	c.mu.Lock()
	updater(c)
	c.mu.Unlock()
	return c.Save()
}
