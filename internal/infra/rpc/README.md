# RPC Package

> ⚠️ **Design Note (IMPORTANT)**
>
> This package is **infra-level code**, not application logic.
> It intentionally handles multiple hard problems at once:
>
> * Multi-provider RPC (Alchemy, Infura, custom nodes)
> * Automatic retry, failover, and rotation
> * Budget / quota tracking
> * Health monitoring / Circuit Breaking
> * Support for **both JSON-RPC and gRPC generated clients**
>
> The size and structure are intentional.
> **Do not refactor or simplify unless absolutely necessary.**


## Architecture

```mermaid
graph TD
    subgraph Application Layer
        A[Application Code]
    end

    subgraph RPC Package
        A --> B[Client]
        B --> C[Coordinator]
        C --> D[Router]
        C --> E[BudgetTracker]
        D --> F[Provider 1]
        D --> G[Provider 2]
        D --> H[Provider N]
        F --> I[ProviderMonitor]
        G --> J[ProviderMonitor]
    end

    subgraph External
        F --> K[Alchemy API]
        G --> L[Infura API]
        H --> M[Other RPC]
    end
```

---

## Quick Start (JSON-RPC)

```go
import (
    "github.com/vietddude/watcher/internal/infra/rpc"
    "github.com/vietddude/watcher/internal/core/domain"
)

// 1. Create providers
alchemy := rpc.NewHTTPProvider("alchemy", os.Getenv("ALCHEMY_URL"), 30*time.Second)
infura := rpc.NewHTTPProvider("infura", os.Getenv("INFURA_URL"), 30*time.Second)

// 2. Setup budget tracker and set quotas
budget := rpc.NewBudgetTracker()
budget.SetProviderQuota("alchemy", 100000)
budget.SetProviderQuota("infura", 100000)

// 3. Setup router with rotation strategy
router := rpc.NewRouterWithStrategy(budget, rpc.RotationProactive)
router.AddProvider("ETHEREUM_MAINNET", alchemy)
router.AddProvider("ETHEREUM_MAINNET", infura)

// 4. Create coordinator
coordinator := rpc.NewCoordinator(router, budget)

// 5. Create client
client := rpc.NewClientWithCoordinator("ETHEREUM_MAINNET", coordinator)

// 6. Make calls (legacy JSON-RPC API)
result, err := client.Call(ctx, "eth_blockNumber", nil)
```

> `Call()` is kept for backward compatibility (JSON-RPC only).
> New code should prefer **`Execute()` with `Operation`**.

---

## Operation Model

The `Operation` abstraction allows the coordinator to execute requests across different protocols (HTTP, gRPC, REST) seamlessly.

### Supported Protocols

1. **JSON-RPC 2.0 (Standard)**
   - Default for Ethereum, Polygon, etc.
   - Use `NewHTTPOperation`.

2. **JSON-RPC 1.0**
   - Required for Bitcoin (strict 1.0 compliance).
   - Use `NewJSONRPC10Operation`.

3. **REST**
   - Required for Sui (Legacy) or custom APIs.
   - Use `NewRESTOperation`.

4. **gRPC**
   - Required for Sui, Solana, etc.
   - Use `NewGRPCOperation`.
   - **Crucial**: The `GRPCHandler` receives the active connection from the provider, enabling load balancing for generated clients.

---

## Usage Examples

### 1. HTTP JSON-RPC 2.0 (Ethereum)

```go
op := rpc.NewHTTPOperation("eth_blockNumber", nil)
result, err := client.Execute(ctx, op)
```

### 2. HTTP JSON-RPC 1.0 (Bitcoin)

```go
// Helper handles version 1.0 quirks (no jsonrpc field, handles positional params)
op := rpc.NewJSONRPC10Operation("getblockcount")
result, err := client.Execute(ctx, op)
```

### 3. HTTP REST

```go
// Example: POST /wallet/getnowblock
op := rpc.NewRESTOperation("wallet/getnowblock", "POST", nil)
result, err := client.Execute(ctx, op)
```

### 4. gRPC (Sui)

```go
// The handler receives the active 'conn' from the load-balanced provider
op := rpc.NewGRPCOperation("GetCheckpoint", func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
    // Create generated client using the injected connection
    cli := suipb.NewLedgerServiceClient(conn)
    return cli.GetCheckpoint(ctx, &req)
})

result, err := client.Execute(ctx, op)
```

---

## Components

| Component           | Responsibility                                         |
| ------------------- | ------------------------------------------------------ |
| **Client**          | Facade for application code                            |
| **Coordinator**     | Orchestrates budget, routing, retry, and failover      |
| **Router**          | Selects best provider based on health and availability |
| **BudgetTracker**   | Manages quota limits and throttling                    |
| **ProviderMonitor** | Tracks latency, errors, and provider health            |
| **CircuitBreaker**  | (Internal) Protects against failing nodes              |

---

## Rotation Strategies

| Strategy             | Description                                   |
| -------------------- | --------------------------------------------- |
| `RotationRoundRobin` | Simple sequential rotation                    |
| `RotationWeighted`   | Based on remaining quota                      |
| `RotationAdaptive`   | Based on performance + quota                  |
| `RotationProactive`  | Actively distributes load to avoid throttling |

---

## Prometheus Metrics

| Metric                                   | Type      | Labels                      | Description          |
| ---------------------------------------- | --------- | --------------------------- | -------------------- |
| `watcher_rpc_calls_total`                | Counter   | chain, provider, method     | Total RPC calls      |
| `watcher_rpc_errors_total`               | Counter   | chain, provider, error_type | Total RPC errors     |
| `watcher_rpc_latency_seconds`            | Histogram | chain, provider, method     | RPC call latency     |
| `watcher_rpc_provider_health_score`      | Gauge     | chain, provider             | Health score (0–100) |
| `watcher_rpc_provider_quota_usage_ratio` | Gauge     | chain, provider             | Quota usage (0–1)    |
| `watcher_rpc_provider_latency_seconds`   | Gauge     | chain, provider             | Average latency      |

---

## Health Score Calculation

`ProviderMonitor.GetHealthScore()` returns a normalized score **0–100**:

* **Status**
  * Blocked: `0` (Returns immediately)
  * Throttled: `-60`
  * Degraded: `-30`
* **Latency**
  * > 3s: `-30`
  * > 2s: `-20`
  * > 1s: `-10`
* **Usage**
  * > 90%: `-30`
  * > 75%: `-15`
  * > 50%: `-5`
* **Errors**
  * HTTP 429: `-3` per error
  * HTTP 403: `-8` per error

---

## Configuration

```go
config := rpc.CoordinatorConfig{
    ProactiveRotation:   true,
    RotationThreshold:   75.0, // rotate at 75% usage
    MinRotationInterval: 2 * time.Minute,
}

coordinator := rpc.NewCoordinatorWithConfig(router, budget, config)
```

---

### Final Note

This package is designed to be **stable and reused** across all indexers and watcher jobs.
The complexity is intentional and centralized here to keep the indexing logic clean.
