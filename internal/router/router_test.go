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

// Паніка в хендлері має перетворитися на 500 і при цьому потрапити в метрики,
// а лічильник запитів в обробці — повернутися до нуля. Тест фіксує саме порядок
// middleware: Recovery мусить бути всередині Metrics.
func TestRecoveredPanicIsCounted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	m := metrics.New("test")
	engine := New(Deps{
		Config:  &config.Config{App: config.App{Mode: config.ModeAPI}},
		Logger:  zap.NewNop(),
		Metrics: m,
		Version: "test",
	})
	engine.GET("/boom", func(*gin.Context) { panic("щось пішло не так") })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("статус: маємо %d, очікували %d", rec.Code, http.StatusInternalServerError)
	}

	if got := counterValue(t, m, "activity_http_requests_total", "500"); got != 1 {
		t.Errorf("запит із 500 не порахований: маємо %v", got)
	}

	if got := gaugeValue(t, m, "activity_http_requests_in_flight"); got != 0 {
		t.Errorf("лічильник запитів в обробці протік: маємо %v", got)
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
		t.Fatalf("зібрати метрики: %v", err)
	}
	return families
}
