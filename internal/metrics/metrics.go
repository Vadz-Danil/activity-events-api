package metrics

import (
	"net/http"
	"time"

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

	AggregationRuns     *prometheus.CounterVec
	AggregationDuration prometheus.Histogram
	AggregationRows     prometheus.Counter
	AggregationLag      prometheus.Gauge

	EventsIngested    *prometheus.CounterVec
	StreamSubscribers prometheus.Gauge
	StreamDropped     prometheus.Counter
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
			Help:        "Total number of HTTP requests.",
			ConstLabels: labels,
		}, []string{"method", "route", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   namespace,
			Subsystem:   "http",
			Name:        "request_duration_seconds",
			Help:        "HTTP request duration.",
			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			ConstLabels: labels,
		}, []string{"method", "route"}),
		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "http",
			Name:        "requests_in_flight",
			Help:        "Number of HTTP requests currently in flight.",
			ConstLabels: labels,
		}),
		AggregationRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   "aggregation",
			Name:        "runs_total",
			Help:        "Aggregation runs by outcome and by what started them.",
			ConstLabels: labels,
		}, []string{"status", "trigger"}),
		AggregationDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace:   namespace,
			Subsystem:   "aggregation",
			Name:        "duration_seconds",
			Help:        "Time it takes to recompute one bucket.",
			Buckets:     []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
			ConstLabels: labels,
		}),
		AggregationRows: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   "aggregation",
			Name:        "rows_total",
			Help:        "Bucket rows written by aggregation.",
			ConstLabels: labels,
		}),
		AggregationLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "aggregation",
			Name:        "lag_seconds",
			Help:        "How far the newest aggregated bucket trails the present.",
			ConstLabels: labels,
		}),
		EventsIngested: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   "events",
			Name:        "ingested_total",
			Help:        "Ingested events split into new ones and idempotency-key repeats.",
			ConstLabels: labels,
		}, []string{"result"}),
		StreamSubscribers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "stream",
			Name:        "subscribers",
			Help:        "Open server-sent event connections.",
			ConstLabels: labels,
		}),
		StreamDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   "stream",
			Name:        "dropped_total",
			Help:        "Events dropped because a subscriber could not keep up.",
			ConstLabels: labels,
		}),
	}

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests,
		m.HTTPDuration,
		m.HTTPInFlight,
		m.AggregationRuns,
		m.AggregationDuration,
		m.AggregationRows,
		m.AggregationLag,
		m.EventsIngested,
		m.StreamSubscribers,
		m.StreamDropped,
	)

	m.initSeries()

	return m
}

func (m *Metrics) initSeries() {
	for _, status := range []string{"succeeded", "failed", "skipped"} {
		for _, trigger := range []string{"schedule", "manual"} {
			m.AggregationRuns.WithLabelValues(status, trigger)
		}
	}

	for _, result := range []string{"created", "duplicate"} {
		m.EventsIngested.WithLabelValues(result)
	}
}

func (m *Metrics) RunFinished(status, trigger string, d time.Duration, rows int) {
	m.AggregationRuns.WithLabelValues(status, trigger).Inc()
	m.AggregationDuration.Observe(d.Seconds())
	if rows > 0 {
		m.AggregationRows.Add(float64(rows))
	}
}

func (m *Metrics) LagSeconds(seconds float64) {
	m.AggregationLag.Set(seconds)
}

func (m *Metrics) EventIngested(created bool) {
	result := "duplicate"
	if created {
		result = "created"
	}
	m.EventsIngested.WithLabelValues(result).Inc()
}

func (m *Metrics) SubscriberAdded()   { m.StreamSubscribers.Inc() }
func (m *Metrics) SubscriberRemoved() { m.StreamSubscribers.Dec() }
func (m *Metrics) EventDropped()      { m.StreamDropped.Inc() }

func (m *Metrics) NewTimer(method, route string) *prometheus.Timer {
	return prometheus.NewTimer(m.HTTPDuration.WithLabelValues(method, route))
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }
