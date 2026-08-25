package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "activity"

type Metrics struct {
	registry *prometheus.Registry

	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec
	HTTPInFlight prometheus.Gauge
}

func New(component string) *Metrics {
	reg := prometheus.NewRegistry()
	labels := prometheus.Labels{"component": component}

	m := &Metrics{
		registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   "http",
			Name:        "requests_total",
			Help:        "Кількість HTTP-запитів.",
			ConstLabels: labels,
		}, []string{"method", "route", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   namespace,
			Subsystem:   "http",
			Name:        "request_duration_seconds",
			Help:        "Тривалість обробки HTTP-запиту.",
			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			ConstLabels: labels,
		}, []string{"method", "route"}),
		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "http",
			Name:        "requests_in_flight",
			Help:        "Кількість запитів в обробці просто зараз.",
			ConstLabels: labels,
		}),
	}

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests,
		m.HTTPDuration,
		m.HTTPInFlight,
	)

	return m
}

func (m *Metrics) NewTimer(method, route string) *prometheus.Timer {
	return prometheus.NewTimer(m.HTTPDuration.WithLabelValues(method, route))
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }
