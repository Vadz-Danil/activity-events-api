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
	ErrInvalidEventType         = errors.New("service: event type is empty or too long")
	ErrInvalidEventPayload      = errors.New("service: event payload must be a json object within the size limit")
	ErrEventTimeOutOfRange      = errors.New("service: occurred_at is too far in the future or in the past")
	ErrInvalidIdempotencyKey    = errors.New("service: idempotency key is too long")
	ErrDuplicateIdempotencyKey  = errors.New("service: idempotency key is repeated inside the batch")
	ErrEmptyBatch               = errors.New("service: batch has no events")
	ErrBatchTooLarge            = errors.New("service: batch is too large")
	ErrInvalidCursor            = errors.New("service: pagination cursor is invalid")
	ErrInvalidTimeRange         = errors.New("service: from must be before to")
	ErrBucketNotClosed          = errors.New("service: this bucket has not closed yet")
	ErrStatsRangeTooLarge       = errors.New("service: requested statistics window is too large")
)
