package response

import "github.com/gin-gonic/gin"

const (
	CodeValidationFailed    = "validation_failed"
	CodeInvalidCredentials  = "invalid_credentials"
	CodeEmailTaken          = "email_taken"
	CodePasswordUnavailable = "password_login_unavailable"
	CodeInvalidToken        = "invalid_token"
	CodeUnauthorized        = "unauthorized"
	CodeForbidden           = "forbidden"
	CodeNotFound            = "not_found"
	CodePayloadTooLarge     = "payload_too_large"
	CodeInvalidOAuthState   = "invalid_oauth_state"
	CodeGoogleDisabled      = "google_disabled"
	CodeGoogleUnverified    = "google_email_unverified"
	CodeInternal            = "internal_error"
)

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}
