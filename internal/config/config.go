package config

import (
	"log"
	"os"
)

const Version = "0.5.1"

type Config struct {
	AppPort    string
	DBUrl      string
	SecretKey  string
	BridgeURL  string // claude-bridge on the host (POST /ask)
	AIProvider  string // "bridge" (default) or "qwen"
	QwenAPIKey  string // OpenRouter/DashScope API key (required when AIProvider=qwen)
	QwenModel   string // e.g. "qwen/qwen3.8-27b"
	QwenBaseURL string // defaults to OpenRouter
}

func Load() Config {
	return Config{
		AppPort:     getEnv("APP_PORT", "8080"),
		DBUrl:       mustEnv("DATABASE_URL"),
		SecretKey:   mustEnv("SESSION_SECRET"),
		BridgeURL:   getEnv("BRIDGE_URL", "http://host.docker.internal:8765"),
		AIProvider:  getEnv("AI_PROVIDER", "bridge"),
		QwenAPIKey:  os.Getenv("QWEN_API_KEY"),
		QwenModel:   getEnv("QWEN_MODEL", "qwen/qwen3.8-27b"),
		QwenBaseURL: os.Getenv("QWEN_BASE_URL"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable: %s", key)
	}
	return v
}
