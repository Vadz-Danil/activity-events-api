package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	HeaderRequestID = "X-Request-ID"
	ctxRequestID    = "request_id"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}

		c.Set(ctxRequestID, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

func RequestIDFrom(c *gin.Context) string {
	id, _ := c.Get(ctxRequestID)
	s, _ := id.(string)
	return s
}
