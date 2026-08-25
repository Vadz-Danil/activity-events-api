package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadMigrations(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://app:app@localhost:5432/activity?sslmode=disable")
	t.Setenv("MIGRATIONS_DIR", "db/migrations")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("APP_MODE", "")

	cfg, err := LoadMigrations()
	require.NoError(t, err)
	require.Equal(t, "db/migrations", cfg.DB.MigrationsDir)
	require.Equal(t, "info", cfg.Log.Level)

	t.Setenv("DATABASE_URL", "")
	_, err = LoadMigrations()
	require.Error(t, err)
}

func TestLoadValidatesAuthPerMode(t *testing.T) {
	const strongSecret = "0123456789abcdef0123456789abcdef"

	tests := []struct {
		name      string
		mode      string
		secret    string
		accessTTL string
		wantErr   bool
	}{
		{"worker without a secret", "worker", "", "", false},
		{"api without a secret", "api", "", "", true},
		{"api with a short secret", "api", "too-short", "", true},
		{"api with a strong secret", "api", strongSecret, "", false},
		{"all with a strong secret", "all", strongSecret, "", false},
		{"access ttl longer than refresh ttl", "api", strongSecret, "1000h", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://app:app@localhost:5432/activity?sslmode=disable")
			t.Setenv("APP_MODE", tt.mode)
			t.Setenv("JWT_SECRET", tt.secret)
			t.Setenv("JWT_ACCESS_TTL", tt.accessTTL)

			cfg, err := Load()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, Mode(tt.mode), cfg.App.Mode)
			require.Equal(t, 30*24*time.Hour, cfg.Auth.RefreshTTL)
		})
	}
}
