package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func Recovery(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic while handling request",
					zap.Any("panic", r),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.String("request_id", RequestIDFrom(c)),
					zap.Stack("stack"),
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"code":    "internal_error",
						"message": "Internal server error",
					},
					"request_id": RequestIDFrom(c),
				})
			}
		}()

		c.Next()
	}
}
