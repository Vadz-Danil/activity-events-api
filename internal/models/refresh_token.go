package models

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	ID         uuid.UUID
	FamilyID   uuid.UUID
	UserID     int64
	TokenHash  []byte
	UserAgent  *string
	IP         *netip.Addr
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	ReplacedBy *uuid.UUID
}

func (t *RefreshToken) Revoked() bool { return t.RevokedAt != nil }

func (t *RefreshToken) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

func ParseIP(ip string) *netip.Addr {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	return &addr
}
