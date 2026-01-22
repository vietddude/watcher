# 🦅 Watcher – Low-Cost Blockchain Indexer

Watcher is a **blockchain indexer built to survive on free / public RPC providers**.

It does **not** index the whole chain.
It only pulls **what you actually care about**, and it tries very hard to **not burn RPC quota**.

If your RPC is flaky, rate-limited, or slow - Watcher assumes that’s normal.


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


## Quick start

### 1. Start dependencies

```bash
make docker-up
```

### 2. Config (config.yaml)

```yaml
chains:
  - id: "ETHEREUM_MAINNET"
    type: "evm"
    scan_interval: 12s
    finality_blocks: 12
    providers:
      - name: "public"
        url: "https://ethereum-rpc.publicnode.com"
        daily_quota: 100000

database:
  url: "postgres://watcher:watcher123@localhost:5432/watcher?sslmode=disable"
```

### 3. Run

```bash
make run
```

## Configuration Guide

To ensure Watcher runs reliably, you must configure each chain correctly in `config.yaml`:

### 1. Chain ID (`id`)
Use descriptive identifiers instead of numbers (e.g., `BITCOIN_MAINNET` instead of `0`). This serves as the unique key for storing cursors in the database.

### 2. Chain Type (`type`)
Watcher supports four main chain architectures:
*   `evm`: For Ethereum, BSC, Polygon, Arbitrum, etc.
*   `bitcoin`: For the Bitcoin network (uses JSON-RPC 1.0).
*   `sui`: For the Sui network (uses gRPC).
*   `tron`: For the Tron network (uses REST).

### 3. Scan Interval (`scan_interval`)
The delay between new block polls.
*   **EVM/Sui**: Recommended `5s` - `12s` due to fast block times.
*   **Bitcoin**: Recommended `1m` - `10m`. Since Bitcoin averages 10-minute block times, polling too frequently will result in repetitive "Out of range" logs.

### 4. Finality Blocks (`finality_blocks`)
The number of confirmation blocks required to safely avoid reorgs.
*   Ethereum: `12`
*   Bitcoin: `6`
*   Polygon: `32` (suggested for higher safety)

### 5. Providers
Multiple providers can be defined for failover support. Use `daily_quota` to ensure Watcher automatically stops using a provider once its free tier limit is reached, preventing API key suspension.

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
