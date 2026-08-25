package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func Logger(log *zap.Logger, skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if _, ok := skip[c.Request.URL.Path]; ok {
			return
		}

		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
			zap.String("request_id", RequestIDFrom(c)),
		}
		if q := c.Request.URL.RawQuery; q != "" {
			fields = append(fields, zap.String("query", q))
		}
		if err := c.Errors.Last(); err != nil {
			fields = append(fields, zap.Error(err.Err))
		}

		switch {
		case c.Writer.Status() >= 500:
			log.Error("http request", fields...)
		case c.Writer.Status() >= 400:
			log.Warn("http request", fields...)
		default:
			log.Info("http request", fields...)
		}
	}
}
