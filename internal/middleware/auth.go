package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/response"
)

const (
	ctxUserID   = "auth_user_id"
	ctxUserRole = "auth_user_role"
)

type TokenParser interface {
	Parse(raw string) (*auth.Claims, error)
}

type Guard struct {
	parser TokenParser
}

func NewGuard(parser TokenParser) *Guard { return &Guard{parser: parser} }

func (g *Guard) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized,
				"Authorization header with a bearer token is required")
			return
		}

		claims, err := g.parser.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, response.CodeInvalidToken,
				"Access token is invalid or expired")
			return
		}

		userID, _ := claims.UserID()
		c.Set(ctxUserID, userID)
		c.Set(ctxUserRole, claims.Role)

		c.Next()
	}
}

func (g *Guard) RequireRole(roles ...models.Role) gin.HandlerFunc {
	allowed := make(map[models.Role]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(c *gin.Context) {
		role, ok := RoleFrom(c)
		if !ok {
			response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized,
				"Authorization header with a bearer token is required")
			return
		}
		if _, ok := allowed[role]; !ok {
			response.Error(c, http.StatusForbidden, response.CodeForbidden,
				"This action requires a different role")
			return
		}

		c.Next()
	}
}

func UserIDFrom(c *gin.Context) (int64, bool) {
	v, ok := c.Get(ctxUserID)
	if !ok {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}

func RoleFrom(c *gin.Context) (models.Role, bool) {
	v, ok := c.Get(ctxUserRole)
	if !ok {
		return "", false
	}
	role, ok := v.(models.Role)
	return role, ok
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}

	token = strings.TrimSpace(token)
	return token, token != ""
}
