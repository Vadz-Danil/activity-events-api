package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type GoogleOAuthRepository struct {
	pool *pgxpool.Pool
}

func NewGoogleOAuthRepository(pool *pgxpool.Pool) *GoogleOAuthRepository {
	return &GoogleOAuthRepository{pool: pool}
}

func (r *GoogleOAuthRepository) CreateState(ctx context.Context, s models.GoogleOAuthState) error {
	const q = `
		INSERT INTO google_oauth_states (state_hash, code_verifier, redirect_to, expires_at)
		VALUES ($1, $2, $3, $4)`

	if _, err := r.pool.Exec(ctx, q, s.StateHash, s.CodeVerifier, s.RedirectTo, s.ExpiresAt); err != nil {
		return fmt.Errorf("repository: insert oauth state: %w", err)
	}
	return nil
}

func (r *GoogleOAuthRepository) TakeState(ctx context.Context, hash []byte, now time.Time) (*models.GoogleOAuthState, error) {
	const q = `
		DELETE FROM google_oauth_states
		WHERE state_hash = $1 AND expires_at > $2
		RETURNING state_hash, code_verifier, redirect_to, created_at, expires_at`

	var s models.GoogleOAuthState
	err := r.pool.QueryRow(ctx, q, hash, now).Scan(
		&s.StateHash, &s.CodeVerifier, &s.RedirectTo, &s.CreatedAt, &s.ExpiresAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: take oauth state: %w", err)
	}

	return &s, nil
}

func (r *GoogleOAuthRepository) CreateCode(ctx context.Context, c models.GoogleOAuthCode) error {
	const q = `
		INSERT INTO google_oauth_codes (code_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`

	if _, err := r.pool.Exec(ctx, q, c.CodeHash, c.UserID, c.ExpiresAt); err != nil {
		return fmt.Errorf("repository: insert oauth code: %w", err)
	}
	return nil
}

func (r *GoogleOAuthRepository) TakeCode(ctx context.Context, hash []byte, now time.Time) (int64, error) {
	const q = `
		UPDATE google_oauth_codes
		SET used_at = $2
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > $2
		RETURNING user_id`

	var userID int64
	err := r.pool.QueryRow(ctx, q, hash, now).Scan(&userID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, ErrNotFound
	case err != nil:
		return 0, fmt.Errorf("repository: take oauth code: %w", err)
	}

	return userID, nil
}

func (r *GoogleOAuthRepository) DeleteExpired(ctx context.Context, olderThan time.Time) (int64, error) {
	const states = `DELETE FROM google_oauth_states WHERE expires_at < $1`
	const codes = `DELETE FROM google_oauth_codes WHERE expires_at < $1`

	tag, err := r.pool.Exec(ctx, states, olderThan)
	if err != nil {
		return 0, fmt.Errorf("repository: delete expired oauth states: %w", err)
	}
	deleted := tag.RowsAffected()

	tag, err = r.pool.Exec(ctx, codes, olderThan)
	if err != nil {
		return deleted, fmt.Errorf("repository: delete expired oauth codes: %w", err)
	}
	return deleted + tag.RowsAffected(), nil
}
