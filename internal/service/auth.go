package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
)

type UserRepository interface {
	Create(ctx context.Context, u models.User) (*models.User, error)
	ByID(ctx context.Context, id int64) (*models.User, error)
	ByEmail(ctx context.Context, email string) (*models.User, error)
	ByGoogleSub(ctx context.Context, sub string) (*models.User, error)
	AttachGoogle(ctx context.Context, id int64, sub string, name *string, emailVerified bool) (*models.User, error)
}

type RefreshTokenRepository interface {
	Create(ctx context.Context, t models.RefreshToken) error
	ByHash(ctx context.Context, hash []byte) (*models.RefreshToken, error)
	Rotate(ctx context.Context, oldID uuid.UUID, next models.RefreshToken) error
	RevokeFamily(ctx context.Context, familyID uuid.UUID) (int64, error)
}

type GoogleVerifier interface {
	Verify(ctx context.Context, idToken string) (*auth.GoogleClaims, error)
}

type ClientMeta struct {
	UserAgent string
	IP        string
}

type Session struct {
	User         *models.User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ExpiresIn    int
}

type Auth struct {
	users      UserRepository
	tokens     RefreshTokenRepository
	oauth      GoogleOAuthRepository
	jwt        *auth.Manager
	google     GoogleVerifier
	exchanger  GoogleExchanger
	log        *zap.Logger
	bcryptCost int
	refreshTTL time.Duration
	decoyHash  string
	now        func() time.Time
}

type AuthDeps struct {
	Users      UserRepository
	Tokens     RefreshTokenRepository
	OAuth      GoogleOAuthRepository
	JWT        *auth.Manager
	Google     GoogleVerifier
	Exchanger  GoogleExchanger
	Logger     *zap.Logger
	BcryptCost int
	RefreshTTL time.Duration
	Now        func() time.Time
}

const decoyPassword = "not-a-real-password"

func NewAuth(d AuthDeps) (*Auth, error) {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	log := d.Logger
	if log == nil {
		log = zap.NewNop()
	}

	decoy, err := auth.HashPassword(decoyPassword, d.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("service: prepare the login decoy hash: %w", err)
	}

	return &Auth{
		users:      d.Users,
		tokens:     d.Tokens,
		oauth:      d.OAuth,
		jwt:        d.JWT,
		google:     d.Google,
		exchanger:  d.Exchanger,
		log:        log,
		bcryptCost: d.BcryptCost,
		refreshTTL: d.RefreshTTL,
		decoyHash:  decoy,
		now:        now,
	}, nil
}

func (s *Auth) Register(ctx context.Context, email, password string, meta ClientMeta) (*Session, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}

	hash, err := auth.HashPassword(password, s.bcryptCost)
	if err != nil {
		return nil, err
	}

	user, err := s.users.Create(ctx, models.User{
		Email:        normalized,
		PasswordHash: &hash,
		Role:         models.RoleUser,
	})
	switch {
	case errors.Is(err, repository.ErrEmailTaken):
		return nil, ErrEmailTaken
	case err != nil:
		return nil, err
	}

	return s.startSession(ctx, user, meta)
}

func (s *Auth) Login(ctx context.Context, email, password string, meta ClientMeta) (*Session, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	user, err := s.users.ByEmail(ctx, normalized)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		auth.CheckPassword(s.decoyHash, password)
		return nil, ErrInvalidCredentials
	case err != nil:
		return nil, err
	}

	if !user.HasPassword() {
		return nil, ErrPasswordLoginUnavailable
	}
	if !auth.CheckPassword(*user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	return s.startSession(ctx, user, meta)
}

func (s *Auth) resolveGoogleUser(ctx context.Context, claims *auth.GoogleClaims) (*models.User, error) {
	if !claims.EmailVerified || claims.Email == "" {
		return nil, ErrGoogleEmailUnverified
	}

	user, err := s.users.ByGoogleSub(ctx, claims.Subject)
	switch {
	case err == nil:
		return user, nil
	case errors.Is(err, repository.ErrNotFound):
		return s.linkOrCreateGoogleUser(ctx, claims)
	default:
		return nil, err
	}
}

func (s *Auth) linkOrCreateGoogleUser(ctx context.Context, claims *auth.GoogleClaims) (*models.User, error) {
	email, err := normalizeEmail(claims.Email)
	if err != nil {
		return nil, ErrInvalidGoogleToken
	}

	existing, err := s.users.ByEmail(ctx, email)
	switch {
	case err == nil:
		return s.attachGoogle(ctx, existing, claims)
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}

	created, err := s.users.Create(ctx, models.User{
		Email:         email,
		Role:          models.RoleUser,
		GoogleSub:     &claims.Subject,
		Name:          textPtr(claims.Name),
		EmailVerified: true,
	})
	switch {
	case err == nil:
		return created, nil
	case errors.Is(err, repository.ErrGoogleSubTaken):
		return nil, ErrInvalidGoogleToken
	case !errors.Is(err, repository.ErrEmailTaken):
		return nil, err
	}

	existing, err = s.users.ByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return s.attachGoogle(ctx, existing, claims)
}

func (s *Auth) attachGoogle(ctx context.Context, user *models.User, claims *auth.GoogleClaims) (*models.User, error) {
	if user.HasGoogle() {
		if *user.GoogleSub != claims.Subject {
			return nil, ErrInvalidGoogleToken
		}
		return user, nil
	}

	linked, err := s.users.AttachGoogle(ctx, user.ID, claims.Subject, textPtr(claims.Name), true)
	switch {
	case errors.Is(err, repository.ErrGoogleSubTaken):
		return nil, ErrInvalidGoogleToken
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrInvalidGoogleToken
	case err != nil:
		return nil, err
	}
	return linked, nil
}

func (s *Auth) Refresh(ctx context.Context, rawToken string, meta ClientMeta) (*Session, error) {
	stored, err := s.tokens.ByHash(ctx, auth.HashRefreshToken(rawToken))
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrInvalidRefreshToken
	case err != nil:
		return nil, err
	}

	now := s.now()

	if stored.Revoked() {
		s.revokeCompromisedFamily(ctx, stored, "refresh token reuse detected")
		return nil, ErrInvalidRefreshToken
	}
	if stored.Expired(now) {
		return nil, ErrInvalidRefreshToken
	}

	user, err := s.users.ByID(ctx, stored.UserID)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrInvalidRefreshToken
	case err != nil:
		return nil, err
	}

	next, raw, err := s.newRefreshToken(user.ID, stored.FamilyID, meta, now)
	if err != nil {
		return nil, err
	}

	err = s.tokens.Rotate(ctx, stored.ID, next)
	switch {
	case errors.Is(err, repository.ErrTokenAlreadyRotated):
		s.revokeCompromisedFamily(ctx, stored, "concurrent rotation of the same refresh token")
		return nil, ErrInvalidRefreshToken
	case err != nil:
		return nil, err
	}

	return s.issueSession(user, raw, now)
}

func (s *Auth) Logout(ctx context.Context, rawToken string) error {
	stored, err := s.tokens.ByHash(ctx, auth.HashRefreshToken(rawToken))
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil
	case err != nil:
		return err
	}

	if _, err := s.tokens.RevokeFamily(ctx, stored.FamilyID); err != nil {
		return err
	}
	return nil
}

func (s *Auth) User(ctx context.Context, id int64) (*models.User, error) {
	user, err := s.users.ByID(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrUserNotFound
	case err != nil:
		return nil, err
	}
	return user, nil
}

func (s *Auth) startSession(ctx context.Context, user *models.User, meta ClientMeta) (*Session, error) {
	now := s.now()

	token, raw, err := s.newRefreshToken(user.ID, uuid.New(), meta, now)
	if err != nil {
		return nil, err
	}
	if err := s.tokens.Create(ctx, token); err != nil {
		return nil, err
	}

	return s.issueSession(user, raw, now)
}

func (s *Auth) issueSession(user *models.User, refreshToken string, now time.Time) (*Session, error) {
	access, err := s.jwt.Issue(user.ID, user.Role, now)
	if err != nil {
		return nil, err
	}

	return &Session{
		User:         user,
		AccessToken:  access.Token,
		RefreshToken: refreshToken,
		ExpiresAt:    access.ExpiresAt,
		ExpiresIn:    int(s.jwt.AccessTTL().Seconds()),
	}, nil
}

func (s *Auth) newRefreshToken(userID int64, familyID uuid.UUID, meta ClientMeta, now time.Time) (models.RefreshToken, string, error) {
	raw, err := auth.NewRefreshToken()
	if err != nil {
		return models.RefreshToken{}, "", err
	}

	return models.RefreshToken{
		ID:        uuid.New(),
		FamilyID:  familyID,
		UserID:    userID,
		TokenHash: auth.HashRefreshToken(raw),
		UserAgent: textPtr(meta.UserAgent),
		IP:        models.ParseIP(meta.IP),
		ExpiresAt: now.Add(s.refreshTTL),
	}, raw, nil
}

func (s *Auth) revokeCompromisedFamily(ctx context.Context, token *models.RefreshToken, reason string) {
	revoked, err := s.tokens.RevokeFamily(ctx, token.FamilyID)
	s.log.Warn(reason,
		zap.Int64("user_id", token.UserID),
		zap.String("family_id", token.FamilyID.String()),
		zap.Int64("revoked_tokens", revoked),
		zap.Error(err),
	)
}

func normalizeEmail(email string) (string, error) {
	normalized := models.NormalizeEmail(email)
	if normalized == "" || len(normalized) > models.MaxEmailLen {
		return "", ErrInvalidEmail
	}
	return normalized, nil
}

func textPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
