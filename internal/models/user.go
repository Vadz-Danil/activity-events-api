package models

import (
	"strings"
	"time"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

const MaxEmailLen = 254

func (r Role) Valid() bool { return r == RoleUser || r == RoleAdmin }

type User struct {
	ID            int64
	Email         string
	PasswordHash  *string
	Role          Role
	GoogleSub     *string
	Name          *string
	EmailVerified bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (u *User) HasPassword() bool { return u.PasswordHash != nil && *u.PasswordHash != "" }

func (u *User) HasGoogle() bool { return u.GoogleSub != nil && *u.GoogleSub != "" }

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
