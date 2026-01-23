# Monitoring & Observability

Watcher provides deep visibility into its internal state via Prometheus metrics and a built-in health monitor.

## 1. Metrics Reference

### Chain Sync
- `watcher_chain_latest_block`: Latest block number reported by RPC providers.
- `watcher_indexer_latest_block`: Latest block number processed and stored in database.
- `watcher_chain_lag`: `latest_block - indexer_block`.
- `watcher_blocks_processed_total`: Cumulative count of blocks processed.

### RPC Infrastructure
- `watcher_rpc_calls_total{chain, provider, method}`: Granular tracking of all RPC traffic.
- `watcher_rpc_errors_total{chain, provider, error_type}`: Tracks rate-limits, timeouts, and JSON-RPC errors.
- `watcher_rpc_latency_seconds`: RPC round-trip time distribution.
- `watcher_rpc_quota_remaining`: Remaining credits for each provider today.
- `watcher_rpc_provider_health_score`: Current health rank (0-100) used for routing.

### Internal Logic
- `watcher_reorgs_detected_total`: How many reorgs were detected.
- `watcher_reorg_depth_blocks`: Histogram of reorg sizes.
- `watcher_transactions_filtered_total`: Tx filtered out by Bloom filter.
- `watcher_transactions_processed_total`: Matched Tx stored in DB.
- `watcher_adaptive_batch_size`: Current number of blocks fetched per request.

### Database
- `watcher_db_query_latency_seconds`: Latency of SQL operations (insert/update).
- `watcher_db_connection_pool_usage`: Current usage of the PGX pool.

## 2. Health Check API

The `/health` endpoint serves as a readiness/liveness probe.

### Standard Health (`/health`)
Returns `200 OK` if the system is functional. Returns `503 Service Unavailable` if:
- All RPC providers for a chain are down.
- Database connection is lost.
- A critical component crashed.

### Detailed Status (`/health/detailed`)
Returns a JSON report of all chains and their health status:
```json
[
  {
    "chain_id": "ETHEREUM_MAINNET",
    "status": "healthy",
    "lag": 0,
    "functional_providers": 2,
    "last_error": ""
  }
]
```

## 3. Alerts Configuration

We recommend setting up the following alerts in Prometheus:

| Alert | Condition | Severity |
|-------|-----------|----------|
| `IndexerStalled` | `increase(watcher_blocks_processed_total[5m]) == 0` | Critical |
| `HighLag` | `watcher_chain_lag > 50` | Warning |
| `QuotaExhausted` | `watcher_rpc_quota_remaining < 1000` | Warning |
| `HighReorgRate` | `rate(watcher_reorgs_detected_total[1h]) > 5` | Critical |

See `monitoring/alerts.yml` for actual YAML definitions.
