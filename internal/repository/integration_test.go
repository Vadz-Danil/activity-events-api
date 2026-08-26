//go:build integration

package repository

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Vadz-Danil/activity-events-api/internal/models"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run repository integration tests")
	}

	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, pool.Ping(context.Background()))
	return pool
}

func createUser(t *testing.T, pool *pgxpool.Pool, email string, verified bool) models.User {
	t.Helper()

	hash := "$2a$04$irrelevanthashvaluewithenoughlength123456"
	users := NewUserRepository(pool)

	user, err := users.Create(context.Background(), models.User{
		Email:         email,
		PasswordHash:  &hash,
		Role:          models.RoleUser,
		EmailVerified: verified,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	})

	return *user
}

func createToken(t *testing.T, pool *pgxpool.Pool, userID int64, familyID uuid.UUID) models.RefreshToken {
	t.Helper()

	token := models.RefreshToken{
		ID:        uuid.New(),
		FamilyID:  familyID,
		UserID:    userID,
		TokenHash: uuid.New().NodeID(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	require.NoError(t, NewRefreshTokenRepository(pool).Create(context.Background(), token))

	return token
}

func liveTokens(t *testing.T, pool *pgxpool.Pool, familyID uuid.UUID) int {
	t.Helper()

	var live int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens WHERE family_id = $1 AND revoked_at IS NULL`, familyID).Scan(&live)
	require.NoError(t, err)

	return live
}

func TestAttachGoogleRevokesSessionsOfAnUnverifiedAccount(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	user := createUser(t, pool, "integration.unverified@example.com", false)
	family := uuid.New()
	createToken(t, pool, user.ID, family)

	linked, err := NewUserRepository(pool).AttachGoogle(ctx, user.ID, "google-"+uuid.NewString(), nil, true)
	require.NoError(t, err)

	require.Nil(t, linked.PasswordHash, "an unverified password must not survive the link")
	require.True(t, linked.EmailVerified)
	require.Zero(t, liveTokens(t, pool, family), "sessions opened before the link must be revoked")
}

func TestAttachGoogleKeepsSessionsOfAVerifiedAccount(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	user := createUser(t, pool, "integration.verified@example.com", true)
	family := uuid.New()
	createToken(t, pool, user.ID, family)

	linked, err := NewUserRepository(pool).AttachGoogle(ctx, user.ID, "google-"+uuid.NewString(), nil, true)
	require.NoError(t, err)

	require.NotNil(t, linked.PasswordHash)
	require.Equal(t, 1, liveTokens(t, pool, family))
}

// A rotation running in parallel with a family revocation must not leave a live
// token behind: the successor is invisible to the revoker until commit, so both
// sides take the same advisory lock.
func TestRotateAndRevokeFamilyDoNotLeaveALiveToken(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	user := createUser(t, pool, "integration.rotation@example.com", true)
	tokens := NewRefreshTokenRepository(pool)

	for i := range 25 {
		family := uuid.New()
		current := createToken(t, pool, user.ID, family)

		next := models.RefreshToken{
			ID:        uuid.New(),
			FamilyID:  family,
			UserID:    user.ID,
			TokenHash: uuid.New().NodeID(),
			ExpiresAt: time.Now().Add(time.Hour),
		}

		var wg sync.WaitGroup
		wg.Add(2)

		var rotateErr error
		go func() {
			defer wg.Done()
			rotateErr = tokens.Rotate(ctx, current.ID, next)
		}()
		go func() {
			defer wg.Done()
			_, _ = tokens.RevokeFamily(ctx, family)
		}()
		wg.Wait()

		if rotateErr == nil {
			require.Zero(t, liveTokens(t, pool, family), "iteration %d left a live token in a revoked family", i)
		}

		_, _ = pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE family_id = $1`, family)
	}
}
