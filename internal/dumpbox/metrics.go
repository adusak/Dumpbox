package dumpbox

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type metrics struct {
	registry       *prometheus.Registry
	uploadedFiles  *prometheus.CounterVec
	uploadedBytes  *prometheus.CounterVec
	uploadRequests *prometheus.CounterVec
	uploadDuration prometheus.Histogram
	activeUploads  prometheus.Gauge
}

func newMetrics() *metrics {
	m := &metrics{
		registry: prometheus.NewRegistry(),
		uploadedFiles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dumpbox",
			Name:      "uploaded_files_total",
			Help:      "Number of files successfully stored, partitioned by pseudonymous user ID.",
		}, []string{"user"}),
		uploadedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dumpbox",
			Name:      "uploaded_bytes_total",
			Help:      "Number of bytes successfully stored, partitioned by pseudonymous user ID.",
		}, []string{"user"}),
		uploadRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dumpbox",
			Name:      "upload_requests_total",
			Help:      "Number of authenticated upload requests, partitioned by pseudonymous user ID and HTTP status code.",
		}, []string{"user", "code"}),
		uploadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "dumpbox",
			Name:      "upload_duration_seconds",
			Help:      "Time spent processing authenticated upload requests.",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 12),
		}),
		activeUploads: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "dumpbox",
			Name:      "active_uploads",
			Help:      "Number of upload requests currently streaming files.",
		}),
	}
	m.registry.MustRegister(
		m.uploadedFiles,
		m.uploadedBytes,
		m.uploadRequests,
		m.uploadDuration,
		m.activeUploads,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *metrics) observeUpload(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		response := &statusResponseWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)

		user := r.Context().Value(identityKey{}).(session)
		m.uploadRequests.WithLabelValues(metricUserID(user), strconv.Itoa(response.statusCode())).Inc()
		m.uploadDuration.Observe(time.Since(started).Seconds())
	})
}

func (m *metrics) recordFile(user session, bytes int64) {
	id := metricUserID(user)
	m.uploadedFiles.WithLabelValues(id).Inc()
	m.uploadedBytes.WithLabelValues(id).Add(float64(bytes))
}

func metricUserID(user session) string {
	return sha256Sum(user.Subject)[:24]
}

type statusResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusResponseWriter) Write(content []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(content)
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) statusCode() int {
	if !w.wroteHeader {
		return http.StatusOK
	}
	return w.status
}
