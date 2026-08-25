package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type RefreshTokenRepository struct {
	pool *pgxpool.Pool
}

func NewRefreshTokenRepository(pool *pgxpool.Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{pool: pool}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, t models.RefreshToken) error {
	return insertRefreshToken(ctx, r.pool, t)
}

func (r *RefreshTokenRepository) ByHash(ctx context.Context, hash []byte) (*models.RefreshToken, error) {
	const q = `
		SELECT id, family_id, user_id, token_hash, user_agent, ip, created_at, expires_at, revoked_at, replaced_by
		FROM refresh_tokens
		WHERE token_hash = $1`

	var t models.RefreshToken

	err := r.pool.QueryRow(ctx, q, hash).Scan(
		&t.ID, &t.FamilyID, &t.UserID, &t.TokenHash, &t.UserAgent, &t.IP,
		&t.CreatedAt, &t.ExpiresAt, &t.RevokedAt, &t.ReplacedBy,
	)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan refresh token: %w", err)
	}

	return &t, nil
}

func (r *RefreshTokenRepository) Rotate(ctx context.Context, oldID uuid.UUID, next models.RefreshToken) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const lock = `SELECT 1 FROM refresh_tokens WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`

	var locked int
	switch err := tx.QueryRow(ctx, lock, oldID).Scan(&locked); {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrTokenAlreadyRotated
	case err != nil:
		return fmt.Errorf("repository: lock rotated token: %w", err)
	}

	if err := insertRefreshToken(ctx, tx, next); err != nil {
		return err
	}

	const q = `
		UPDATE refresh_tokens
		SET revoked_at = now(), replaced_by = $2
		WHERE id = $1 AND revoked_at IS NULL`

	tag, err := tx.Exec(ctx, q, oldID, next.ID)
	if err != nil {
		return fmt.Errorf("repository: revoke rotated token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTokenAlreadyRotated
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repository: commit rotation: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("repository: begin family revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockFamily(ctx, tx, familyID); err != nil {
		return 0, err
	}

	const q = `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL`

	tag, err := tx.Exec(ctx, q, familyID)
	if err != nil {
		return 0, fmt.Errorf("repository: revoke token family: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("repository: commit family revocation: %w", err)
	}
	return tag.RowsAffected(), nil
}

func lockFamily(ctx context.Context, db executor, familyID uuid.UUID) error {
	const q = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

	if _, err := db.Exec(ctx, q, familyID.String()); err != nil {
		return fmt.Errorf("repository: lock token family: %w", err)
	}
	return nil
}

func (r *RefreshTokenRepository) DeleteExpired(ctx context.Context, olderThan time.Time) (int64, error) {
	const q = `DELETE FROM refresh_tokens WHERE expires_at < $1`

	tag, err := r.pool.Exec(ctx, q, olderThan)
	if err != nil {
		return 0, fmt.Errorf("repository: delete expired tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

func insertRefreshToken(ctx context.Context, db executor, t models.RefreshToken) error {
	const q = `
		INSERT INTO refresh_tokens (id, family_id, user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := db.Exec(ctx, q, t.ID, t.FamilyID, t.UserID, t.TokenHash, t.UserAgent, t.IP, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("repository: insert refresh token: %w", err)
	}
	return nil
}
