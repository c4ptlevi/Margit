package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const MetricsPath = "/metrics"

type metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
	checks   *prometheus.CounterVec
	handler  http.Handler
}

func newMetrics() *metrics {
	reg := prometheus.NewRegistry()
	m := &metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "margit_http_requests_total",
			Help: "HTTP requests by method, route and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "margit_http_request_duration_seconds",
			Help:    "HTTP request latency by method and route.",
			Buckets: []float64{.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "margit_http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
		checks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "margit_check_results_total",
			Help: "Check decisions by result.",
		}, []string{"allowed"}),
	}
	reg.MustRegister(
		m.requests, m.duration, m.inFlight, m.checks,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	m.handler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg})
	return m
}

func (m *metrics) observe(r *http.Request, status int, took time.Duration) {
	route := "unmatched"
	if r.Pattern != "" {
		_, path, ok := strings.Cut(r.Pattern, " ")
		if !ok {
			path = r.Pattern
		}
		route = path
	}
	m.requests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(r.Method, route).Observe(took.Seconds())
}

func (m *metrics) checkResult(allowed bool) {
	m.checks.WithLabelValues(strconv.FormatBool(allowed)).Inc()
}
