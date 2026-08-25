package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

const (
	googleStateTTL = 10 * time.Minute
	googleCodeTTL  = time.Minute
)

type GoogleExchanger interface {
	AuthCodeURL(state, challenge string) string
	Exchange(ctx context.Context, code, verifier string) (string, error)
}

type GoogleOAuthRepository interface {
	CreateState(ctx context.Context, s models.GoogleOAuthState) error
	TakeState(ctx context.Context, hash []byte, now time.Time) (*models.GoogleOAuthState, error)
	CreateCode(ctx context.Context, c models.GoogleOAuthCode) error
	TakeCode(ctx context.Context, hash []byte, now time.Time) (int64, error)
}

type GoogleCallback struct {
	Code       string
	RedirectTo string
}

func (s *Auth) StartGoogleFlow(ctx context.Context, redirectTo string) (string, error) {
	if s.exchanger == nil || s.oauth == nil {
		return "", ErrGoogleDisabled
	}

	state, err := auth.NewOAuthSecret()
	if err != nil {
		return "", err
	}
	verifier, err := auth.NewOAuthSecret()
	if err != nil {
		return "", err
	}

	err = s.oauth.CreateState(ctx, models.GoogleOAuthState{
		StateHash:    auth.HashOAuthSecret(state),
		CodeVerifier: verifier,
		RedirectTo:   textPtr(safeRedirect(redirectTo)),
		ExpiresAt:    s.now().Add(googleStateTTL),
	})
	if err != nil {
		return "", err
	}

	return s.exchanger.AuthCodeURL(state, auth.PKCEChallenge(verifier)), nil
}

func (s *Auth) CompleteGoogleFlow(ctx context.Context, code, state string) (*GoogleCallback, error) {
	if s.exchanger == nil || s.oauth == nil {
		return nil, ErrGoogleDisabled
	}
	if code == "" || state == "" {
		return nil, ErrInvalidOAuthState
	}

	now := s.now()

	stored, err := s.oauth.TakeState(ctx, auth.HashOAuthSecret(state), now)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrInvalidOAuthState
	case err != nil:
		return nil, err
	}

	idToken, err := s.exchanger.Exchange(ctx, code, stored.CodeVerifier)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidGoogleToken, err)
	}

	claims, err := s.google.Verify(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidGoogleToken, err)
	}

	user, err := s.resolveGoogleUser(ctx, claims)
	if err != nil {
		return nil, err
	}

	oneTime, err := auth.NewOAuthSecret()
	if err != nil {
		return nil, err
	}

	err = s.oauth.CreateCode(ctx, models.GoogleOAuthCode{
		CodeHash:  auth.HashOAuthSecret(oneTime),
		UserID:    user.ID,
		ExpiresAt: now.Add(googleCodeTTL),
	})
	if err != nil {
		return nil, err
	}

	redirectTo := ""
	if stored.RedirectTo != nil {
		redirectTo = *stored.RedirectTo
	}

	return &GoogleCallback{Code: oneTime, RedirectTo: redirectTo}, nil
}

func (s *Auth) ExchangeGoogleCode(ctx context.Context, code string, meta ClientMeta) (*Session, error) {
	if s.oauth == nil {
		return nil, ErrGoogleDisabled
	}

	userID, err := s.oauth.TakeCode(ctx, auth.HashOAuthSecret(code), s.now())
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrInvalidExchangeCode
	case err != nil:
		return nil, err
	}

	user, err := s.users.ByID(ctx, userID)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrInvalidExchangeCode
	case err != nil:
		return nil, err
	}

	return s.startSession(ctx, user, meta)
}

func safeRedirect(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	return path
}
