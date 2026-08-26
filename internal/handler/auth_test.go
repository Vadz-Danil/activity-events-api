package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

func TestRegisterAndLoginTrimEmail(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		status  int
		wantErr bool
		email   string
	}{
		{"register trims the email", "/register", `{"email":"  User@Example.com ","password":"super-secret"}`, http.StatusCreated, false, "User@Example.com"},
		{"login trims the email", "/login", `{"email":"\tUser@Example.com\n","password":"super-secret"}`, http.StatusOK, false, "User@Example.com"},
		{"register rejects a broken address", "/register", `{"email":"not-an-email","password":"super-secret"}`, http.StatusBadRequest, true, ""},
		{"register rejects a padded broken address", "/register", `{"email":"   ","password":"super-secret"}`, http.StatusBadRequest, true, ""},
		{"register rejects a non-string email", "/register", `{"email":42,"password":"super-secret"}`, http.StatusBadRequest, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, svc := authEngine(t)

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body)))

			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.wantErr {
				require.Empty(t, svc.email, "the service must not be called on an invalid request")
				return
			}
			require.Equal(t, tt.email, svc.email)
		})
	}
}

func authEngine(t *testing.T) (*gin.Engine, *fakeAuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	svc := &fakeAuthService{}
	h := NewAuth(svc, zap.NewNop(), testFrontendURL, false)

	engine := gin.New()
	engine.POST("/register", h.Register)
	engine.POST("/login", h.Login)

	return engine, svc
}

const testFrontendURL = "http://localhost:5173"

var errUnexpected = errors.New("boom")

type fakeAuthService struct {
	email      string
	authURL    string
	callback   *service.GoogleCallback
	err        error
	redirectTo string
	code       string
}

func (f *fakeAuthService) session() *service.Session {
	return &service.Session{
		User:         &models.User{ID: 1, Email: models.NormalizeEmail(f.email), Role: models.RoleUser},
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Minute),
		ExpiresIn:    60,
	}
}

func (f *fakeAuthService) Register(_ context.Context, email, _ string, _ service.ClientMeta) (*service.Session, error) {
	f.email = email
	return f.session(), nil
}

func (f *fakeAuthService) Login(_ context.Context, email, _ string, _ service.ClientMeta) (*service.Session, error) {
	f.email = email
	return f.session(), nil
}

func (f *fakeAuthService) StartGoogleFlow(_ context.Context, redirectTo string) (string, error) {
	f.redirectTo = redirectTo
	if f.err != nil {
		return "", f.err
	}
	return f.authURL, nil
}

func (f *fakeAuthService) CompleteGoogleFlow(_ context.Context, code, _ string) (*service.GoogleCallback, error) {
	f.code = code
	if f.err != nil {
		return nil, f.err
	}
	return f.callback, nil
}

func (f *fakeAuthService) ExchangeGoogleCode(_ context.Context, code string, _ service.ClientMeta) (*service.Session, error) {
	f.code = code
	if f.err != nil {
		return nil, f.err
	}
	return f.session(), nil
}

func (f *fakeAuthService) Refresh(context.Context, string, service.ClientMeta) (*service.Session, error) {
	return f.session(), nil
}

func (f *fakeAuthService) Logout(context.Context, string) error { return nil }

func (f *fakeAuthService) User(context.Context, int64) (*models.User, error) {
	return &models.User{ID: 1, Email: "user@example.com", Role: models.RoleUser}, nil
}

func methodsEngine(t *testing.T, google bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.GET("/methods", NewAuth(&fakeAuthService{}, zap.NewNop(), testFrontendURL, google).Methods)

	return engine
}

func TestMethodsReportsGoogleOnlyWhenItIsConfigured(t *testing.T) {
	for _, tc := range []struct {
		name   string
		google bool
	}{
		{"configured", true},
		{"not configured", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			methodsEngine(t, tc.google).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/methods", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
			}

			var body methodsResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Google != tc.google {
				t.Errorf("google: got %v, want %v", body.Google, tc.google)
			}
			if !body.Password {
				t.Error("password sign-in must always be reported as available")
			}
		})
	}
}
