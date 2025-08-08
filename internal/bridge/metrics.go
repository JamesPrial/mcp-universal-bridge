package bridge

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// Metrics contains Prometheus metrics
type Metrics struct {
	MessagesProcessed *prometheus.CounterVec
	MessageLatency    *prometheus.HistogramVec
	ActiveConnections prometheus.Gauge
	ErrorCount        *prometheus.CounterVec
	MessageSize       *prometheus.HistogramVec
}

// NewMetrics creates new metrics
func NewMetrics() *Metrics {
	return &Metrics{
		MessagesProcessed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mcp_bridge_messages_total",
				Help: "Total number of messages processed",
			},
			[]string{"direction", "status"},
		),
		
		MessageLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mcp_bridge_message_latency_seconds",
				Help:    "Message processing latency in seconds",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
			},
			[]string{"operation"},
		),
		
		ActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "mcp_bridge_active_connections",
				Help: "Number of active connections",
			},
		),
		
		ErrorCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mcp_bridge_errors_total",
				Help: "Total number of errors",
			},
			[]string{"type", "operation"},
		),
		
		MessageSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mcp_bridge_message_size_bytes",
				Help:    "Size of messages in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 7), // 100B to 100MB
			},
			[]string{"direction"},
		),
	}
}

// Serve starts the metrics HTTP server
func (m *Metrics) Serve(port int) {
	addr := fmt.Sprintf(":%d", port)
	
	http.Handle("/metrics", promhttp.Handler())
	
	// Also add a simple health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	log.Info().Str("addr", addr).Msg("Starting metrics server")
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Error().Err(err).Msg("Metrics server failed")
	}
}

