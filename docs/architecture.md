# System Architecture & Data Flows

## 1. High-Level Architecture

The **Watcher** is a modular, event-driven indexing system. It assumes the role of a "Control Plane" orchestrating data flow from blockchains to your application's storage or event bus.

```mermaid
graph TD
    User[User/App] --> API[Control Plane API]
    API --> Watcher[Watcher Engine]
    
    subgraph "Core Engine"
        Watcher --> Pipeline[Indexing Pipeline]
        Pipeline --> Adapter[Chain Adapter]
        Pipeline --> Filter[Filter Engine]
        Pipeline --> Reorg[Reorg Detector]
        Pipeline --> Cursor[Cursor Manager]
    end
    
    subgraph "Storage Layer"
        Pipeline --> PG[(Postgres DB)]
        Cursor --> PG
        Filter --"Load Data"--> PG
    end

    subgraph "Rescan System"
        Worker[Rescan Worker] --> Redis[(Redis)]
        Worker --> PG
    end
    
    Adapter --> Coordinator[RPC Coordinator]
    Coordinator --> RPC[Blockchain Node(s)]
```

## 2. Component Design

### Control Plane (`internal/control`)
- **Watcher**: Composition root. Wires all dependencies including DB, Redis, and Indexers.
- **Health**: Monitored via `health.Monitor`. Exposes HTTP `/health`.

### Indexing Pipeline (`internal/indexing/indexer/pipeline.go`)
The heart of the system. Runs in a loop:
1.  **Fetch**: Get next block via `ChainAdapter`.
2.  **Reorg Check**: Verify parent hash consistency.
3.  **Process**:
    - Extract Transactions.
    - **Filter**: Check against `WalletAddress` Bloom Filter.
    - Emit events (`Emitter`).
4.  **Commit**: Update Cursor (`CursorRepo`) and persist data (`BlockRepo`, `TxRepo`).

### RPC Management (`internal/infra/rpc`)
- **Coordinator**: Manages multiple RPC providers per chain.
- **Router**: Routes requests based on health and budget.
- **BudgetTracker**: Enforces global and per-chain rate limits.

### Data Models (`internal/core/domain`)
- **Block**: Normalized block header.
- **Transaction**: Normalized tx data.
- **WalletAddress**: Monitored addresses for filtering.
- **Cursor**: State checkpoint.
- **Missing/Failed Block**: Recovery queue items.

## 3. Storage Layer

### PostgreSQL
Primary persistence for all indexed data.
- **Schema**: Managed via `goose` migrations.
    - `blocks`: Indexed blocks.
    - `transactions`: Filtered transactions.
    - `cursors`: Indexer progress.
    - `wallet_addresses`: Monitored targets.
    - `missing_blocks`: Backfill queue.
    - `failed_blocks`: Retry queue.
- **Repositories**: `internal/infra/storage/postgres` (using `sqlx`).

### Redis
Used for coordination of heavy background tasks.
- **Rescan Ranges**: Queue for re-scanning past blocks.
- **Locks**: Distributed locking for workers.

## 4. Wallet Filtering (Bloom Filter)

To optimize storage and processing, the Watcher uses a filtering mechanism:
1.  **Storage**: `wallet_addresses` table stores interested addresses.
2.  **In-Memory**: On startup, addresses are loaded into a `SimpleFilter` (Bloom Filter equivalent).
3.  **Pipeline**: Every transaction is checked against the filter. Only matches are persisted and emitted.

## 5. Failure Recovery

1.  **Transient Failure (RPC Timeout)**:
    - Pipeline retries with **Exponential Backoff**.
2.  **Persistent Failure (Bad Data)**:
    - Block marked as Failed in `failed_blocks` (Postgres).
    - Pipeline continues; background retry mechanism handles remediation.
3.  **Missing Blocks (Gaps)**:
    - `Backfill` process detects gaps in `blocks` table.
    - Queues ranges in `missing_blocks`.
    - Background worker fills gaps.

## 6. Directory Structure

- `cmd/`: Entry points.
- `internal/`: Private library code.
    - `control/`: App assembly.
    - `core/`: Domain types, config, ports.
    - `indexing/`: Logic (Pipeline, Recover, Reorg).
    - `infra/`: Adapters (RPC, Postgres, Redis, Memory).
- `migrations/`: SQL schemas (Goose).
- `docs/`: Architecture and design docs.
