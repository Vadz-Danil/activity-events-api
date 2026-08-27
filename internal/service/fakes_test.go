package service

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

type fakeUsers struct {
	mu           sync.Mutex
	lastLimit    int
	seq          int64
	rows         map[int64]models.User
	tokens       *fakeTokens
	beforeCreate func()
}

func newFakeUsers(tokens *fakeTokens) *fakeUsers {
	return &fakeUsers{rows: make(map[int64]models.User), tokens: tokens}
}

func (f *fakeUsers) Create(_ context.Context, u models.User) (*models.User, error) {
	if f.beforeCreate != nil {
		hook := f.beforeCreate
		f.beforeCreate = nil
		hook()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, row := range f.rows {
		if strings.EqualFold(row.Email, u.Email) {
			return nil, repository.ErrEmailTaken
		}
		if u.GoogleSub != nil && row.GoogleSub != nil && *row.GoogleSub == *u.GoogleSub {
			return nil, repository.ErrGoogleSubTaken
		}
	}

	if !u.Role.Valid() {
		return nil, fmt.Errorf("fake: role %q violates users_role_check", u.Role)
	}

	f.seq++
	u.ID = f.seq
	u.CreatedAt = time.Now()
	u.UpdatedAt = u.CreatedAt
	f.rows[u.ID] = u

	return copyUser(u), nil
}

func (f *fakeUsers) List(_ context.Context, limit int) ([]models.UserSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastLimit = limit

	out := make([]models.UserSummary, 0, len(f.rows))
	for _, u := range f.rows {
		if len(out) == limit {
			break
		}
		out = append(out, models.UserSummary{ID: u.ID, Email: u.Email, Role: u.Role})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out, nil
}

func (f *fakeUsers) ByID(_ context.Context, id int64) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	row, ok := f.rows[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return copyUser(row), nil
}

func (f *fakeUsers) ByEmail(_ context.Context, email string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, row := range f.rows {
		if strings.EqualFold(row.Email, email) {
			return copyUser(row), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeUsers) ByGoogleSub(_ context.Context, sub string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, row := range f.rows {
		if row.GoogleSub != nil && *row.GoogleSub == sub {
			return copyUser(row), nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeUsers) AttachGoogle(_ context.Context, id int64, sub string, name *string, emailVerified bool) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, row := range f.rows {
		if row.ID != id && row.GoogleSub != nil && *row.GoogleSub == sub {
			return nil, repository.ErrGoogleSubTaken
		}
	}

	row, ok := f.rows[id]
	if !ok || (row.GoogleSub != nil && *row.GoogleSub != sub) {
		return nil, repository.ErrNotFound
	}

	row.GoogleSub = &sub
	if row.Name == nil {
		row.Name = name
	}
	if !row.EmailVerified {
		row.PasswordHash = nil
		f.tokens.revokeUser(id)
	}
	row.EmailVerified = row.EmailVerified || emailVerified
	row.UpdatedAt = time.Now()
	f.rows[id] = row

	return copyUser(row), nil
}

func copyUser(u models.User) *models.User {
	return &u
}

type fakeTokens struct {
	mu   sync.Mutex
	rows map[uuid.UUID]models.RefreshToken
}

func newFakeTokens() *fakeTokens {
	return &fakeTokens{rows: make(map[uuid.UUID]models.RefreshToken)}
}

func (f *fakeTokens) Create(_ context.Context, t models.RefreshToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rows[t.ID] = t
	return nil
}

func (f *fakeTokens) ByHash(_ context.Context, hash []byte) (*models.RefreshToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, row := range f.rows {
		if bytes.Equal(row.TokenHash, hash) {
			copied := row
			return &copied, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeTokens) Rotate(_ context.Context, oldID uuid.UUID, next models.RefreshToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	old, ok := f.rows[oldID]
	if !ok || old.RevokedAt != nil {
		return repository.ErrTokenAlreadyRotated
	}

	now := time.Now()
	old.RevokedAt = &now
	old.ReplacedBy = &next.ID
	f.rows[oldID] = old
	f.rows[next.ID] = next

	return nil
}

func (f *fakeTokens) RevokeFamily(_ context.Context, familyID uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	var revoked int64

	for id, row := range f.rows {
		if row.FamilyID == familyID && row.RevokedAt == nil {
			row.RevokedAt = &now
			f.rows[id] = row
			revoked++
		}
	}
	return revoked, nil
}

func (f *fakeTokens) revokeUser(userID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	for id, row := range f.rows {
		if row.UserID == userID && row.RevokedAt == nil {
			row.RevokedAt = &now
			f.rows[id] = row
		}
	}
}

func (f *fakeTokens) all() []models.RefreshToken {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]models.RefreshToken, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row)
	}
	return out
}

type fakeOAuth struct {
	mu     sync.Mutex
	states map[string]models.GoogleOAuthState
	codes  map[string]models.GoogleOAuthCode
}

func newFakeOAuth() *fakeOAuth {
	return &fakeOAuth{
		states: make(map[string]models.GoogleOAuthState),
		codes:  make(map[string]models.GoogleOAuthCode),
	}
}

func (f *fakeOAuth) CreateState(_ context.Context, s models.GoogleOAuthState) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	s.CreatedAt = time.Now()
	f.states[string(s.StateHash)] = s
	return nil
}

func (f *fakeOAuth) TakeState(_ context.Context, hash []byte, now time.Time) (*models.GoogleOAuthState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.states[string(hash)]
	if !ok || !now.Before(s.ExpiresAt) {
		return nil, repository.ErrNotFound
	}
	delete(f.states, string(hash))

	return &s, nil
}

func (f *fakeOAuth) CreateCode(_ context.Context, c models.GoogleOAuthCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	c.CreatedAt = time.Now()
	f.codes[string(c.CodeHash)] = c
	return nil
}

func (f *fakeOAuth) TakeCode(_ context.Context, hash []byte, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c, ok := f.codes[string(hash)]
	if !ok || c.UsedAt != nil || !now.Before(c.ExpiresAt) {
		return 0, repository.ErrNotFound
	}

	c.UsedAt = &now
	f.codes[string(hash)] = c

	return c.UserID, nil
}

func (f *fakeOAuth) pendingStates() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.states)
}

type fakeExchanger struct {
	mu        sync.Mutex
	idToken   string
	err       error
	state     string
	challenge string
	code      string
	verifier  string
}

func (f *fakeExchanger) AuthCodeURL(state, challenge string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.state, f.challenge = state, challenge
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + state + "&code_challenge=" + challenge
}

func (f *fakeExchanger) Exchange(_ context.Context, code, verifier string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.code, f.verifier = code, verifier
	if f.err != nil {
		return "", f.err
	}
	return f.idToken, nil
}

type fakeGoogle struct {
	claims *auth.GoogleClaims
	err    error
}

func (f *fakeGoogle) Verify(context.Context, string) (*auth.GoogleClaims, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.claims, nil
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
