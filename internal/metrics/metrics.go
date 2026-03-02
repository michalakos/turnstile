package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	inflightRequests prometheus.Gauge
	redisErrorsTotal prometheus.Counter
	registry         *prometheus.Registry
}

func New() *Metrics {
	reg := prometheus.NewRegistry()

	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "turnstile_requests_total",
		Help: "Total number of rate limit requests.",
	}, []string{"action", "result"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "turnstile_request_duration_seconds",
		Help:    "Duration of rate limit requests in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"action"})

	inflightRequests := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "turnstile_inflight_requests",
		Help: "Number of rate limit requests currently being processed.",
	})

	redisErrorsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "turnstile_redis_errors_total",
		Help: "Total number of Redis errors.",
	})

	reg.MustRegister(requestsTotal, requestDuration, inflightRequests, redisErrorsTotal)

	return &Metrics{
		requestsTotal:    requestsTotal,
		requestDuration:  requestDuration,
		inflightRequests: inflightRequests,
		redisErrorsTotal: redisErrorsTotal,
		registry:         reg,
	}
}

func (m *Metrics) RecordRequest(action, result string, duration time.Duration) {
	m.requestsTotal.WithLabelValues(action, result).Inc()
	m.requestDuration.WithLabelValues(action).Observe(duration.Seconds())
}

func (m *Metrics) InflightInc() {
	m.inflightRequests.Inc()
}

func (m *Metrics) InflightDec() {
	m.inflightRequests.Dec()
}

func (m *Metrics) RecordRedisError() {
	m.redisErrorsTotal.Inc()
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
