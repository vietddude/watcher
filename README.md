# Watcher - EVM Blockchain Indexer

Watcher is a high-performance, modular, and resilient blockchain indexer designed to ingest, filter, and persist blockchain data. It serves as a "Control Plane" for your blockchain data infrastructure.

## 🚀 Key Features

*   **Multi-Chain Support**: Index multiple EVM chains simultaneously.
*   **Resilient Indexing**: Handles reorgs, RPC failures, and missing blocks automatically.
*   **PostgreSQL Persistence**: Robust data storage for blocks, transactions, and state.
*   **Wallet Filtering**: Bloom Filter-based optimization to only index relevant transactions.
*   **Redis Rescan**: Background worker system for re-scanning historical data ranges.
*   **RPC Management**: Smart router with budget tracking and failover.
*   **Observability**: Built-in Prometheus metrics and Grafana dashboards.

## 🏗 Architecture

See [Architecture Documentation](docs/architecture.md) for detailed design.

## 🛠 Prerequisites

*   Go 1.22+
*   Docker & Docker Compose
*   Make

## ⚡ Quick Start

1.  **Start Dependencies** (Postgres, Redis, Prometheus, Grafana):
    ```bash
    make docker-up
    ```

2.  **Configuration**:
    The default `config.yaml` is ready for local development. Adjust `chains` and `providers` as needed.

3.  **Run the Indexer**:
    ```bash
    make run
    ```

## 📦 Key Commands

| Command | Description |
|---------|-------------|
| `make build` | Build the binary to `bin/watcher` |
| `make run` | Build and run locally |
| `make test` | Run unit tests |
| `make docker-up` | Start infrastructure |
| `make migrate-up` | Run database migrations manually |

## 🔧 Configuration (`config.yaml`)

```yaml
chains:
  - chain_id: "1"
    internal_code: "ETH_MAINNET"
    providers:
      - name: "public-node"
        url: "https://ethereum-rpc.publicnode.com"

database:
  url: "postgres://watcher:watcher123@localhost:5432/watcher?sslmode=disable"

backfill:
  blocks_per_minute: 60

budget:
  daily_quota: 100000
```
