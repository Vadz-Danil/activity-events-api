package repository

import "errors"

var (
	ErrNotFound            = errors.New("repository: record not found")
	ErrEmailTaken          = errors.New("repository: email is already registered")
	ErrGoogleSubTaken      = errors.New("repository: google account is already linked")
	ErrTokenAlreadyRotated = errors.New("repository: refresh token has already been used")
	ErrBucketLocked        = errors.New("repository: this bucket is already being aggregated")
)
