package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testClientID = "123456.apps.googleusercontent.com"

var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

func TestGoogleVerifierAcceptsValidToken(t *testing.T) {
	verifier, _ := newTestVerifier(t)

	claims, err := verifier.Verify(context.Background(), signGoogleToken(t, "kid-1", googleClaims(nil)))
	require.NoError(t, err)

	require.Equal(t, "google-user-1", claims.Subject)
	require.Equal(t, "user@example.com", claims.Email)
	require.True(t, claims.EmailVerified)
	require.Equal(t, "Test User", claims.Name)
}

func TestGoogleVerifierRejects(t *testing.T) {
	tests := map[string]jwt.MapClaims{
		"wrong client_id": googleClaims(jwt.MapClaims{"aud": "someone-else.apps.googleusercontent.com"}),
		"wrong issuer":    googleClaims(jwt.MapClaims{"iss": "https://accounts.evil.com"}),
		"expired":         googleClaims(jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}),
		"missing exp":     googleClaims(jwt.MapClaims{"exp": nil}),
		"empty sub":       googleClaims(jwt.MapClaims{"sub": ""}),
	}

	for name, claims := range tests {
		t.Run(name, func(t *testing.T) {
			verifier, _ := newTestVerifier(t)

			_, err := verifier.Verify(context.Background(), signGoogleToken(t, "kid-1", claims))
			require.ErrorIs(t, err, ErrInvalidGoogleToken)
		})
	}
}

func TestGoogleVerifierRejectsForeignKey(t *testing.T) {
	verifier, _ := newTestVerifier(t)

	foreign, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, googleClaims(nil))
	token.Header["kid"] = "kid-1"
	raw, err := token.SignedString(foreign)
	require.NoError(t, err)

	_, err = verifier.Verify(context.Background(), raw)
	require.ErrorIs(t, err, ErrInvalidGoogleToken)
}

func TestGoogleVerifierAcceptsStringEmailVerified(t *testing.T) {
	verifier, _ := newTestVerifier(t)

	claims, err := verifier.Verify(context.Background(),
		signGoogleToken(t, "kid-1", googleClaims(jwt.MapClaims{"email_verified": "true"})))
	require.NoError(t, err)
	require.True(t, claims.EmailVerified)
}

func TestGoogleVerifierCachesKeys(t *testing.T) {
	verifier, hits := newTestVerifier(t)

	for range 3 {
		_, err := verifier.Verify(context.Background(), signGoogleToken(t, "kid-1", googleClaims(nil)))
		require.NoError(t, err)
	}

	require.Equal(t, int64(1), hits.Load())
}

func TestGoogleVerifierThrottlesRefetchOnUnknownKid(t *testing.T) {
	verifier, hits := newTestVerifier(t)

	now := time.Now()
	verifier.now = func() time.Time { return now }

	_, err := verifier.Verify(context.Background(), signGoogleToken(t, "kid-1", googleClaims(nil)))
	require.NoError(t, err)

	for range 5 {
		_, err = verifier.Verify(context.Background(), signGoogleToken(t, "kid-unknown", googleClaims(nil)))
		require.Error(t, err)
	}
	require.Equal(t, int64(1), hits.Load(), "a kid from an unverified header must not reach google on every request")

	now = now.Add(jwksMinRefetch + time.Second)

	_, err = verifier.Verify(context.Background(), signGoogleToken(t, "kid-unknown", googleClaims(nil)))
	require.Error(t, err)
	require.Equal(t, int64(2), hits.Load(), "after the interval a rotated key must still be picked up")
}

func TestGoogleVerifierFailsWhenJWKSUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	verifier := NewGoogleVerifier(testClientID, server.URL)

	_, err := verifier.Verify(context.Background(), signGoogleToken(t, "kid-1", googleClaims(nil)))
	require.ErrorIs(t, err, ErrInvalidGoogleToken)
}

func TestJWKSTTL(t *testing.T) {
	require.Equal(t, jwksFallbackTTL, jwksTTL(""))
	require.Equal(t, jwksFallbackTTL, jwksTTL("public, max-age=not-a-number"))
	require.Equal(t, 20487*time.Second, jwksTTL("public, max-age=20487, must-revalidate"))
	require.Equal(t, jwksMinTTL, jwksTTL("max-age=5"))
	require.Equal(t, jwksMaxTTL, jwksTTL("max-age=999999999"))
}

func newTestVerifier(t *testing.T) (*GoogleVerifier, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		pub := testKey().Public().(*rsa.PublicKey)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")

		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "kid-1",
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	}))
	t.Cleanup(server.Close)

	return NewGoogleVerifier(testClientID, server.URL), &hits
}

func googleClaims(override jwt.MapClaims) jwt.MapClaims {
	claims := jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"sub":            "google-user-1",
		"email":          "user@example.com",
		"email_verified": true,
		"name":           "Test User",
		"iat":            time.Now().Add(-time.Minute).Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	}

	for k, v := range override {
		if v == nil {
			delete(claims, k)
			continue
		}
		claims[k] = v
	}
	return claims
}

func signGoogleToken(t *testing.T, kid string, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	raw, err := token.SignedString(testKey())
	require.NoError(t, err)
	return raw
}
