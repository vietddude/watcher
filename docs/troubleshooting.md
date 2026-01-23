# Troubleshooting Guide

This guide addresses common issues, FAQs, and operational tasks for maintaining the Watcher.

## 1. Frequently Asked Questions

### Why is my indexer falling behind?
There are several common reasons for lag:
- **Low `scan_interval`**: If the interval is too high (e.g., 60s for BSC), the indexer can't keep up with new blocks.
- **RPC Rate Limits**: Your providers might be rate-limiting you. Check `watcher_rpc_errors_total` in Prometheus.
- **Database Bottlenecks**: If PostgreSQL is slow, the pipeline will wait for commits. Check `watcher_db_query_latency_seconds`.
- **Large Backfill**: If a backfill task is running, it might consume RPC quota and bandwidth.

### How to handle RPC provider downtime?
Watcher handles this automatically:
- **Failover**: Define multiple providers in `config.yaml`. If search for block fails on provider A, Watcher rotates to provider B.
- **Wait & Retry**: If all providers are down, the pipeline enters an exponential backoff state and resumes once providers are functional.
- **Health Scores**: Providers with high error rates or latency are deprioritized.

### How to re-index from block X?

There are two ways to re-process historical data depending on your goal:

#### A. Targeted Re-indexing (History Fix)
If you just need to re-fetch/re-process a specific range of blocks (e.g., you added a new wallet address and want to backfill its history), **use the Rescan Worker via Redis**. This is the preferred way as it doesn't interrupt the live indexer.

```bash
# Push the range to Redis (e.g., block 10,000 to 20,000)
redis-cli ZADD missing_blocks:ETHEREUM_MAINNET 10000 "10000-20000"
```

#### B. Full Chain Rewind
If you need the main indexing pipeline itself to "start over" from an older block (e.g., to wipe and re-sync everything sequentially):
1. Stop the Watcher.
2. Update the `cursors` table in PostgreSQL:
   ```sql
   UPDATE cursors SET block_number = <X-1> WHERE chain_id = 'YOUR_CHAIN_ID';
   ```
3. Restart the Watcher.

**Recommendation**: Always try the Redis-based rescan first for history fixes. Only use the SQL rewind if you need the main pointer to move backwards permanently.

### How do I monitor performance?
Check the `/metrics` endpoint. Specifically, `watcher_adaptive_batch_size` tells you how aggressively the indexer is pulling blocks to catch up.

## 2. Common Scenarios

### Handling a Massive Reorg
If a reorg exceeds the `finality_blocks` threshold, the indexer might encounter a hash mismatch it cannot resolve automatically. In this case, it will log a critical error and stop to prevent data corruption.
**Solution**: Manually reset the cursor to a known safe block (see "How to re-index from block X").

### Fixing Gaps in Data (Rescan)
If you notice missing blocks in your database but don't want to rewind the main indexer, use the **Rescan Worker**. This worker is designed to fill historical gaps without affecting live indexing.

To trigger a manual rescan, push the desired block range into **Redis**:
```bash
# Using redis-cli
# Format: ZADD missing_blocks:<CHAIN_ID> <START_BLOCK> "<START_BLOCK>-<END_BLOCK>"
redis-cli ZADD missing_blocks:ETHEREUM_MAINNET 1000 "1000-1100"
```
The `rescan.Worker` will automatically pick up this range, split it into chunks, and fetch the missing data in the background.

### Failed Block Recovery
If the indexer fails to fetch or process a specific block (e.g., RPC timeout), it is recorded in the `failed_blocks` table.
- **Automated**: The system's recovery handler will automatically retry these blocks with exponential backoff.
- **Manual Check**: You can monitor `failed_blocks` where `status = 'pending'`. If a block fails too many times, its status may change (e.g., to `abandoned`). To retry an abandoned block, simply set its status back to `pending`.

### RAM Usage is High
Watcher uses an in-memory Bloom filter for wallet addresses. If you have millions of addresses, RAM usage will scale linearly (~20-50 bytes per address).
**Solution**: Optimize your filter configuration or ensure the host has sufficient memory. Database pruning (deleting old blocks) does not reduce RAM usage.
