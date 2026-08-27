package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

const testRefreshTTL = 30 * 24 * time.Hour

var testMeta = ClientMeta{UserAgent: "go-test", IP: "203.0.113.7"}

type authFixture struct {
	svc       *Auth
	users     *fakeUsers
	tokens    *fakeTokens
	oauth     *fakeOAuth
	exchanger *fakeExchanger
	clock     *testClock
}

func newAuthFixture(t *testing.T, google GoogleVerifier) authFixture {
	t.Helper()
	return newFixture(t, google, nil)
}

func newCodeFlowFixture(t *testing.T, google GoogleVerifier) authFixture {
	t.Helper()
	return newFixture(t, google, &fakeExchanger{idToken: "id-token"})
}

func newFixture(t *testing.T, google GoogleVerifier, exchanger *fakeExchanger) authFixture {
	t.Helper()

	jwtManager, err := auth.NewManager("secret-long-enough-for-hs256-signing", "test", 15*time.Minute)
	require.NoError(t, err)

	tokens := newFakeTokens()
	users, oauth := newFakeUsers(tokens), newFakeOAuth()
	clock := &testClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)}

	deps := AuthDeps{
		Users:      users,
		Tokens:     tokens,
		OAuth:      oauth,
		JWT:        jwtManager,
		Google:     google,
		Logger:     zap.NewNop(),
		BcryptCost: bcrypt.MinCost,
		RefreshTTL: testRefreshTTL,
		Now:        clock.Now,
	}
	if exchanger != nil {
		deps.Exchanger = exchanger
	}

	svc, err := NewAuth(deps)
	require.NoError(t, err)

	return authFixture{svc: svc, users: users, tokens: tokens, oauth: oauth, exchanger: exchanger, clock: clock}
}

func TestRegisterCreatesUserAndSession(t *testing.T) {
	f := newAuthFixture(t, nil)

	session, err := f.svc.Register(context.Background(), "  User@Example.COM ", "super-secret", testMeta)
	require.NoError(t, err)

	require.NotEmpty(t, session.AccessToken)
	require.NotEmpty(t, session.RefreshToken)
	require.Equal(t, int(15*time.Minute/time.Second), session.ExpiresIn)
	require.Equal(t, "user@example.com", session.User.Email)
	require.Equal(t, models.RoleUser, session.User.Role)
	require.True(t, session.User.HasPassword())
	require.False(t, session.User.HasGoogle())

	stored := f.tokens.all()
	require.Len(t, stored, 1)
	require.Equal(t, auth.HashRefreshToken(session.RefreshToken), stored[0].TokenHash)
	require.NotEqual(t, session.RefreshToken, string(stored[0].TokenHash))
	require.Equal(t, f.clock.Now().Add(testRefreshTTL), stored[0].ExpiresAt)
	require.Equal(t, "203.0.113.7", stored[0].IP.String())
}

func TestRegisterRejectsBadCredentials(t *testing.T) {
	f := newAuthFixture(t, nil)

	_, err := f.svc.Register(context.Background(), "user@example.com", "short", testMeta)
	require.ErrorIs(t, err, auth.ErrPasswordTooShort)

	_, err = f.svc.Register(context.Background(), "user@example.com", strings.Repeat("p", auth.MaxPasswordLen+1), testMeta)
	require.ErrorIs(t, err, auth.ErrPasswordTooLong)

	_, err = f.svc.Register(context.Background(), "   ", "super-secret", testMeta)
	require.ErrorIs(t, err, ErrInvalidEmail)

	longEmail := strings.Repeat("a", models.MaxEmailLen) + "@example.com"
	_, err = f.svc.Register(context.Background(), longEmail, "super-secret", testMeta)
	require.ErrorIs(t, err, ErrInvalidEmail)

	require.Empty(t, f.users.rows)

	_, err = f.svc.Login(context.Background(), longEmail, "super-secret", testMeta)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	f := newAuthFixture(t, nil)

	_, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	_, err = f.svc.Register(context.Background(), "USER@example.com", "another-secret", testMeta)
	require.ErrorIs(t, err, ErrEmailTaken)
}

func TestLogin(t *testing.T) {
	f := newAuthFixture(t, nil)

	_, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	t.Run("successful login", func(t *testing.T) {
		session, err := f.svc.Login(context.Background(), "User@Example.com", "super-secret", testMeta)
		require.NoError(t, err)
		require.NotEmpty(t, session.AccessToken)
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := f.svc.Login(context.Background(), "user@example.com", "wrong", testMeta)
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})

	t.Run("unknown email returns the same error", func(t *testing.T) {
		_, err := f.svc.Login(context.Background(), "nobody@example.com", "super-secret", testMeta)
		require.ErrorIs(t, err, ErrInvalidCredentials)
	})
}

func signInWithGoogle(t *testing.T, f authFixture) *Session {
	t.Helper()

	callback := completeGoogleFlow(t, f)

	session, err := f.svc.ExchangeGoogleCode(context.Background(), callback.Code, testMeta)
	require.NoError(t, err)

	return session
}

func completeGoogleFlow(t *testing.T, f authFixture) *GoogleCallback {
	t.Helper()

	callback, err := startAndComplete(f)
	require.NoError(t, err)

	return callback
}

func startAndComplete(f authFixture) (*GoogleCallback, error) {
	if _, err := f.svc.StartGoogleFlow(context.Background(), ""); err != nil {
		return nil, err
	}
	return f.svc.CompleteGoogleFlow(context.Background(), "google-code", f.exchanger.state)
}

func TestLoginRejectsGoogleOnlyAccount(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	signInWithGoogle(t, f)

	_, err := f.svc.Login(context.Background(), "user@example.com", "whatever", testMeta)
	require.ErrorIs(t, err, ErrPasswordLoginUnavailable)
}

func TestGoogleSignInCreatesAccountOnce(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	first := signInWithGoogle(t, f)
	require.True(t, first.User.HasGoogle())
	require.False(t, first.User.HasPassword())
	require.True(t, first.User.EmailVerified)
	require.Equal(t, "Google User", *first.User.Name)

	second := signInWithGoogle(t, f)
	require.Equal(t, first.User.ID, second.User.ID)
	require.Len(t, f.users.rows, 1)
}

func TestGoogleSignInLinksExistingAccount(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "User@Example.com")})

	registered, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	session := signInWithGoogle(t, f)

	require.Equal(t, registered.User.ID, session.User.ID)
	require.True(t, session.User.HasGoogle())
	require.True(t, session.User.EmailVerified)
	require.False(t, session.User.HasPassword())
	require.Len(t, f.users.rows, 1)

	_, err = f.svc.Login(context.Background(), "user@example.com", "super-secret", testMeta)
	require.ErrorIs(t, err, ErrPasswordLoginUnavailable)
}

func TestGoogleSignInRevokesSessionsOfTheLinkedAccount(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	planted, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	signInWithGoogle(t, f)

	_, err = f.svc.Refresh(context.Background(), planted.RefreshToken, testMeta)
	require.ErrorIs(t, err, ErrInvalidRefreshToken, "a session opened before the link must not survive it")
}

func TestGoogleSignInKeepsPasswordOfVerifiedAccount(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	created, err := f.users.Create(context.Background(), models.User{
		Email:         "user@example.com",
		PasswordHash:  strPtr(hashFor(t, "super-secret")),
		Role:          models.RoleUser,
		EmailVerified: true,
	})
	require.NoError(t, err)

	session := signInWithGoogle(t, f)
	require.Equal(t, created.ID, session.User.ID)
	require.True(t, session.User.HasGoogle())
	require.True(t, session.User.HasPassword())

	_, err = f.svc.Login(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)
}

func TestGoogleSignInSurvivesConcurrentRegistration(t *testing.T) {
	f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-1", "user@example.com")})

	f.users.beforeCreate = func() {
		_, err := f.users.Create(context.Background(), models.User{
			Email:        "user@example.com",
			PasswordHash: strPtr("$2a$04$irrelevant"),
			Role:         models.RoleUser,
		})
		require.NoError(t, err)
	}

	session := signInWithGoogle(t, f)
	require.True(t, session.User.HasGoogle())
	require.Len(t, f.users.rows, 1)
}

func TestGoogleSignInRejects(t *testing.T) {
	t.Run("google is not configured", func(t *testing.T) {
		f := newAuthFixture(t, nil)

		_, err := f.svc.StartGoogleFlow(context.Background(), "")
		require.ErrorIs(t, err, ErrGoogleDisabled)
	})

	t.Run("unverified email", func(t *testing.T) {
		claims := googleUser("google-1", "user@example.com")
		claims.EmailVerified = false

		f := newCodeFlowFixture(t, &fakeGoogle{claims: claims})

		_, err := startAndComplete(f)
		require.ErrorIs(t, err, ErrGoogleEmailUnverified)
	})

	t.Run("invalid id-token", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{err: errors.New("bad signature")})

		_, err := startAndComplete(f)
		require.ErrorIs(t, err, ErrInvalidGoogleToken)
	})

	t.Run("email already linked to another google account", func(t *testing.T) {
		f := newCodeFlowFixture(t, &fakeGoogle{claims: googleUser("google-2", "user@example.com")})

		_, err := f.users.Create(context.Background(), models.User{
			Email:         "user@example.com",
			Role:          models.RoleUser,
			GoogleSub:     strPtr("google-1"),
			EmailVerified: true,
		})
		require.NoError(t, err)

		_, err = startAndComplete(f)
		require.ErrorIs(t, err, ErrInvalidGoogleToken)
	})
}

func TestRefreshRotatesToken(t *testing.T) {
	f := newAuthFixture(t, nil)

	first, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	f.clock.Advance(time.Hour)

	second, err := f.svc.Refresh(context.Background(), first.RefreshToken, testMeta)
	require.NoError(t, err)
	require.NotEqual(t, first.RefreshToken, second.RefreshToken)
	require.NotEmpty(t, second.AccessToken)

	rows := f.tokens.all()
	require.Len(t, rows, 2)

	var old, fresh models.RefreshToken
	for _, row := range rows {
		if row.RevokedAt != nil {
			old = row
		} else {
			fresh = row
		}
	}
	require.Equal(t, old.FamilyID, fresh.FamilyID)
	require.NotNil(t, old.ReplacedBy)
	require.Equal(t, fresh.ID, *old.ReplacedBy)

	require.Equal(t, auth.HashRefreshToken(second.RefreshToken), fresh.TokenHash)
	require.Equal(t, f.clock.Now().Add(testRefreshTTL), fresh.ExpiresAt)

	f.clock.Advance(time.Hour)

	third, err := f.svc.Refresh(context.Background(), second.RefreshToken, testMeta)
	require.NoError(t, err)
	require.NotEqual(t, second.RefreshToken, third.RefreshToken)
}

func TestRefreshDetectsTokenReuse(t *testing.T) {
	f := newAuthFixture(t, nil)

	first, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	second, err := f.svc.Refresh(context.Background(), first.RefreshToken, testMeta)
	require.NoError(t, err)

	_, err = f.svc.Refresh(context.Background(), first.RefreshToken, testMeta)
	require.ErrorIs(t, err, ErrInvalidRefreshToken)

	_, err = f.svc.Refresh(context.Background(), second.RefreshToken, testMeta)
	require.ErrorIs(t, err, ErrInvalidRefreshToken, "a live token of the family must be revoked together with the stolen one")

	for _, row := range f.tokens.all() {
		require.NotNil(t, row.RevokedAt)
	}
}

func TestRefreshRejects(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		f := newAuthFixture(t, nil)

		_, err := f.svc.Refresh(context.Background(), "made-up-token", testMeta)
		require.ErrorIs(t, err, ErrInvalidRefreshToken)
	})

	t.Run("expired token", func(t *testing.T) {
		f := newAuthFixture(t, nil)

		session, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
		require.NoError(t, err)

		f.clock.Advance(testRefreshTTL + time.Minute)

		_, err = f.svc.Refresh(context.Background(), session.RefreshToken, testMeta)
		require.ErrorIs(t, err, ErrInvalidRefreshToken)
	})
}

func TestLogoutRevokesFamily(t *testing.T) {
	f := newAuthFixture(t, nil)

	first, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	second, err := f.svc.Refresh(context.Background(), first.RefreshToken, testMeta)
	require.NoError(t, err)

	require.NoError(t, f.svc.Logout(context.Background(), second.RefreshToken))

	_, err = f.svc.Refresh(context.Background(), second.RefreshToken, testMeta)
	require.ErrorIs(t, err, ErrInvalidRefreshToken)
	require.NoError(t, f.svc.Logout(context.Background(), second.RefreshToken))
	require.NoError(t, f.svc.Logout(context.Background(), "made-up-token"))
}

func TestLogoutKeepsOtherDevices(t *testing.T) {
	f := newAuthFixture(t, nil)

	_, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	phone, err := f.svc.Login(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	laptop, err := f.svc.Login(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	require.NoError(t, f.svc.Logout(context.Background(), phone.RefreshToken))

	_, err = f.svc.Refresh(context.Background(), laptop.RefreshToken, testMeta)
	require.NoError(t, err)
}

func TestUser(t *testing.T) {
	f := newAuthFixture(t, nil)

	session, err := f.svc.Register(context.Background(), "user@example.com", "super-secret", testMeta)
	require.NoError(t, err)

	user, err := f.svc.User(context.Background(), session.User.ID)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", user.Email)

	_, err = f.svc.User(context.Background(), 999)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func googleUser(sub, email string) *auth.GoogleClaims {
	return &auth.GoogleClaims{
		Subject:       sub,
		Email:         email,
		EmailVerified: true,
		Name:          "Google User",
	}
}

func strPtr(s string) *string { return &s }

func hashFor(t *testing.T, password string) string {
	t.Helper()

	hash, err := auth.HashPassword(password, bcrypt.MinCost)
	require.NoError(t, err)
	return hash
}

func TestAccountsClampsTheLimit(t *testing.T) {
	tests := []struct {
		name string
		ask  int
		want int
	}{
		{"zero falls back to the default", 0, DefaultAccountsLimit},
		{"a negative number falls back too", -5, DefaultAccountsLimit},
		{"a sane number is passed through", 10, 10},
		{"an oversized ask is capped", MaxAccountsLimit * 3, MaxAccountsLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAuthFixture(t, nil)

			_, err := f.svc.Accounts(context.Background(), tt.ask)
			require.NoError(t, err)
			require.Equal(t, tt.want, f.users.lastLimit)
		})
	}
}
