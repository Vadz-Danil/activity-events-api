package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

func TestGoogleStartRedirectsToGoogle(t *testing.T) {
	engine, svc := oauthEngine(t)
	svc.authURL = "https://accounts.google.com/o/oauth2/v2/auth?client_id=test"

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/google/start?redirect_to=/dashboard", nil))

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, svc.authURL, rec.Header().Get("Location"))
	require.Equal(t, "/dashboard", svc.redirectTo)
}

func TestGoogleCallbackRedirectsWithExchangeCode(t *testing.T) {
	engine, svc := oauthEngine(t)
	svc.callback = &service.GoogleCallback{Code: "one-time-code", RedirectTo: "/dashboard"}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/google/callback?code=google-code&state=state", nil))

	require.Equal(t, http.StatusFound, rec.Code)

	target, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, testFrontendURL+frontendCallbackPath, target.Scheme+"://"+target.Host+target.Path)
	require.Equal(t, "one-time-code", target.Query().Get("code"))
	require.Equal(t, "/dashboard", target.Query().Get("redirect_to"))
	require.Equal(t, "google-code", svc.code)
}

func TestGoogleCallbackRedirectsWithError(t *testing.T) {
	tests := []struct {
		name  string
		query string
		err   error
		want  string
	}{
		{"user denied consent", "?error=access_denied", nil, "access_denied"},
		{"replayed state", "?code=c&state=s", service.ErrInvalidOAuthState, "invalid_state"},
		{"unverified google email", "?code=c&state=s", service.ErrGoogleEmailUnverified, "email_unverified"},
		{"anything else", "?code=c&state=s", errUnexpected, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, svc := oauthEngine(t)
			svc.err = tt.err

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/google/callback"+tt.query, nil))

			require.Equal(t, http.StatusFound, rec.Code)

			target, err := url.Parse(rec.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, tt.want, target.Query().Get("error"))
			require.Empty(t, target.Query().Get("code"))
		})
	}
}

func TestGoogleExchange(t *testing.T) {
	engine, svc := oauthEngine(t)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/google/exchange",
		strings.NewReader(`{"code":"one-time-code"}`)))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "one-time-code", svc.code)

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/google/exchange", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	engine, svc = oauthEngine(t)
	svc.err = service.ErrInvalidExchangeCode

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/google/exchange",
		strings.NewReader(`{"code":"already-used"}`)))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func oauthEngine(t *testing.T) (*gin.Engine, *fakeAuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	svc := &fakeAuthService{}
	h := NewAuth(svc, zap.NewNop(), testFrontendURL)

	engine := gin.New()
	engine.GET("/google/start", h.GoogleStart)
	engine.GET("/google/callback", h.GoogleCallback)
	engine.POST("/google/exchange", h.GoogleExchange)

	return engine, svc
}
