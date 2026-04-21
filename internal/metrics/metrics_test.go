package metrics_test

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mhersson/contextmatrix/internal/metrics"
)

// descNames collects the fully qualified metric names from a Collector's
// Describe output. Vec collectors emit descriptors even before any label
// combinations are observed, making Describe the correct way to assert
// registration without forcing observations.
func descNames(c prometheus.Collector) []string {
	ch := make(chan *prometheus.Desc, 16)

	go func() {
		c.Describe(ch)
		close(ch)
	}()

	var names []string

	for d := range ch {
		// Desc.String() looks like: Desc{fqName: "http_requests_total", ...}
		s := d.String()

		const prefix = `fqName: "`

		if i := strings.Index(s, prefix); i >= 0 {
			rest := s[i+len(prefix):]
			if j := strings.Index(rest, `"`); j >= 0 {
				names = append(names, rest[:j])
			}
		}
	}

	return names
}

func TestRegister_AllMetricsPresent(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.Register(reg)

	// Non-vec collectors appear in Gather() immediately after registration.
	type gatherCase struct {
		name       string
		metricType dto.MetricType
	}

	gatherCases := []gatherCase{
		{"contextmatrix_sse_active_connections", dto.MetricType_GAUGE},
		{"contextmatrix_eventbus_dropped_total", dto.MetricType_COUNTER},
		{"contextmatrix_gitsync_duration_seconds", dto.MetricType_HISTOGRAM},
		{"contextmatrix_stall_scan_duration_seconds", dto.MetricType_HISTOGRAM},
		{"contextmatrix_stall_cards_marked_total", dto.MetricType_COUNTER},
		{"contextmatrix_card_cache_size", dto.MetricType_GAUGE},
		{"contextmatrix_card_cache_miss_total", dto.MetricType_COUNTER},
		{"contextmatrix_commit_queue_depth", dto.MetricType_GAUGE},
		{"contextmatrix_commit_duration_seconds", dto.MetricType_HISTOGRAM},
		{"contextmatrix_commit_errors_total", dto.MetricType_COUNTER},
		{"contextmatrix_parent_autotransition_errors_total", dto.MetricType_COUNTER},
		{"contextmatrix_rollback_failures_total", dto.MetricType_COUNTER},
	}

	mfs, err := reg.Gather()
	require.NoError(t, err)

	gathered := make(map[string]dto.MetricType, len(mfs))
	for _, mf := range mfs {
		gathered[mf.GetName()] = mf.GetType()
	}

	for _, tc := range gatherCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()

			gotType, ok := gathered[tc.name]
			assert.True(t, ok, "metric family %q not found in registry", tc.name)

			if ok {
				assert.Equal(t, tc.metricType, gotType,
					"metric %q: expected type %v, got %v", tc.name, tc.metricType, gotType)
			}
		})
	}

	// Vec collectors only appear in Gather() after observations; use Describe.
	type vecCase struct {
		name      string
		collector prometheus.Collector
	}

	vecCases := []vecCase{
		{"contextmatrix_http_requests_total", metrics.HTTPRequestsTotal},
		{"contextmatrix_http_request_duration_seconds", metrics.HTTPRequestDuration},
		{"contextmatrix_runner_webhook_total", metrics.RunnerWebhookTotal},
	}

	for _, tc := range vecCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()

			names := descNames(tc.collector)
			assert.Contains(t, names, tc.name,
				"Vec collector descriptor for %q not found; got descriptors: %v", tc.name, names)
		})
	}
}

// TestRegister_Idempotent verifies that calling Register against the same
// registry twice does not panic and does not produce a duplicate error.
func TestRegister_Idempotent(t *testing.T) {
	reg := prometheus.NewRegistry()

	require.NotPanics(t, func() { metrics.Register(reg) })
	require.NotPanics(t, func() { metrics.Register(reg) })
}
