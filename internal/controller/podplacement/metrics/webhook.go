package metrics

import (
	"sync"

	"github.com/openshift/multiarch-tuning-operator/pkg/utils"

	metrics2 "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ProcessedPodsWH prometheus.Counter
	GatedPods       prometheus.Counter
	ResponseTime    prometheus.Histogram

	// CEL-related metrics for Phase 0+
	CELProcessedPodsWH        prometheus.Counter
	CELImmediatePodsWH        prometheus.Counter
	CELSkippedPodsWH          prometheus.Counter
	CELEvaluationErrorsWH     prometheus.Counter
	CELContextTimeoutWH       prometheus.Counter
	CELFallbackToGateWH       prometheus.Counter
	CELWebhookDurationSeconds prometheus.Histogram
)

var onceWebhook sync.Once

func InitWebhookMetrics() {
	onceWebhook.Do(initWebhookMetrics)
}

func initWebhookMetrics() {
	initCommonMetrics()
	ProcessedPodsWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_pods_processed_total",
			Help: "The total number of pods processed by the webhook",
		},
	)
	GatedPods = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_pods_gated_total",
			Help: "The total number of pods gated by the webhook",
		},
	)

	ResponseTime = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "mto_ppo_wh_response_time_seconds",
			Help:    "The response time of the webhook",
			Buckets: utils.Buckets(),
		},
	)

	// CEL-related metrics
	CELProcessedPodsWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_cel_processed_pods_total",
			Help: "Total pods successfully processed by CEL in webhook",
		},
	)

	CELImmediatePodsWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_cel_immediate_pods_total",
			Help: "Total CEL pods that scheduled immediately without scheduling gate",
		},
	)

	CELSkippedPodsWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_cel_skipped_pods_total",
			Help: "Total pods that skipped CEL (no PPC or plugin disabled)",
		},
	)

	CELEvaluationErrorsWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_cel_evaluation_errors_total",
			Help: "Total CEL evaluation failures (not including timeouts)",
		},
	)

	CELContextTimeoutWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_cel_context_timeout_total",
			Help: "Total CEL evaluations cancelled due to context timeout",
		},
	)

	CELFallbackToGateWH = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mto_ppo_wh_cel_fallback_to_gate_total",
			Help: "Total pods that fell back to scheduling gate due to CEL panic",
		},
	)

	CELWebhookDurationSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "mto_ppo_wh_cel_duration_seconds",
			Help:    "Duration of CEL evaluation in webhook",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		},
	)

	metrics2.Registry.MustRegister(
		ProcessedPodsWH,
		GatedPods,
		ResponseTime,
		CELProcessedPodsWH,
		CELImmediatePodsWH,
		CELSkippedPodsWH,
		CELEvaluationErrorsWH,
		CELContextTimeoutWH,
		CELFallbackToGateWH,
		CELWebhookDurationSeconds,
	)
}
