package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/middleware"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/response"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

type AuthService interface {
	Register(ctx context.Context, email, password string, meta service.ClientMeta) (*service.Session, error)
	Login(ctx context.Context, email, password string, meta service.ClientMeta) (*service.Session, error)
	StartGoogleFlow(ctx context.Context, redirectTo string) (string, error)
	CompleteGoogleFlow(ctx context.Context, code, state string) (*service.GoogleCallback, error)
	ExchangeGoogleCode(ctx context.Context, code string, meta service.ClientMeta) (*service.Session, error)
	Refresh(ctx context.Context, rawToken string, meta service.ClientMeta) (*service.Session, error)
	Logout(ctx context.Context, rawToken string) error
	User(ctx context.Context, id int64) (*models.User, error)
	Accounts(ctx context.Context, limit int) ([]models.UserSummary, error)
}

type Auth struct {
	service     AuthService
	log         *zap.Logger
	frontendURL string
	google      bool
}

func NewAuth(svc AuthService, log *zap.Logger, frontendURL string, google bool) *Auth {
	return &Auth{service: svc, log: log, frontendURL: frontendURL, google: google}
}

func (h *Auth) Accounts(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, response.CodeValidationFailed, "limit must be a number")
			return
		}
		limit = parsed
	}

	accounts, err := h.service.Accounts(c.Request.Context(), limit)
	if err != nil {
		h.log.Error("list accounts",
			zap.String("request_id", middleware.RequestIDFrom(c)),
			zap.Error(err),
		)
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "Internal server error")
		return
	}

	c.JSON(http.StatusOK, newAccountsResponse(accounts))
}

func (h *Auth) Methods(c *gin.Context) {
	c.JSON(http.StatusOK, methodsResponse{Password: true, Google: h.google})
}

func (h *Auth) Register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}

	session, err := h.service.Register(c.Request.Context(), string(req.Email), req.Password, clientMeta(c))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusCreated, newSessionResponse(session))
}

func (h *Auth) Login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}

	session, err := h.service.Login(c.Request.Context(), string(req.Email), req.Password, clientMeta(c))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newSessionResponse(session))
}

func (h *Auth) Refresh(c *gin.Context) {
	var req refreshRequest
	if !bindJSON(c, &req) {
		return
	}

	session, err := h.service.Refresh(c.Request.Context(), req.RefreshToken, clientMeta(c))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newSessionResponse(session))
}

func (h *Auth) Logout(c *gin.Context) {
	var req refreshRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := h.service.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		h.fail(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Auth) Me(c *gin.Context) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized,
			"Authorization header with a bearer token is required")
		return
	}

	user, err := h.service.User(c.Request.Context(), userID)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newUserResponse(user))
}

func (h *Auth) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrEmailTaken):
		response.Error(c, http.StatusConflict, response.CodeEmailTaken,
			"This email is already registered")

	case errors.Is(err, service.ErrInvalidCredentials):
		response.Error(c, http.StatusUnauthorized, response.CodeInvalidCredentials,
			"Invalid email or password")

	case errors.Is(err, service.ErrPasswordLoginUnavailable):
		response.Error(c, http.StatusConflict, response.CodePasswordUnavailable,
			"This account was created with Google, sign in with Google")

	case errors.Is(err, service.ErrInvalidRefreshToken):
		response.Error(c, http.StatusUnauthorized, response.CodeInvalidToken,
			"Refresh token is invalid or expired, sign in again")

	case errors.Is(err, service.ErrInvalidGoogleToken):
		response.Error(c, http.StatusUnauthorized, response.CodeInvalidToken,
			"Google id-token is invalid")

	case errors.Is(err, service.ErrGoogleEmailUnverified):
		response.Error(c, http.StatusForbidden, response.CodeGoogleUnverified,
			"Google has not verified this email address")

	case errors.Is(err, service.ErrGoogleDisabled):
		response.Error(c, http.StatusNotImplemented, response.CodeGoogleDisabled,
			"Google sign-in is not configured on this server")

	case errors.Is(err, service.ErrInvalidOAuthState):
		response.Error(c, http.StatusBadRequest, response.CodeInvalidOAuthState,
			"Sign-in request is invalid or expired, start again")

	case errors.Is(err, service.ErrInvalidExchangeCode):
		response.Error(c, http.StatusUnauthorized, response.CodeInvalidToken,
			"Exchange code is invalid, expired or already used")

	case errors.Is(err, service.ErrUserNotFound):
		response.Error(c, http.StatusNotFound, response.CodeNotFound,
			"User not found")

	case errors.Is(err, auth.ErrPasswordTooShort):
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			"Password must be at least 8 characters long")

	case errors.Is(err, auth.ErrPasswordTooLong):
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			"Password must be at most 72 bytes long")

	case errors.Is(err, service.ErrInvalidEmail):
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			"Email is empty or too long")

	default:
		h.log.Error("unhandled auth error",
			zap.String("request_id", middleware.RequestIDFrom(c)),
			zap.String("path", c.FullPath()),
			zap.Error(err),
		)
		response.Error(c, http.StatusInternalServerError, response.CodeInternal,
			"Internal server error")
	}
}

func bindJSON(c *gin.Context, req any) bool {
	err := c.ShouldBindJSON(req)
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		response.Error(c, http.StatusRequestEntityTooLarge, response.CodePayloadTooLarge,
			"Request body is too large")
		return false
	}

	response.Error(c, http.StatusBadRequest, response.CodeValidationFailed, err.Error())
	return false
}

func clientMeta(c *gin.Context) service.ClientMeta {
	return service.ClientMeta{
		UserAgent: c.Request.UserAgent(),
		IP:        c.ClientIP(),
	}
}
