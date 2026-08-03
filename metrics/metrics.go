package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics collection
var (
	httpRequests = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_requests_seconds",
			Help:    "Duration, method, path, code.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "code"},
	)

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests.",
		},
		[]string{"method", "path", "code"},
	)

	httpRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "In-flight HTTP requests.",
		},
		[]string{"path"},
	)

	// Labels: "id", "tags", "cron"
	JobberScheduledQueries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "jobber_schedulded_queries",
			Help: "Total Schedulded Queries.",
		},
		[]string{"id", "tags", "cron"},
	)

	// Labels: "keywords", "location"
	JobberNewQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "jobber_new_queries",
			Help: "Total new Queries created.",
		},
		[]string{"keywords", "location"},
	)

	// Labels: "portal", "keywords", "location", itemCount
	ScraperJob = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "scraper_job_seconds",
			Help:    "Scraper Job information.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"portal", "keywords", "location", "itemCount"},
	)
)

func Init() {
	prometheus.MustRegister(
		httpRequests,
		httpRequestsTotal,
		httpRequestsInFlight,
		JobberScheduledQueries,
		JobberNewQueries,
		ScraperJob,
	)
}

func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" { // skip telemetry for the telemetry endpoint
			next.ServeHTTP(w, r)
			return
		}
		httpRequestsInFlight.WithLabelValues(r.URL.Path).Inc()
		defer httpRequestsInFlight.WithLabelValues(r.URL.Path).Dec()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: 0}
		next.ServeHTTP(rec, r)
		d := time.Since(start).Seconds()
		code := rec.code
		if code == 0 {
			code = http.StatusOK
		}
		httpRequests.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(code)).Observe(d)
		httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(code)).Inc()
	})
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
