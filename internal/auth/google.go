package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	GoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

	jwksMinTTL      = time.Minute
	jwksMaxTTL      = 24 * time.Hour
	jwksFallbackTTL = time.Hour
	jwksTimeout     = 5 * time.Second
	jwksMinRefetch  = time.Minute
)

var googleIssuers = map[string]struct{}{
	"accounts.google.com":         {},
	"https://accounts.google.com": {},
}

var (
	ErrInvalidGoogleToken = errors.New("auth: invalid google id-token")
	errJWKSThrottled      = errors.New("auth: jwks refresh is rate limited")
)

type GoogleClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

type GoogleVerifier struct {
	clientID string
	jwksURL  string
	client   *http.Client
	now      func() time.Time

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	freshTill time.Time

	fetchMu   sync.Mutex
	nextFetch time.Time
}

func NewGoogleVerifier(clientID, jwksURL string) *GoogleVerifier {
	if jwksURL == "" {
		jwksURL = GoogleJWKSURL
	}
	return &GoogleVerifier{
		clientID: clientID,
		jwksURL:  jwksURL,
		client:   &http.Client{Timeout: jwksTimeout},
		now:      time.Now,
		keys:     make(map[string]*rsa.PublicKey),
	}
}

func (v *GoogleVerifier) Verify(ctx context.Context, raw string) (*GoogleClaims, error) {
	claims := &googleIDTokenClaims{}

	token, err := jwt.ParseWithClaims(raw, claims,
		func(t *jwt.Token) (any, error) { return v.keyForToken(ctx, t) },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithAudience(v.clientID),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("%w: %v", ErrInvalidGoogleToken, err)
	}

	if _, ok := googleIssuers[claims.Issuer]; !ok {
		return nil, fmt.Errorf("%w: unexpected issuer %q", ErrInvalidGoogleToken, claims.Issuer)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: empty sub", ErrInvalidGoogleToken)
	}

	return &GoogleClaims{
		Subject:       claims.Subject,
		Email:         strings.TrimSpace(claims.Email),
		EmailVerified: bool(claims.EmailVerified),
		Name:          strings.TrimSpace(claims.Name),
	}, nil
}

func (v *GoogleVerifier) keyForToken(ctx context.Context, token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("auth: google id-token header has no kid")
	}

	key, fresh := v.cachedKey(kid)
	if fresh {
		return key, nil
	}

	err := v.refreshKeys(ctx)

	if refreshed, ok := v.cachedKey(kid); ok {
		return refreshed, nil
	}
	switch {
	case err != nil && key != nil:
		return key, nil
	case err != nil:
		return nil, err
	default:
		return nil, fmt.Errorf("auth: google has no key with kid %q", kid)
	}
}

func (v *GoogleVerifier) cachedKey(kid string) (*rsa.PublicKey, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	key, ok := v.keys[kid]
	return key, ok && v.now().Before(v.freshTill)
}

func (v *GoogleVerifier) refreshKeys(ctx context.Context) error {
	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()

	if now := v.now(); now.Before(v.nextFetch) {
		return errJWKSThrottled
	}
	v.nextFetch = v.now().Add(jwksMinRefetch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("auth: build jwks request: %w", err)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: jwks returned status %d", resp.StatusCode)
	}

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("auth: decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kid == "" || k.Kty != "RSA" || (k.Alg != "" && k.Alg != jwt.SigningMethodRS256.Alg()) {
			continue
		}
		pub, err := rsaPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return errors.New("auth: jwks has no usable RSA keys")
	}

	v.mu.Lock()
	v.keys = keys
	v.freshTill = v.now().Add(jwksTTL(resp.Header.Get("Cache-Control")))
	v.mu.Unlock()

	return nil
}

func rsaPublicKey(nRaw, eRaw string) (*rsa.PublicKey, error) {
	n, err := decodeBase64URL(nRaw)
	if err != nil {
		return nil, fmt.Errorf("auth: key modulus: %w", err)
	}
	e, err := decodeBase64URL(eRaw)
	if err != nil {
		return nil, fmt.Errorf("auth: key exponent: %w", err)
	}
	if len(n) == 0 || len(e) == 0 || len(e) > 8 {
		return nil, errors.New("auth: malformed RSA key parameters")
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(n),
		E: int(new(big.Int).SetBytes(e).Int64()),
	}, nil
}

func decodeBase64URL(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func jwksTTL(cacheControl string) time.Duration {
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(part)
		value, ok := strings.CutPrefix(part, "max-age=")
		if !ok {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || seconds <= 0 {
			break
		}
		return min(max(time.Duration(seconds)*time.Second, jwksMinTTL), jwksMaxTTL)
	}
	return jwksFallbackTTL
}

type googleIDTokenClaims struct {
	Email         string   `json:"email"`
	EmailVerified flexBool `json:"email_verified"`
	Name          string   `json:"name"`
	jwt.RegisteredClaims
}

type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch strings.Trim(string(data), `"`) {
	case "true":
		*b = true
	case "false", "null", "":
		*b = false
	default:
		return fmt.Errorf("auth: email_verified has unexpected value %s", data)
	}
	return nil
}
