package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_http_requests_total",
		Help: "HTTP requests handled, by method, route pattern and status code.",
	}, []string{"method", "route", "status"})

	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agrelha_http_request_duration_seconds",
		Help:    "HTTP request handling duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	DatastarRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agrelha_datastar_requests_total",
		Help: "Requests carrying the Datastar-Request header.",
	})

	SSEActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agrelha_sse_active_connections",
		Help: "Currently open dashboard SSE streams.",
	})

	SSEOpened = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agrelha_sse_opened_total",
		Help: "Dashboard SSE streams opened.",
	})

	SSEClosed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_sse_closed_total",
		Help: "Dashboard SSE streams closed, by reason.",
	}, []string{"reason"})

	SSEFrames = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agrelha_sse_frames_total",
		Help: "SSE frames written to clients, by kind.",
	}, []string{"kind"})

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
