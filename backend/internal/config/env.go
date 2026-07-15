package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	NodeEnv    string
	Port       int
	DatabaseURL string
	RedisURL   string
	JwtSecret  string
	JwtExpiresIn string
	CorsOrigin string
	AnthropicApiKey string
	GeminiApiKey    string
	GeminiModel     string
	AIApiKey      string
	AIBaseURL     string
	AIModel       string
	AIRouterModel string
}

var Env *Config

func Load() {
	// Load .env file (ignore error if not present — Docker uses real env vars)
	_ = godotenv.Load()

	Env = &Config{
		NodeEnv:          getEnv("NODE_ENV", "development"),
		Port:             getEnvInt("PORT", 4000),
		DatabaseURL:      requireEnv("DATABASE_URL"),
		RedisURL:         getEnv("REDIS_URL", "redis://localhost:6379"),
		JwtSecret:        getEnv("JWT_SECRET", "dev-secret-change-in-production-min-32-chars!!"),
		JwtExpiresIn:     getEnv("JWT_EXPIRES_IN", "24h"),
		CorsOrigin:       getEnv("CORS_ORIGIN", "http://localhost:5173"),
		AnthropicApiKey:  getEnv("ANTHROPIC_API_KEY", ""),
		GeminiApiKey:     getEnv("GEMINI_API_KEY", ""),
		GeminiModel:      getEnv("GEMINI_MODEL", "gemini-1.5-flash-latest"),
		AIApiKey:         getEnv("AI_API_KEY", getEnv("GROQ_API_KEY", "")),
		AIBaseURL:        getEnv("AI_BASE_URL", ""),
		AIModel:          getEnv("AI_MODEL", ""),
		AIRouterModel:    getEnv("AI_ROUTER_MODEL", ""),
	}
}

func (c *Config) IsDev() bool  { return c.NodeEnv == "development" }
func (c *Config) IsProd() bool { return c.NodeEnv == "production" }

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		panic(fmt.Sprintf("Missing required environment variable: %s", key))
	}
	return val
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}
