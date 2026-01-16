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

	// CurrentBlockLag tracks how far behind the indexer is
	CurrentBlockLag = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_current_block_lag",
			Help: "Current block lag (chain head - indexed block)",
		},
		[]string{"chain"},
	)

	// TransactionsProcessed tracks total transactions processed per chain
	TransactionsProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_transactions_processed_total",
			Help: "Total number of transactions processed",
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

	// RPCQuotaUsedPercent tracks quota usage percentage
	RPCQuotaUsedPercent = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_rpc_quota_used_percent",
			Help: "RPC quota usage percentage",
		},
		[]string{"chain"},
	)

	// FailedBlocksCount tracks current failed blocks in queue
	FailedBlocksCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_failed_blocks_count",
			Help: "Number of failed blocks in queue",
		},
		[]string{"chain"},
	)

	// MissingBlocksCount tracks current missing blocks in queue
	MissingBlocksCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "watcher_missing_blocks_count",
			Help: "Number of missing blocks in queue",
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

	// EventsEmitted tracks total events emitted
	EventsEmitted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_events_emitted_total",
			Help: "Total number of events emitted",
		},
		[]string{"chain", "event_type"},
	)

	// RescanRangesProcessed tracks rescan ranges processed
	RescanRangesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_rescan_ranges_processed_total",
			Help: "Total number of rescan ranges processed",
		},
		[]string{"chain"},
	)

	// RescanBlocksProcessed tracks blocks processed via rescan
	RescanBlocksProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_rescan_blocks_processed_total",
			Help: "Total number of blocks processed via rescan",
		},
		[]string{"chain"},
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

	// RPCFailoverTotal tracks RPC provider failover events
	RPCFailoverTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_rpc_failover_total",
			Help: "Total number of RPC provider failovers",
		},
		[]string{"chain", "from_provider", "to_provider", "reason"},
	)

	// RPCRetryTotal tracks RPC call retries
	RPCRetryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "watcher_rpc_retry_total",
			Help: "Total number of RPC call retries",
		},
		[]string{"chain", "provider"},
	)
)
