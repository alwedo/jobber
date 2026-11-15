package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var Enabled bool

// Metrics collection
var (
	httpRequests = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_requests_seconds",
			Help:    "HTTP request duration by method, route, and status code.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "code"},
	)

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests by method, route, and status code.",
		},
		[]string{"method", "route", "code"},
	)

	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "In-flight HTTP requests.",
		},
	)

	JobberScheduledQueries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "jobber_schedulded_queries",
			Help: "Number of currently scheduled queries.",
		},
	)

	JobberNewQueries = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "jobber_queries_created_total",
			Help: "Total number of queries created.",
		},
	)
)

func Init() {
	Enabled = true
	prometheus.MustRegister(
		httpRequests,
		httpRequestsTotal,
		httpRequestsInFlight,
		JobberScheduledQueries,
		JobberNewQueries,
	)
}

func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" { // skip telemetry for the telemetry endpoint
			next.ServeHTTP(w, r)
			return
		}
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 0}
		next.ServeHTTP(rec, r)
		d := time.Since(start).Seconds()
		code := rec.code
		if code == 0 {
			code = http.StatusOK
		}
		method := methodLabel(r.Method)
		route := routeLabel(r)
		httpRequests.WithLabelValues(method, route, strconv.Itoa(code)).Observe(d)
		httpRequestsTotal.WithLabelValues(method, route, strconv.Itoa(code)).Inc()
	})
}

func routeLabel(r *http.Request) string {
	if r.Pattern == "" {
		return "unmatched"
	}
	return r.Pattern
}

func methodLabel(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.code != 0 {
		return
	}
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
		r.ResponseWriter.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}
