package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery", bcrypt.MinCost)
	require.NoError(t, err)
	require.NotEqual(t, "correct horse battery", hash)

	require.True(t, CheckPassword(hash, "correct horse battery"))
	require.False(t, CheckPassword(hash, "correct horse batterY"))
	require.False(t, CheckPassword(hash, ""))
	require.False(t, CheckPassword("", "correct horse battery"))
}

func TestHashPasswordIsSalted(t *testing.T) {
	first, err := HashPassword("same-password", bcrypt.MinCost)
	require.NoError(t, err)

	second, err := HashPassword("same-password", bcrypt.MinCost)
	require.NoError(t, err)

	require.NotEqual(t, first, second)
	require.True(t, CheckPassword(first, "same-password"))
	require.True(t, CheckPassword(second, "same-password"))
}

func TestHashPasswordRejectsTooLong(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", MaxPasswordLen+1), bcrypt.MinCost)
	require.ErrorIs(t, err, ErrPasswordTooLong)
}
