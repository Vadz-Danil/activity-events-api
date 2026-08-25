package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

const testSecret = "secret-long-enough-for-hs256-signing"

func TestManagerIssueAndParse(t *testing.T) {
	m, err := NewManager(testSecret, "activity-events-api", 15*time.Minute)
	require.NoError(t, err)

	now := time.Now()
	access, err := m.Issue(42, models.RoleAdmin, now)
	require.NoError(t, err)
	require.WithinDuration(t, now.Add(15*time.Minute), access.ExpiresAt, time.Second)

	claims, err := m.Parse(access.Token)
	require.NoError(t, err)

	userID, err := claims.UserID()
	require.NoError(t, err)
	require.Equal(t, int64(42), userID)
	require.Equal(t, models.RoleAdmin, claims.Role)
}

func TestManagerParseRejects(t *testing.T) {
	const issuer = "activity-events-api"

	m, err := NewManager(testSecret, issuer, 15*time.Minute)
	require.NoError(t, err)

	other, err := NewManager("another-secret-long-enough-for-hs256", issuer, 15*time.Minute)
	require.NoError(t, err)

	foreignIssuer, err := NewManager(testSecret, "someone-else", 15*time.Minute)
	require.NoError(t, err)

	expired, err := NewManager(testSecret, issuer, time.Minute)
	require.NoError(t, err)

	expiredToken, err := expired.Issue(1, models.RoleUser, time.Now().Add(-2*time.Minute))
	require.NoError(t, err)

	foreignSecretToken, err := other.Issue(1, models.RoleUser, time.Now())
	require.NoError(t, err)

	foreignIssuerToken, err := foreignIssuer.Issue(1, models.RoleUser, time.Now())
	require.NoError(t, err)

	tests := map[string]string{
		"foreign secret":           foreignSecretToken.Token,
		"foreign issuer":           foreignIssuerToken.Token,
		"expired":                  expiredToken.Token,
		"unsigned":                 unsignedToken(t, issuer),
		"unknown role":             tokenWithRole(t, issuer, "superuser"),
		"garbage instead of a jwt": "not-a-token",
		"empty string":             "",
	}

	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := m.Parse(token)
			require.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}

func unsignedToken(t *testing.T, issuer string) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{
		Role: models.RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	raw, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	return raw
}

func tokenWithRole(t *testing.T, issuer, role string) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		Role: models.Role(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	raw, err := token.SignedString([]byte(testSecret))
	require.NoError(t, err)
	return raw
}

func TestNewManagerValidatesArgs(t *testing.T) {
	_, err := NewManager("", "issuer", time.Minute)
	require.Error(t, err)

	_, err = NewManager(testSecret, "issuer", 0)
	require.Error(t, err)
}
