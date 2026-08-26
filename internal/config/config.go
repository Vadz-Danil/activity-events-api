package config

import (
	"fmt"
	"net/netip"
	"net/url"
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
	App         App
	HTTP        HTTP
	Log         Log
	DB          DB
	CORS        CORS
	Auth        Auth
	Google      Google
	Aggregation Aggregation
}

type Aggregation struct {
	Bucket   time.Duration
	Tick     time.Duration
	Backfill int
}

type App struct {
	Env         string
	Mode        Mode
	FrontendURL string
}

func (a App) IsProduction() bool { return a.Env == "production" }

type HTTP struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
	TrustedProxies  []string
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

type Auth struct {
	JWTSecret    string
	MinSecretLen int
	Issuer       string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	BcryptCost   int
}

type Google struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	JWKSURL      string
}

func (g Google) Enabled() bool { return g.ClientID != "" }

func (g Google) CodeFlowEnabled() bool {
	return g.Enabled() && g.ClientSecret != "" && g.RedirectURL != ""
}

const day = 24 * time.Hour

const (
	defaultMinJWTSecretLen = 32

	defaultAggregationBucket   = 4 * time.Hour
	defaultAggregationTick     = 5 * time.Minute
	defaultAggregationBackfill = 12
)

type Migrations struct {
	Log Log
	DB  DB
}

func LoadMigrations() (*Migrations, error) {
	cfg := &Migrations{Log: loadLog(), DB: loadDB()}

	if cfg.DB.URL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	return cfg, nil
}

func Load() (*Config, error) {
	cfg := &Config{
		App: App{
			Env:         env("APP_ENV", "development"),
			Mode:        Mode(strings.ToLower(env("APP_MODE", string(ModeAll)))),
			FrontendURL: strings.TrimRight(env("FRONTEND_URL", "http://localhost:5173"), "/"),
		},
		HTTP: HTTP{
			Port:            env("PORT", env("HTTP_PORT", "8080")),
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
			MaxBodyBytes:    int64(envInt("MAX_BODY_BYTES", 64*1024)),
			TrustedProxies:  envList("TRUSTED_PROXIES", nil),
		},
		Log: loadLog(),
		DB:  loadDB(),
		CORS: CORS{
			AllowedOrigins: envList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		},
		Auth: Auth{
			JWTSecret:    env("JWT_SECRET", ""),
			MinSecretLen: envInt("JWT_SECRET_MIN_LEN", defaultMinJWTSecretLen),
			Issuer:       env("JWT_ISSUER", "activity-events-api"),
			AccessTTL:    envDuration("JWT_ACCESS_TTL", 15*time.Minute),
			RefreshTTL:   envDuration("JWT_REFRESH_TTL", 30*day),
			BcryptCost:   envInt("BCRYPT_COST", 12),
		},
		Aggregation: Aggregation{
			Bucket:   envDuration("AGGREGATION_BUCKET", defaultAggregationBucket),
			Tick:     envDuration("AGGREGATION_TICK", defaultAggregationTick),
			Backfill: envInt("AGGREGATION_BACKFILL", defaultAggregationBackfill),
		},
		Google: Google{
			ClientID:     env("GOOGLE_CLIENT_ID", ""),
			ClientSecret: env("GOOGLE_CLIENT_SECRET", ""),
			RedirectURL:  env("GOOGLE_REDIRECT_URL", ""),
			AuthURL:      env("GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/v2/auth"),
			TokenURL:     env("GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token"),
			JWKSURL:      env("GOOGLE_JWKS_URL", "https://www.googleapis.com/oauth2/v3/certs"),
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
		return fmt.Errorf("config: unknown APP_MODE %q (expected api, worker or all)", c.App.Mode)
	}

	if c.DB.URL == "" {
		return fmt.Errorf("config: DATABASE_URL is required")
	}
	if c.DB.MinConns > c.DB.MaxConns {
		return fmt.Errorf("config: DB_MIN_CONNS (%d) is greater than DB_MAX_CONNS (%d)", c.DB.MinConns, c.DB.MaxConns)
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: SHUTDOWN_TIMEOUT must be positive")
	}

	switch {
	case c.Aggregation.Bucket <= 0:
		return fmt.Errorf("config: AGGREGATION_BUCKET must be positive")
	case day%c.Aggregation.Bucket != 0:
		return fmt.Errorf("config: AGGREGATION_BUCKET (%s) must divide %s evenly", c.Aggregation.Bucket, day)
	case c.Aggregation.Tick <= 0:
		return fmt.Errorf("config: AGGREGATION_TICK must be positive")
	case c.Aggregation.Backfill <= 0:
		return fmt.Errorf("config: AGGREGATION_BACKFILL must be positive")
	}

	if !c.App.Mode.RunsAPI() {
		return nil
	}

	if c.HTTP.MaxBodyBytes <= 0 {
		return fmt.Errorf("config: MAX_BODY_BYTES must be positive")
	}
	for _, cidr := range c.HTTP.TrustedProxies {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			if _, addrErr := netip.ParseAddr(cidr); addrErr != nil {
				return fmt.Errorf("config: TRUSTED_PROXIES entry %q is not an ip or a cidr", cidr)
			}
		}
	}
	if _, err := url.ParseRequestURI(c.App.FrontendURL); err != nil {
		return fmt.Errorf("config: FRONTEND_URL %q is not a valid url", c.App.FrontendURL)
	}

	switch {
	case c.Google.ClientSecret != "" && c.Google.RedirectURL == "":
		return fmt.Errorf("config: GOOGLE_REDIRECT_URL is required when GOOGLE_CLIENT_SECRET is set")
	case c.Google.RedirectURL != "" && c.Google.ClientSecret == "":
		return fmt.Errorf("config: GOOGLE_CLIENT_SECRET is required when GOOGLE_REDIRECT_URL is set")
	case c.Google.CodeFlowEnabled():
		if _, err := url.ParseRequestURI(c.Google.RedirectURL); err != nil {
			return fmt.Errorf("config: GOOGLE_REDIRECT_URL %q is not a valid url", c.Google.RedirectURL)
		}
	}

	if c.Auth.MinSecretLen < defaultMinJWTSecretLen {
		return fmt.Errorf("config: JWT_SECRET_MIN_LEN must not be lower than %d", defaultMinJWTSecretLen)
	}
	if len(c.Auth.JWTSecret) < c.Auth.MinSecretLen {
		return fmt.Errorf("config: JWT_SECRET must be at least %d characters long", c.Auth.MinSecretLen)
	}
	if c.Auth.AccessTTL <= 0 {
		return fmt.Errorf("config: JWT_ACCESS_TTL must be positive")
	}
	if c.Auth.RefreshTTL <= c.Auth.AccessTTL {
		return fmt.Errorf("config: JWT_REFRESH_TTL (%s) must be greater than JWT_ACCESS_TTL (%s)",
			c.Auth.RefreshTTL, c.Auth.AccessTTL)
	}
	if c.Auth.BcryptCost < 4 || c.Auth.BcryptCost > 31 {
		return fmt.Errorf("config: BCRYPT_COST %d is out of range 4..31", c.Auth.BcryptCost)
	}

	return nil
}

func loadLog() Log {
	return Log{
		Level:  strings.ToLower(env("LOG_LEVEL", "info")),
		Format: strings.ToLower(env("LOG_FORMAT", "json")),
	}
}

func loadDB() DB {
	return DB{
		URL:            env("DATABASE_URL", ""),
		MaxConns:       int32(envInt("DB_MAX_CONNS", 10)),
		MinConns:       int32(envInt("DB_MIN_CONNS", 2)),
		MigrationsDir:  env("MIGRATIONS_DIR", "migrations"),
		ConnectRetries: envInt("DB_CONNECT_RETRIES", 10),
		ConnectBackoff: envDuration("DB_CONNECT_BACKOFF", time.Second),
	}
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
