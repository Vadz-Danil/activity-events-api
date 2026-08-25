package handler

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

type emailField string

func (e *emailField) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*e = emailField(strings.TrimSpace(raw))
	return nil
}

type registerRequest struct {
	Email    emailField `json:"email" binding:"required,email"`
	Password string     `json:"password" binding:"required"`
}

type loginRequest struct {
	Email    emailField `json:"email" binding:"required,email"`
	Password string     `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type exchangeRequest struct {
	Code string `json:"code" binding:"required"`
}

type eventRequest struct {
	Type           string          `json:"type" binding:"required"`
	Payload        json.RawMessage `json:"payload"`
	OccurredAt     *time.Time      `json:"occurred_at"`
	IdempotencyKey string          `json:"idempotency_key"`
}

func (r eventRequest) toInput() service.EventInput {
	return service.EventInput{
		Type:           r.Type,
		Payload:        r.Payload,
		OccurredAt:     r.OccurredAt,
		IdempotencyKey: r.IdempotencyKey,
	}
}

type batchRequest struct {
	Events []eventRequest `json:"events" binding:"required,dive"`
}

type eventResponse struct {
	ID             int64           `json:"id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	OccurredAt     time.Time       `json:"occurred_at"`
	CreatedAt      time.Time       `json:"created_at"`
	IdempotencyKey *string         `json:"idempotency_key,omitempty"`
}

type eventPageResponse struct {
	Items      []eventResponse `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type batchResponse struct {
	Created      []eventResponse `json:"created"`
	CreatedCount int             `json:"created_count"`
	Duplicates   int             `json:"duplicates"`
}

func newEventResponse(e models.Event) eventResponse {
	return eventResponse{
		ID:             e.ID,
		Type:           e.Type,
		Payload:        e.Payload,
		OccurredAt:     e.OccurredAt,
		CreatedAt:      e.CreatedAt,
		IdempotencyKey: e.IdempotencyKey,
	}
}

func newEventPageResponse(page *service.EventPage) eventPageResponse {
	items := make([]eventResponse, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, newEventResponse(e))
	}

	return eventPageResponse{Items: items, NextCursor: page.NextCursor}
}

func newBatchResponse(result *service.BatchResult) batchResponse {
	created := make([]eventResponse, 0, len(result.Created))
	for _, e := range result.Created {
		created = append(created, newEventResponse(e))
	}

	return batchResponse{Created: created, CreatedCount: len(created), Duplicates: result.Duplicates}
}

type sessionResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	TokenType    string       `json:"token_type"`
	ExpiresIn    int          `json:"expires_in"`
	User         userResponse `json:"user"`
}

type userResponse struct {
	ID            int64       `json:"id"`
	Email         string      `json:"email"`
	Role          models.Role `json:"role"`
	Name          *string     `json:"name"`
	EmailVerified bool        `json:"email_verified"`
	HasPassword   bool        `json:"has_password"`
	HasGoogle     bool        `json:"has_google"`
	CreatedAt     time.Time   `json:"created_at"`
}

func newSessionResponse(s *service.Session) sessionResponse {
	return sessionResponse{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    s.ExpiresIn,
		User:         newUserResponse(s.User),
	}
}

func newUserResponse(u *models.User) userResponse {
	return userResponse{
		ID:            u.ID,
		Email:         u.Email,
		Role:          u.Role,
		Name:          u.Name,
		EmailVerified: u.EmailVerified,
		HasPassword:   u.HasPassword(),
		HasGoogle:     u.HasGoogle(),
		CreatedAt:     u.CreatedAt,
	}
}
