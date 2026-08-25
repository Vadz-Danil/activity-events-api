package service

import "errors"

var (
	ErrEmailTaken               = errors.New("service: email is already registered")
	ErrInvalidEmail             = errors.New("service: email is empty or too long")
	ErrInvalidCredentials       = errors.New("service: invalid email or password")
	ErrPasswordLoginUnavailable = errors.New("service: password login is not available for this account")
	ErrInvalidRefreshToken      = errors.New("service: refresh token is invalid or expired")
	ErrInvalidGoogleToken       = errors.New("service: invalid google id-token")
	ErrGoogleDisabled           = errors.New("service: google sign-in is not configured")
	ErrGoogleEmailUnverified    = errors.New("service: google has not verified this email")
	ErrInvalidOAuthState        = errors.New("service: oauth state is invalid or expired")
	ErrInvalidExchangeCode      = errors.New("service: exchange code is invalid, expired or already used")
	ErrUserNotFound             = errors.New("service: user not found")
)
