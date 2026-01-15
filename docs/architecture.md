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
    end
    
    Adapter --> RPC[Blockchain Node]
```

## 2. Component Design

### Control Plane (`internal/control`)
- **Watcher**: Composition root. Wires all dependencies.
- **Health**: Monitored via `health.Monitor`. Exposes HTTP `/health`.

### Indexing Pipeline (`internal/indexing/indexer/pipeline.go`)
The heart of the system. Runs in a loop:
1.  **Fetch**: Get next block via `ChainAdapter`.
2.  **Reorg Check**: Verify parent hash consistency.
3.  **Process**:
    - Extract Transactions.
    - Filter interesting TXs (`Filter`).
    - Emit events (`Emitter`).
4.  **Commit**: Update Cursor (`CursorRepo`).

### Data Models (`internal/core/domain`)
- **Block**: Normalized block header.
- **Transaction**: Normalized tx data.
- **Event**: Derived event (e.g., "Log Found").
- **Cursor**: State checkpoint.

## 3. Data Flow: Indexing

```mermaid
sequenceDiagram
    participant P as Pipeline
    participant A as Adapter (RPC)
    participant R as ReorgDetector
    participant F as Filter
    participant E as Emitter
    participant C as Cursor

    loop Every Interval
        P->>C: GetCurrentBlock()
        P->>A: GetBlock(N+1)
        
        alt Reorg Detected
            P->>R: DetectReorg(N+1)
            R->>P: Reorg Confirmed (Depth X)
            P->>C: Rollback(N-X)
        else Normal Flow
            P->>F: Filter(Block.Txs)
            F->>P: Relevant Txs
            P->>E: Emit(Txs)
            P->>C: Advance(N+1)
        end
    end
```

## 4. Failure Recovery

1.  **Transient Failure (RPC Timeout)**:
    - Pipeline retries with **Exponential Backoff**.
2.  **Persistent Failure (Bad Data)**:
    - Block marked as Failed in `failed_blocks` queue.
    - Pipeline skips and continues (if policy allows) or halts.
3.  **Missing Blocks (Gaps)**:
    - `Backfill` process detects gaps in `blocks` table.
    - Queues ranges in `missing_blocks`.
    - Background worker fills gaps.

## 5. Directory Structure

- `cmd/`: Entry points.
- `internal/`: Private library code.
    - `control/`: App assembly.
    - `core/`: Domain types, config, ports.
    - `indexing/`: Logic (Pipeline, Recover, Reorg).
    - `infra/`: Adapters (RPC, Postgres, Memory).
- `migrations/`: SQL schemas.
