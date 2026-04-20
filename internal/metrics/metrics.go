package metrics

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by method, path, and status.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency by method and path.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	SSEActiveConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "sse_active_connections",
		Help: "Active SSE connections across all streaming endpoints.",
	})

	EventBusDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "eventbus_dropped_total",
		Help: "Event bus messages dropped due to full subscriber channels.",
	})

	// RunnerWebhookTotal counts outbound runner webhook calls by outcome.
	// The result label is bounded to {success, failure}.
	RunnerWebhookTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "runner_webhook_total",
		Help: "Outbound runner webhook calls labeled by result.",
	}, []string{"result"})

	// GitSyncDuration uses wider buckets than DefBuckets because repo
	// commits routinely exceed 10s when pushing to a remote or staging
	// a large change set.
	GitSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "gitsync_duration_seconds",
		Help:    "Duration of git commit operations.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 15, 30, 60},
	})

	StallScanDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "stall_scan_duration_seconds",
		Help:    "Duration of heartbeat stall scanner ticks.",
		Buckets: prometheus.DefBuckets,
	})

	StallCardsMarked = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "stall_cards_marked_total",
		Help: "Cards transitioned to stalled by the heartbeat scanner.",
	})
)

// Register registers all metrics with the given registerer. Re-registering an
// already-registered collector is not an error; tests and potential hot-reload
// paths can call Register more than once without panicking.
func Register(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		HTTPRequestsTotal,
		HTTPRequestDuration,
		SSEActiveConnections,
		EventBusDropped,
		RunnerWebhookTotal,
		GitSyncDuration,
		StallScanDuration,
		StallCardsMarked,
	}

	for _, c := range collectors {
		if err := reg.Register(c); err != nil {
			var are prometheus.AlreadyRegisteredError
			if !errors.As(err, &are) {
				panic(err)
			}
		}
	}
}
