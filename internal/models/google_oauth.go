package models

import "time"

type GoogleOAuthState struct {
	StateHash    []byte
	CodeVerifier string
	RedirectTo   *string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

type GoogleOAuthCode struct {
	CodeHash  []byte
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}
