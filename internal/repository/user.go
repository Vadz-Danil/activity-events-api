package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, u models.User) (*models.User, error) {
	const q = `
		INSERT INTO users (email, password_hash, role, google_sub, name, email_verified)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, email, password_hash, role, google_sub, name, email_verified, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q,
		u.Email, u.PasswordHash, u.Role, u.GoogleSub, u.Name, u.EmailVerified)

	created, err := scanUser(row)
	if err != nil {
		return nil, translateUniqueViolation(err)
	}
	return created, nil
}

func (r *UserRepository) ByID(ctx context.Context, id int64) (*models.User, error) {
	const q = `
		SELECT id, email, password_hash, role, google_sub, name, email_verified, created_at, updated_at
		FROM users
		WHERE id = $1`

	return scanUser(r.pool.QueryRow(ctx, q, id))
}

func (r *UserRepository) ByEmail(ctx context.Context, email string) (*models.User, error) {
	const q = `
		SELECT id, email, password_hash, role, google_sub, name, email_verified, created_at, updated_at
		FROM users
		WHERE lower(email) = lower($1)`

	return scanUser(r.pool.QueryRow(ctx, q, email))
}

func (r *UserRepository) ByGoogleSub(ctx context.Context, sub string) (*models.User, error) {
	const q = `
		SELECT id, email, password_hash, role, google_sub, name, email_verified, created_at, updated_at
		FROM users
		WHERE google_sub = $1`

	return scanUser(r.pool.QueryRow(ctx, q, sub))
}

func (r *UserRepository) AttachGoogle(ctx context.Context, id int64, sub string, name *string, emailVerified bool) (*models.User, error) {
	const q = `
		UPDATE users
		SET google_sub     = $2,
		    name           = COALESCE(name, $3),
		    password_hash  = CASE WHEN email_verified THEN password_hash END,
		    email_verified = email_verified OR $4,
		    updated_at     = now()
		WHERE id = $1 AND (google_sub IS NULL OR google_sub = $2)
		RETURNING id, email, password_hash, role, google_sub, name, email_verified, created_at, updated_at`

	updated, err := scanUser(r.pool.QueryRow(ctx, q, id, sub, name, emailVerified))
	if err != nil {
		return nil, translateUniqueViolation(err)
	}
	return updated, nil
}

func scanUser(row pgx.Row) (*models.User, error) {
	var u models.User

	err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role,
		&u.GoogleSub, &u.Name, &u.EmailVerified,
		&u.CreatedAt, &u.UpdatedAt,
	)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan user: %w", err)
	}

	return &u, nil
}

func translateUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}

	switch pgErr.ConstraintName {
	case "users_email_lower_key":
		return ErrEmailTaken
	case "users_google_sub_key":
		return ErrGoogleSubTaken
	default:
		return err
	}
}
