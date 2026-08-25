package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLen = 8
	MaxPasswordLen = 72
)

var (
	ErrPasswordTooShort = fmt.Errorf("auth: password is shorter than %d bytes", MinPasswordLen)
	ErrPasswordTooLong  = fmt.Errorf("auth: password is longer than %d bytes", MaxPasswordLen)
)

func HashPassword(plain string, cost int) (string, error) {
	switch {
	case len(plain) < MinPasswordLen:
		return "", ErrPasswordTooShort
	case len(plain) > MaxPasswordLen:
		return "", ErrPasswordTooLong
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

func CheckPassword(hash, plain string) bool {
	if hash == "" || len(plain) > MaxPasswordLen {
		return false
	}
	return errors.Is(bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)), nil)
}
