package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Vadz-Danil/activity-events-api/internal/metrics"
)

func Metrics(m *metrics.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		timer := m.NewTimer(c.Request.Method, route)
		m.HTTPInFlight.Inc()

		// Через defer, щоб лічильник запитів в обробці не «протікав»,
		// якщо нижче по ланцюжку хтось запанікує.
		defer func() {
			timer.ObserveDuration()
			m.HTTPInFlight.Dec()
			m.HTTPRequests.
				WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).
				Inc()
		}()

		c.Next()
	}
}
