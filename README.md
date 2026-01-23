# 🦅 Watcher – Low-Cost Blockchain Indexer

Watcher is a **blockchain indexer built to survive on free / public RPC providers**.

It does **not** index the whole chain.
It only pulls **what you actually care about**, and it tries very hard to **not burn RPC quota**.

If your RPC is flaky, rate-limited, or slow - Watcher assumes that’s normal.

## Architecture

```text
[RPC Providers] → [Watcher] → [PostgreSQL]
                      ↓
                [Prometheus/Grafana]
```

## Why Watcher exists

* Free RPCs **rate limit aggressively**
* Public nodes **randomly fail**
* Reorgs **happen**
* Indexing everything = **expensive + pointless**

Watcher is designed around those constraints.


## Design principles

* RPC calls are **expensive**
* Errors are **expected**
* 90% of transactions are **irrelevant**
* Trust **your database**, not the RPC


## What it actually does

### Chains

* EVM chains (Ethereum, BSC, Polygon, …)
* Sui (gRPC)
* Tron (REST)
* Bitcoin (JSON-RPC)

### RPC usage (cost-first)

* Multiple providers per chain
* Daily quota tracking
* Throttling before rate-limit hits
* No aggressive retries

### Reorg handling (cheap)

* Detects reorgs via **parent hash comparison**
* Uses **stored blocks**, no extra RPC calls
* Rolls back locally
* RPC verification only when necessary

### Backfill & recovery

* Detects missing blocks from DB gaps
* Backfill runs slowly in background
* Failed blocks are retried with backoff

### Address filtering

* In-memory filter by default
* Bloom filter optimization for EVM
* Only enrich transactions that match

### Storage

* PostgreSQL (production)
* In-memory (dev / tests)
* Optional pruning via retention policy

### Observability

* Prometheus metrics
* Grafana dashboards
* `/health` and `/metrics` endpoints


## What Watcher is NOT
* ❌ A full-chain analytics engine
* ❌ A replacement for The Graph
* ❌ A high-frequency trading indexer

Watcher is meant to be **backend infrastructure**, not a data warehouse.



## Installation

### Requirements
- **Go 1.25+** (as specified in `go.mod`)
- **Docker & Docker Compose**
- **PostgreSQL 15+**

### Steps

1. **Clone the repository**
   ```bash
   git clone https://github.com/vietddude/watcher.git
   cd watcher
   ```

2. **Setup Configuration**
   ```bash
   cp config.example.yaml config.yaml
   # Edit config.yaml with your RPC providers and database credentials
   ```

3. **Start Dependencies**
   Watcher requires PostgreSQL and Prometheus/Grafana (optional but recommended).
   ```bash
   make docker-up
   ```

4. **Run Migrations**
   ```bash
   make migrate-up
   ```

5. **Build and Install**
   ```bash
   make build
   # Optional: move to bin
   # cp bin/watcher /usr/local/bin/
   ```

## Quick Start (Docker Compose)
If you want to run everything via Docker:
```bash
docker-compose up -d
```

## Manual Run
Once configured and built:
```bash
./bin/watcher
# or
make run
```

## Performance

Watcher is designed to be extremely lightweight and efficient with RPC credits.

| Chain    | RPC calls/block | Backfill speed | RAM usage |
|----------|-----------------|----------------|-----------|
| Ethereum | ~1-3            | ~60 blocks/min | ~200MB    |
| BSC      | ~1-2            | ~120 blocks/min| ~150MB    |
| Sui      | ~2-3 (Hybrid)   | ~3000 seq/min  | ~400MB    |

*Note: RPC calls per block vary based on whether the chain supports `PreFilter`. Sui uses gRPC subscriptions for real-time detection but manually fetches checkpoints and effects via RPC.*

## Monitoring

Watcher exposes a Prometheus `/metrics` endpoint and a `/health` endpoint on the configured `port`.

### Available Metrics
- `watcher_chain_lag`: Number of blocks the indexer is behind chain head.
- `watcher_rpc_calls_total`: Total RPC requests sent, labeled by provider and method.
- `watcher_rpc_quota_remaining`: Daily quota remaining per provider.
- `watcher_reorgs_detected_total`: Total chain reorganizations handled.
- `watcher_db_query_latency_seconds`: Latency of database operations.

### Grafana Dashboard
A pre-configured Grafana dashboard is available in `monitoring/grafana/dashboards/watcher.json`.

### Alerts Example
Prometheus alert rules are provided in `monitoring/alerts.yml`. Example alerts:
- **IndexerFallingBehind**: Alerts if lag > 100 blocks.
- **ProviderHighErrorRate**: Alerts if an RPC provider fails > 10% of requests.
- **QuotaNearExhaustion**: Alerts if < 10% of daily quota remains.

## Data Consumption

Watcher does not provide a built-in REST API for querying blocks. Instead, application developers should query the **PostgreSQL** database directly.

### Core Tables
- `blocks`: Contains indexed block headers.
- `transactions`: Contains filtered transactions that matched your monitored addresses.
- `cursors`: Stores the current sync progress for each chain.

See [Database Schema](migrations/00001_init_schema.sql) for details.

## Troubleshooting

See the [Troubleshooting Guide](docs/troubleshooting.md) for FAQs on:
- Handling RPC downtime
- Re-indexing from a specific block
- Debugging performance issues

## Configuration Guide

## Useful commands

| Command           | Description       |
| ----------------- | ----------------- |
| `make run`        | Run locally       |
| `make test`       | Run tests         |
| `make docker-up`  | Start infra       |
| `make migrate-up` | Run DB migrations |

## Mental model

Watcher is closer to a **smart cron job** than a streaming system.

* Slow is fine (not that slow though)
* Cheap is the goal
* Restart anytime
* Never trust the RPC blindly
