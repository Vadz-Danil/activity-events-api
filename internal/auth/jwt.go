package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

var ErrInvalidToken = errors.New("auth: invalid access token")

type Claims struct {
	Role models.Role `json:"role"`
	jwt.RegisteredClaims
}

func (c *Claims) UserID() (int64, error) {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidToken
	}
	return id, nil
}

type Access struct {
	Token     string
	ExpiresAt time.Time
}

type Manager struct {
	secret    []byte
	issuer    string
	accessTTL time.Duration
}

func NewManager(secret, issuer string, accessTTL time.Duration) (*Manager, error) {
	if secret == "" {
		return nil, errors.New("auth: empty token signing secret")
	}
	if accessTTL <= 0 {
		return nil, errors.New("auth: access token ttl must be positive")
	}
	return &Manager{secret: []byte(secret), issuer: issuer, accessTTL: accessTTL}, nil
}

func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

func (m *Manager) Issue(userID int64, role models.Role, now time.Time) (Access, error) {
	expiresAt := now.Add(m.accessTTL)

	claims := &Claims{
		Role:      role,
		Subject:   strconv.FormatInt(userID, 10),
		Issuer:    m.issuer,
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return Access{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return Access{Token: signed, ExpiresAt: expiresAt}, nil
}

func (m *Manager) Parse(raw string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if !claims.Role.Valid() {
		return nil, ErrInvalidToken
	}
	if _, err := claims.UserID(); err != nil {
		return nil, ErrInvalidToken
	}

	return claims, nil
}
