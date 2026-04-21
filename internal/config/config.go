package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTP      HTTPConfig
	GRPC      GRPCConfig
	WS        WSConfig
	Database  DatabaseConfig
	Redis     RedisConfig
	External  ExternalConfig
	Dashboard DashboardConfig
	Inbound   InboundConfig
	Log       LogConfig
}

type HTTPConfig struct {
	ListenAddr string
}

type GRPCConfig struct {
	ListenAddr string
}

type WSConfig struct {
	ListenAddr string
	Path       string
}

type DatabaseConfig struct {
	URL      string
	MaxConns int32
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	Channel  string
}

type ExternalConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

type DashboardConfig struct {
	BaseURL        string
	APIKey         string
	Timeout        time.Duration
	AllowedOrigins []string
}

type InboundConfig struct {
	APIKey string
}

type LogConfig struct {
	Level  string
	Format string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		HTTP: HTTPConfig{ListenAddr: getEnv("HTTP_LISTEN_ADDR", ":8080")},
		GRPC: GRPCConfig{ListenAddr: getEnv("GRPC_LISTEN_ADDR", ":9090")},
		WS: WSConfig{
			ListenAddr: getEnv("WS_LISTEN_ADDR", ":8090"),
			Path:       getEnv("WS_PATH", "/ws"),
		},
		Database: DatabaseConfig{
			URL:      os.Getenv("DATABASE_URL"),
			MaxConns: int32(getEnvInt("DATABASE_MAX_CONNS", 10)),
		},
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       getEnvInt("REDIS_DB", 0),
			Channel:  getEnv("REDIS_CHANNEL", "worker.events"),
		},
		External: ExternalConfig{
			BaseURL: getEnv("EXTERNAL_BASE_URL", ""),
			APIKey:  os.Getenv("EXTERNAL_API_KEY"),
			Timeout: time.Duration(getEnvInt("EXTERNAL_TIMEOUT_SECONDS", 10)) * time.Second,
		},
		Dashboard: DashboardConfig{
			BaseURL:        getEnv("DASHBOARD_BASE_URL", ""),
			APIKey:         os.Getenv("DASHBOARD_API_KEY"),
			Timeout:        time.Duration(getEnvInt("DASHBOARD_TIMEOUT_SECONDS", 10)) * time.Second,
			AllowedOrigins: splitAndTrim(getEnv("DASHBOARD_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:8080")),
		},
		Inbound: InboundConfig{APIKey: os.Getenv("INBOUND_API_KEY")},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
	}

	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func splitAndTrim(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
