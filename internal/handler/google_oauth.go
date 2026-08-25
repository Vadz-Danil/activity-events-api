package handler

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/middleware"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

const frontendCallbackPath = "/auth/callback"

func (h *Auth) GoogleStart(c *gin.Context) {
	target, err := h.service.StartGoogleFlow(c.Request.Context(), c.Query("redirect_to"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.Redirect(http.StatusFound, target)
}

func (h *Auth) GoogleCallback(c *gin.Context) {
	if reason := c.Query("error"); reason != "" {
		h.redirectToFrontend(c, "", "", reason)
		return
	}

	result, err := h.service.CompleteGoogleFlow(c.Request.Context(), c.Query("code"), c.Query("state"))
	if err != nil {
		h.log.Warn("google callback failed",
			zap.String("request_id", middleware.RequestIDFrom(c)),
			zap.Error(err),
		)
		h.redirectToFrontend(c, "", "", callbackError(err))
		return
	}

	h.redirectToFrontend(c, result.Code, result.RedirectTo, "")
}

func (h *Auth) GoogleExchange(c *gin.Context) {
	var req exchangeRequest
	if !bindJSON(c, &req) {
		return
	}

	session, err := h.service.ExchangeGoogleCode(c.Request.Context(), req.Code, clientMeta(c))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newSessionResponse(session))
}

func (h *Auth) redirectToFrontend(c *gin.Context, code, redirectTo, failure string) {
	q := url.Values{}
	switch {
	case failure != "":
		q.Set("error", failure)
	default:
		q.Set("code", code)
		if redirectTo != "" {
			q.Set("redirect_to", redirectTo)
		}
	}

	c.Redirect(http.StatusFound, h.frontendURL+frontendCallbackPath+"?"+q.Encode())
}

func callbackError(err error) string {
	switch {
	case errors.Is(err, service.ErrInvalidOAuthState):
		return "invalid_state"
	case errors.Is(err, service.ErrGoogleEmailUnverified):
		return "email_unverified"
	case errors.Is(err, service.ErrInvalidGoogleToken):
		return "invalid_token"
	case errors.Is(err, service.ErrGoogleDisabled):
		return "google_disabled"
	default:
		return "internal_error"
	}
}
