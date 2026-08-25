package service

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

func TestStartGoogleFlow(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	target, err := f.svc.StartGoogleFlow(context.Background(), "/dashboard")
	require.NoError(t, err)

	parsed, err := url.Parse(target)
	require.NoError(t, err)
	require.Equal(t, f.exchanger.state, parsed.Query().Get("state"))
	require.Equal(t, auth.PKCEChallenge(storedVerifier(t, f)), parsed.Query().Get("code_challenge"))

	require.Equal(t, 1, f.oauth.pendingStates())
}

func TestStartGoogleFlowRejectsForeignRedirect(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	for _, redirect := range []string{"https://evil.example.com/steal", "//evil.example.com", "dashboard"} {
		_, err := f.svc.StartGoogleFlow(context.Background(), redirect)
		require.NoError(t, err)
	}

	for _, state := range f.oauth.states {
		require.Nil(t, state.RedirectTo)
	}
}

func TestStartGoogleFlowWithoutCodeFlow(t *testing.T) {
	f := newAuthFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	_, err := f.svc.StartGoogleFlow(context.Background(), "")
	require.ErrorIs(t, err, ErrGoogleDisabled)

	_, err = f.svc.CompleteGoogleFlow(context.Background(), "code", "state")
	require.ErrorIs(t, err, ErrGoogleDisabled)
}

func TestCompleteGoogleFlow(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	_, err := f.svc.StartGoogleFlow(context.Background(), "/dashboard")
	require.NoError(t, err)

	state, verifier := f.exchanger.state, storedVerifier(t, f)

	callback, err := f.svc.CompleteGoogleFlow(context.Background(), "google-code", state)
	require.NoError(t, err)
	require.NotEmpty(t, callback.Code)
	require.Equal(t, "/dashboard", callback.RedirectTo)

	require.Equal(t, "google-code", f.exchanger.code)
	require.Equal(t, verifier, f.exchanger.verifier)
	require.Len(t, f.users.rows, 1)
	require.Equal(t, 0, f.oauth.pendingStates(), "a state must not survive its callback")

	session, err := f.svc.ExchangeGoogleCode(context.Background(), callback.Code, testMeta)
	require.NoError(t, err)
	require.True(t, session.User.HasGoogle())
	require.NotEmpty(t, session.AccessToken)
	require.NotEmpty(t, session.RefreshToken)

	_, err = f.svc.ExchangeGoogleCode(context.Background(), callback.Code, testMeta)
	require.ErrorIs(t, err, ErrInvalidExchangeCode, "an exchange code works exactly once")
}

func TestCompleteGoogleFlowRejects(t *testing.T) {
	t.Run("unknown state", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

		_, err := f.svc.CompleteGoogleFlow(context.Background(), "google-code", "never-issued")
		require.ErrorIs(t, err, ErrInvalidOAuthState)
	})

	t.Run("replayed state", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

		_, err := f.svc.StartGoogleFlow(context.Background(), "")
		require.NoError(t, err)
		state := f.exchanger.state

		_, err = f.svc.CompleteGoogleFlow(context.Background(), "google-code", state)
		require.NoError(t, err)

		_, err = f.svc.CompleteGoogleFlow(context.Background(), "google-code", state)
		require.ErrorIs(t, err, ErrInvalidOAuthState)
	})

	t.Run("expired state", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

		_, err := f.svc.StartGoogleFlow(context.Background(), "")
		require.NoError(t, err)

		f.clock.Advance(googleStateTTL + time.Minute)

		_, err = f.svc.CompleteGoogleFlow(context.Background(), "google-code", f.exchanger.state)
		require.ErrorIs(t, err, ErrInvalidOAuthState)
	})

	t.Run("google refuses the code", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})
		f.exchanger.err = errors.New("invalid_grant")

		_, err := f.svc.StartGoogleFlow(context.Background(), "")
		require.NoError(t, err)

		_, err = f.svc.CompleteGoogleFlow(context.Background(), "google-code", f.exchanger.state)
		require.ErrorIs(t, err, ErrInvalidGoogleToken)
	})

	t.Run("unverified email", func(t *testing.T) {
		claims := googleUser("google-1", "user@example.com")
		claims.EmailVerified = false

		f := newCodeFlowFixture(t, &fakeGoogle{claims: claims})

		_, err := f.svc.StartGoogleFlow(context.Background(), "")
		require.NoError(t, err)

		_, err = f.svc.CompleteGoogleFlow(context.Background(), "google-code", f.exchanger.state)
		require.ErrorIs(t, err, ErrGoogleEmailUnverified)
	})

	t.Run("expired exchange code", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

		_, err := f.svc.StartGoogleFlow(context.Background(), "")
		require.NoError(t, err)

		callback, err := f.svc.CompleteGoogleFlow(context.Background(), "google-code", f.exchanger.state)
		require.NoError(t, err)

		f.clock.Advance(googleCodeTTL + time.Second)

		_, err = f.svc.ExchangeGoogleCode(context.Background(), callback.Code, testMeta)
		require.ErrorIs(t, err, ErrInvalidExchangeCode)
	})
}

func storedVerifier(t *testing.T, f authFixture) string {
	t.Helper()

	f.oauth.mu.Lock()
	defer f.oauth.mu.Unlock()

	var state models.GoogleOAuthState
	require.Len(t, f.oauth.states, 1)
	for _, s := range f.oauth.states {
		state = s
	}
	return state.CodeVerifier
}
