package models

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	MaxEventTypeLen = 64
	MaxPayloadBytes = 8 * 1024
)

type Event struct {
	ID             int64
	UserID         int64
	Type           string
	Payload        json.RawMessage
	OccurredAt     time.Time
	CreatedAt      time.Time
	IdempotencyKey *string
}

func NormalizeEventType(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}
