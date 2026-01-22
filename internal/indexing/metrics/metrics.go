package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// BlocksProcessed tracks total blocks processed per chain
	BlocksProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_blocks_processed_total",
			Help: "Total number of blocks processed",
		},
		[]string{"chain"},
	)

	// RPCCallsTotal tracks RPC calls per chain and provider
	RPCCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_rpc_calls_total",
			Help: "Total number of RPC calls",
		},
		[]string{"chain", "provider", "method"},
	)

	// RPCErrorsTotal tracks RPC errors per chain and provider
	RPCErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_rpc_errors_total",
			Help: "Total number of RPC errors",
		},
		[]string{"chain", "provider", "error_type"},
	)

	// RPCLatency tracks RPC call latency
	RPCLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "watcher_rpc_latency_seconds",
			Help:    "RPC call latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"chain", "provider", "method"},
	)

	// ChainLatestBlock tracks the latest block height of the chain
	ChainLatestBlock = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_chain_latest_block",
			Help: "Latest block height of the chain",
		},
		[]string{"chain"},
	)

	// IndexerLatestBlock tracks the latest block indexed by the watcher
	IndexerLatestBlock = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_indexer_latest_block",
			Help: "Latest block height indexed by the watcher",
		},
		[]string{"chain"},
	)

	// ChainLag tracks the lag between chain head and indexer cursor
	ChainLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_chain_lag",
			Help: "Lag between chain head and indexer cursor",
		},
		[]string{"chain"},
	)

	// RPCProviderHealthScore tracks provider health score (0-100)
	RPCProviderHealthScore = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_rpc_provider_health_score",
			Help: "Provider health score (0-100, higher is better)",
		},
		[]string{"chain", "provider"},
	)

	// RPCProviderQuotaUsage tracks provider quota usage percentage
	RPCProviderQuotaUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_rpc_provider_quota_usage_ratio",
			Help: "Provider quota usage as a ratio (0-1)",
		},
		[]string{"chain", "provider"},
	)

	// RPCProviderLatencySeconds tracks provider average latency
	RPCProviderLatencySeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_rpc_provider_latency_seconds",
			Help: "Provider average response latency in seconds",
		},
		[]string{"chain", "provider"},
	)

	// BackfillBlocksQueued tracks number of blocks queued for backfill
	BackfillBlocksQueued = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_backfill_blocks_queued",
			Help: "Number of blocks queued for backfill",
		},
		[]string{"chain"},
	)

	// BackfillBlocksProcessed tracks total blocks processed by backfill
	BackfillBlocksProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_backfill_blocks_processed_total",
			Help: "Total number of blocks processed by backfill",
		},
		[]string{"chain"},
	)

	// ReorgsDetected tracks total reorgs detected
	ReorgsDetected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_reorgs_detected_total",
			Help: "Total number of reorgs detected",
		},
		[]string{"chain"},
	)

	// ReorgDepth tracks depth of reorgs in blocks
	ReorgDepth = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "watcher_reorg_depth_blocks",
			Help:    "Depth of reorgs in blocks",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100},
		},
		[]string{"chain"},
	)

	// DBQueryLatency tracks database query latency
	DBQueryLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "watcher_db_query_latency_seconds",
			Help:    "Database query latency",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"}, // "insert_block", "get_block", etc.
	)

	// DBConnectionPoolUsage tracks database connection pool usage percentage
	DBConnectionPoolUsage = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "watcher_db_connection_pool_usage",
			Help: "Database connection pool usage percentage",
		},
	)

	// RPCQuotaRemaining tracks remaining RPC calls in daily quota
	RPCQuotaRemaining = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_rpc_quota_remaining",
			Help: "Remaining RPC calls in daily quota",
		},
		[]string{"chain", "provider"},
	)

	// TransactionsFiltered tracks transactions filtered out (not matching addresses)
	TransactionsFiltered = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_transactions_filtered_total",
			Help: "Transactions filtered out (not matching addresses)",
		},
		[]string{"chain"},
	)

	// TransactionsProcessed tracks transactions actually processed (matched filter)
	TransactionsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_transactions_processed_total",
			Help: "Transactions actually processed (matched filter)",
		},
		[]string{"chain"},
	)

	// ChainAvailable tracks if a chain has any functional RPC providers (0 or 1)
	ChainAvailable = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_chain_available",
			Help: "Chain availability (1 if at least one provider is functional, 0 otherwise)",
		},
		[]string{"chain"},
	)

	// AdaptiveScanIntervalSeconds tracks the current adaptive scan interval
	AdaptiveScanIntervalSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_adaptive_scan_interval_seconds",
			Help: "Current adaptive scan interval in seconds",
		},
		[]string{"chain"},
	)

	// AdaptiveBatchSize tracks the current adaptive batch size
	AdaptiveBatchSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_adaptive_batch_size",
			Help: "Current adaptive batch size for block fetching",
		},
		[]string{"chain"},
	)

	// AdaptiveAdjustmentsTotal tracks total number of throttle adjustments
	AdaptiveAdjustmentsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_adaptive_adjustments_total",
			Help: "Total number of adaptive throttle adjustments",
		},
		[]string{"chain", "type", "direction"}, // type: interval/batch, direction: up/down
	)
)
