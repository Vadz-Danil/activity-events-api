package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/response"
)

func TestRequireAuth(t *testing.T) {
	engine, manager := guardedEngine(t)

	token, err := manager.Issue(7, models.RoleUser, time.Now())
	require.NoError(t, err)

	expired, err := manager.Issue(7, models.RoleUser, time.Now().Add(-time.Hour))
	require.NoError(t, err)

	tests := []struct {
		name   string
		header string
		status int
		code   string
	}{
		{"no header", "", http.StatusUnauthorized, response.CodeUnauthorized},
		{"wrong scheme", "Basic dXNlcjpwYXNz", http.StatusUnauthorized, response.CodeUnauthorized},
		{"empty token", "Bearer   ", http.StatusUnauthorized, response.CodeUnauthorized},
		{"garbage instead of a token", "Bearer not-a-token", http.StatusUnauthorized, response.CodeInvalidToken},
		{"expired token", "Bearer " + expired.Token, http.StatusUnauthorized, response.CodeInvalidToken},
		{"valid token", "Bearer " + token.Token, http.StatusOK, ""},
		{"scheme in a different case", "bearer " + token.Token, http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(engine, http.MethodGet, "/private", tt.header)
			require.Equal(t, tt.status, rec.Code)

			if tt.code != "" {
				require.Equal(t, tt.code, errorCode(t, rec))
				return
			}

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, float64(7), body["user_id"])
			require.Equal(t, string(models.RoleUser), body["role"])
		})
	}
}

func TestRequireRole(t *testing.T) {
	engine, manager := guardedEngine(t)

	user, err := manager.Issue(7, models.RoleUser, time.Now())
	require.NoError(t, err)

	admin, err := manager.Issue(8, models.RoleAdmin, time.Now())
	require.NoError(t, err)

	t.Run("wrong role", func(t *testing.T) {
		rec := do(engine, http.MethodGet, "/admin", "Bearer "+user.Token)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, response.CodeForbidden, errorCode(t, rec))
	})

	t.Run("required role", func(t *testing.T) {
		rec := do(engine, http.MethodGet, "/admin", "Bearer "+admin.Token)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("route without token check", func(t *testing.T) {
		rec := do(engine, http.MethodGet, "/misconfigured", "Bearer "+admin.Token)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func guardedEngine(t *testing.T) (*gin.Engine, *auth.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	manager, err := auth.NewManager("secret-long-enough-for-hs256-signing", "test", 15*time.Minute)
	require.NoError(t, err)

	guard := NewGuard(manager)
	ok := func(c *gin.Context) {
		id, _ := UserIDFrom(c)
		role, _ := RoleFrom(c)
		c.JSON(http.StatusOK, gin.H{"user_id": id, "role": role})
	}

	engine := gin.New()
	engine.GET("/private", guard.RequireAuth(), ok)
	engine.GET("/admin", guard.RequireAuth(), guard.RequireRole(models.RoleAdmin), ok)
	engine.GET("/misconfigured", guard.RequireRole(models.RoleAdmin), ok)

	return engine, manager
}

func do(engine *gin.Engine, method, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body response.ErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error.Code
}
