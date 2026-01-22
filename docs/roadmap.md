# Multi-Chain Indexer - Implementation Roadmap

## Legend
- ✅ Complete
- 🔄 In Progress
- ⏳ Planned
- ❌ Not Started

---

## Phase 1: Core Pipeline (COMPLETE) ✅

### Indexing Pipeline
- [x] Block fetching with cursor management
- [x] Transaction fetching and parsing
- [x] Bloom filter for address matching
- [x] Event emission via finality buffer
- [x] Lazy receipt fetching (only for matched txs)

### Multi-Chain Support
- [x] Chain adapter interface
- [x] EVM adapter implementation
- [x] Per-chain configuration
- [x] Nested providers in config.yaml

### RPC Management
- [x] Multi-provider support with failover
- [x] Budget tracking per chain/provider
- [x] CoordinatedProvider wrapper
- [x] Router with rotation strategies

---

## Phase 2: Reliability (COMPLETE) ✅

### Reorg Handling
- [x] Parent hash verification detection
- [x] Configurable max reorg depth
- [x] Rollback handler (mark orphaned blocks)
- [x] Finality buffer (12 blocks for EVM) - prevents most reorg issues
- [~] Emit revert events to downstream *(optional - only if emitting before finality)*
- [~] Verify finalized blocks periodically *(optional - extra paranoia)*

### Missing Block Detection
- [x] Gap detector
- [x] Backfill processor with rate limiting
- [x] Persistent missing block queue (Redis ZSET)
- [x] Priority ordering (score = start block)

### Failed Block Recovery
- [x] Basic recovery handler
- [x] Persistent failed block queue (Redis)
- [x] Exponential backoff retry (2s, 4s, 8s, 16s, 32s, max 60s)
- [x] Max retry tracking (default 5 attempts)

### Redis Rescan Pipeline (NEW)
- [x] Redis integration (go-redis)
- [x] Config: `redis.url`, `chains.*.id`, `chains.*.rescan_ranges`
- [x] Queue consumer: `missing_blocks:<id>` (ZSET)
- [x] Range merge/split logic (chunk_size: 5-500 blocks)
- [x] Processing lock: `processing:<id>:<range>` with TTL
- [x] Progress tracking: `processed:<id>:<range>`
- [x] Re-queue remaining subrange on timeout/interruption
- [x] Multi-instance safe (locks prevent double work)
- [x] CLI flag: `--rescan-ranges=true|false`

---

## Phase 3: Monitoring & Observability (IN PROGRESS) 🔄

### Health System
- [x] Health monitor with chain status
- [x] HTTP health endpoints (/health)
- [x] Detailed health endpoint (/health/detailed)
- [x] Prometheus metrics endpoint (/metrics)

### Metrics Implemented
- [x] watcher_blocks_processed_total
- [x] watcher_transactions_processed_total
- [x] watcher_current_block_lag
- [x] watcher_rpc_quota_used_percent
- [x] watcher_failed_blocks_count
- [x] watcher_missing_blocks_count
- [x] watcher_reorgs_detected_total
- [x] watcher_events_emitted_total

### Logging
- [x] Structured logging (slog)
- [x] Log levels configurable via config and CLI (`--log-level`)
- [ ] Trace IDs for request tracking

---

## Phase 4: Persistence 🚧

### Database Storage
- [x] PostgreSQL block repository
- [x] PostgreSQL transaction repository
- [x] PostgreSQL cursor repository
- [x] PostgreSQL missing block queue
- [x] PostgreSQL failed block queue

### Migrations
- [x] Schema for blocks table
- [x] Schema for transactions table
- [x] Schema for cursors table
- [x] Schema for queues table

---

## Phase 5: Admin & Operations ❌

### Admin API
- [ ] POST /admin/pause/:chain
- [ ] POST /admin/resume/:chain
- [ ] POST /admin/backfill/:chain?from=X&to=Y
- [ ] POST /admin/reorg-check/:chain
- [ ] DELETE /admin/failed-blocks/:chain

### Configuration
- [ ] Runtime config reload
- [ ] Per-chain enable/disable
- [ ] Dynamic scan interval adjustment

---

## Phase 6: Advanced Features ❌

### Adaptive Throttling
- [ ] Auto-adjust scan interval based on lag
- [ ] Auto-adjust batch size based on RPC performance
- [ ] Pause low-priority chains when quota critical

### Event Confidence Levels
- [ ] pending (1 confirmation)
- [ ] confirming (6 confirmations)
- [ ] confirmed (12+ confirmations)

### Bitcoin Adapter
- [ ] UTXO-based filtering
- [ ] Different finality rules

---

## Current Priority Order

1. **Prometheus Metrics** - Essential for production monitoring
2. **PostgreSQL Persistence** - Required for production
3. **Persistent Queues** - Survive restarts
4. **Admin API** - Operational control
5. **Adaptive Throttling** - Self-healing

---

## Quick Start Commands

```bash
# Run indexer
go run ./cmd/watcher/main.go

# Run tests
go test ./...

# Build binary
go build -o watcher ./cmd/watcher

# Check health
curl http://localhost:8080/health
```

---

## Configuration Reference

```yaml
# config.yaml
chains:
  - id: "ethereum-mainnet"
    type: "evm"
    finality_blocks: 12
    scan_interval: 5s
    providers:
      - name: "alchemy"
        url: "https://..."
      - name: "infura"
        url: "https://..."

backfill:
  blocks_per_minute: 10
  min_interval: 2s

reorg:
  max_depth: 100

budget:
  daily_quota: 100000
```
