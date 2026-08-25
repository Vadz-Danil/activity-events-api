package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Vadz-Danil/activity-events-api/internal/metrics"
)

var knownMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodOptions: {},
}

func Metrics(m *metrics.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := requestMethod(c.Request.Method)

		timer := m.NewTimer(method, route)
		m.HTTPInFlight.Inc()

		defer func() {
			timer.ObserveDuration()
			m.HTTPInFlight.Dec()
			m.HTTPRequests.
				WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).
				Inc()
		}()

		c.Next()
	}
}

func requestMethod(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return "other"
}
