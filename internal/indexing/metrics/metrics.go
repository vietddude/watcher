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
)
