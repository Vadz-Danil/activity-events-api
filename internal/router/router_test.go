package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/metrics"
)

func TestRecoveredPanicIsCounted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	m := metrics.New("test")
	engine := New(Deps{
		Config:  &config.Config{App: config.App{Mode: config.ModeAPI}},
		Logger:  zap.NewNop(),
		Metrics: m,
		Version: "test",
	})
	engine.GET("/boom", func(*gin.Context) { panic("something went wrong") })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if got := counterValue(t, m, "activity_http_requests_total", "500"); got != 1 {
		t.Errorf("request with 500 was not counted: got %v", got)
	}

	if got := gaugeValue(t, m, "activity_http_requests_in_flight"); got != 0 {
		t.Errorf("in-flight gauge leaked: got %v", got)
	}
}

func counterValue(t *testing.T, m *metrics.Metrics, name, status string) float64 {
	t.Helper()

	for _, family := range gather(t, m) {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "status" && label.GetValue() == status {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func gaugeValue(t *testing.T, m *metrics.Metrics, name string) float64 {
	t.Helper()

	for _, family := range gather(t, m) {
		if family.GetName() == name {
			return family.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return 0
}

func gather(t *testing.T, m *metrics.Metrics) []*dto.MetricFamily {
	t.Helper()

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	return families
}
