package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeAPI    Mode = "api"
	ModeWorker Mode = "worker"
	ModeAll    Mode = "all"
)

func (m Mode) RunsAPI() bool    { return m == ModeAPI || m == ModeAll }
func (m Mode) RunsWorker() bool { return m == ModeWorker || m == ModeAll }

type Config struct {
	App  App
	HTTP HTTP
	Log  Log
	DB   DB
	CORS CORS
}

type App struct {
	Env  string
	Mode Mode
}

func (a App) IsProduction() bool { return a.Env == "production" }

type HTTP struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

func (h HTTP) Addr() string { return ":" + h.Port }

type Log struct {
	Level  string
	Format string
}

type DB struct {
	URL            string
	MaxConns       int32
	MinConns       int32
	MigrationsDir  string
	ConnectRetries int
	ConnectBackoff time.Duration
}

type CORS struct {
	AllowedOrigins []string
}

func Load() (*Config, error) {
	cfg := &Config{
		App: App{
			Env:  env("APP_ENV", "development"),
			Mode: Mode(strings.ToLower(env("APP_MODE", string(ModeAll)))),
		},
		HTTP: HTTP{
			Port:            env("PORT", env("HTTP_PORT", "8080")),
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Log: Log{
			Level:  strings.ToLower(env("LOG_LEVEL", "info")),
			Format: strings.ToLower(env("LOG_FORMAT", "json")),
		},
		DB: DB{
			URL:            env("DATABASE_URL", ""),
			MaxConns:       int32(envInt("DB_MAX_CONNS", 10)),
			MinConns:       int32(envInt("DB_MIN_CONNS", 2)),
			MigrationsDir:  env("MIGRATIONS_DIR", "migrations"),
			ConnectRetries: envInt("DB_CONNECT_RETRIES", 10),
			ConnectBackoff: envDuration("DB_CONNECT_BACKOFF", time.Second),
		},
		CORS: CORS{
			AllowedOrigins: envList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch c.App.Mode {
	case ModeAPI, ModeWorker, ModeAll:
	default:
		return fmt.Errorf("config: невідомий APP_MODE %q (очікується api, worker або all)", c.App.Mode)
	}

	if c.DB.URL == "" {
		return fmt.Errorf("config: DATABASE_URL обовʼязковий")
	}
	if c.DB.MinConns > c.DB.MaxConns {
		return fmt.Errorf("config: DB_MIN_CONNS (%d) більший за DB_MAX_CONNS (%d)", c.DB.MinConns, c.DB.MaxConns)
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: SHUTDOWN_TIMEOUT має бути додатнім")
	}
	return nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v, err := strconv.Atoi(env(key, ""))
	if err != nil {
		return def
	}
	return v
}

func envDuration(key string, def time.Duration) time.Duration {
	v, err := time.ParseDuration(env(key, ""))
	if err != nil {
		return def
	}
	return v
}

func envList(key string, def []string) []string {
	raw := env(key, "")
	if raw == "" {
		return def
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}
