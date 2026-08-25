package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	googleScope       = "openid email profile"
	googleCodeTimeout = 10 * time.Second
)

func NewOAuthSecret() (string, error) { return randomToken() }

func HashOAuthSecret(secret string) []byte { return hashToken(secret) }

func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type GoogleExchanger struct {
	clientID     string
	clientSecret string
	redirectURL  string
	authURL      string
	tokenURL     string
	client       *http.Client
}

func NewGoogleExchanger(clientID, clientSecret, redirectURL, authURL, tokenURL string) *GoogleExchanger {
	return &GoogleExchanger{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		authURL:      authURL,
		tokenURL:     tokenURL,
		client:       &http.Client{Timeout: googleCodeTimeout},
	}
}

func (e *GoogleExchanger) AuthCodeURL(state, challenge string) string {
	q := url.Values{
		"client_id":             {e.clientID},
		"redirect_uri":          {e.redirectURL},
		"response_type":         {"code"},
		"scope":                 {googleScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"online"},
		"prompt":                {"select_account"},
	}
	return e.authURL + "?" + q.Encode()
}

func (e *GoogleExchanger) Exchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"client_id":     {e.clientID},
		"client_secret": {e.clientSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {e.redirectURL},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth: exchange authorization code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body struct {
		IDToken          string `json:"id_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("auth: decode token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK || body.Error != "" {
		return "", fmt.Errorf("%w: google returned %d %s %s",
			ErrInvalidGoogleToken, resp.StatusCode, body.Error, body.ErrorDescription)
	}
	if body.IDToken == "" {
		return "", fmt.Errorf("%w: token response has no id_token", ErrInvalidGoogleToken)
	}

	return body.IDToken, nil
}
