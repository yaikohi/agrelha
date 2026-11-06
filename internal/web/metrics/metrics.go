package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Dashboard: sum by (status) (rate(agrelha_http_requests_total[5m]))
	// Alert:     sum(rate(agrelha_http_requests_total{status=~"5.."}[5m])) / sum(rate(agrelha_http_requests_total[5m])) > 0.01
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_http_requests_total",
		Help: "HTTP requests handled, by method, route pattern and status code.",
	}, []string{"method", "route", "status"})

	// Dashboard: histogram_quantile(0.99, sum(rate(agrelha_http_request_duration_seconds_bucket[5m])) by (le, route))
	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agrelha_http_request_duration_seconds",
		Help:    "HTTP request handling duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	// Dashboard: rate(agrelha_datastar_requests_total[5m])
	DatastarRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agrelha_datastar_requests_total",
		Help: "Requests carrying the Datastar-Request header.",
	})

	// Dashboard: agrelha_sse_active_connections
	SSEActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agrelha_sse_active_connections",
		Help: "Currently open dashboard SSE streams.",
	})

	// Dashboard: rate(agrelha_sse_opened_total[5m])
	SSEOpened = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agrelha_sse_opened_total",
		Help: "Dashboard SSE streams opened.",
	})

	// Dashboard: sum by (reason) (rate(agrelha_sse_closed_total[5m]))
	SSEClosed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_sse_closed_total",
		Help: "Dashboard SSE streams closed, by reason.",
	}, []string{"reason"})

	// Dashboard: sum by (kind) (rate(agrelha_sse_frames_total[5m]))
	SSEFrames = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_sse_frames_total",
		Help: "SSE frames written to clients, by kind.",
	}, []string{"kind"})

	// Dashboard: sum by (action, result) (rate(agrelha_control_actions_total[5m]))
	ControlActions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_control_actions_total",
		Help: "Valheim server control actions, by action and result.",
	}, []string{"action", "result"})
)

func init() {
	prometheus.MustRegister(
		HTTPRequests,
		HTTPDuration,
		DatastarRequests,
		SSEActive,
		SSEOpened,
		SSEClosed,
		SSEFrames,
		ControlActions,
	)
}

func Handler() http.Handler {
	return promhttp.Handler()
}
