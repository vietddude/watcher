# RPC Package

> ⚠️ **Design Note (IMPORTANT)**
>
> This package is **infra-level code**, not application logic.
> It intentionally handles multiple hard problems at once:
>
> * Multi-provider RPC (Alchemy, Infura, custom nodes)
> * Automatic retry, failover, and rotation
> * Budget / quota tracking
> * Health monitoring
> * Support for **both JSON-RPC and gRPC generated clients**
>
> The size and structure are intentional.
> **Do not refactor or simplify unless absolutely necessary.**

Production-ready RPC infrastructure for multi-chain blockchain indexers.

---

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
import "github.com/vietddude/watcher/internal/infra/rpc"

// 1. Create providers
alchemy := rpc.NewHTTPProvider("alchemy", os.Getenv("ALCHEMY_URL"), 30*time.Second)
infura := rpc.NewHTTPProvider("infura", os.Getenv("INFURA_URL"), 30*time.Second)

// 2. Setup budget tracker
budget := rpc.NewBudgetTracker(100000, map[string]float64{
    "ethereum": 1.0,
})

// 3. Setup router with rotation strategy
router := rpc.NewRouterWithStrategy(budget, rpc.RotationProactive)
router.AddProvider("ethereum", alchemy)
router.AddProvider("ethereum", infura)

// 4. Create coordinator
coordinator := rpc.NewCoordinator(router, budget)

// 5. Create client
client := rpc.NewClientWithCoordinator("ethereum", coordinator)

// 6. Make calls (legacy JSON-RPC API)
result, err := client.Call(ctx, "eth_blockNumber", nil)
```

> `Call()` is kept for backward compatibility (JSON-RPC only).
> New code should prefer **`Execute()` with `Operation`**.

---

## Operation Model (Important)

The `Operation` abstraction is the **core execution unit** of this package.

### Rules

* **HTTP JSON-RPC providers**

  * Use `Operation.Name` and `Operation.Params`
  * `Operation.Invoke` is ignored

* **gRPC providers**

  * `Operation.Invoke` **MUST** be provided
  * The generated gRPC client call is wrapped inside `Invoke`

This design exists because **gRPC uses generated, strongly-typed clients**
and cannot be called via `Call(method, params)`.

---

## Operation-based Usage (gRPC & HTTP)

The package exposes a unified:

```go
Execute(ctx context.Context, op Operation)
```

This method supports **both HTTP JSON-RPC and gRPC** with the same
retry, failover, budget, and monitoring pipeline.

---

### gRPC (Generated Clients)

```go
// 1. Create gRPC provider
grpcProv, _ := rpc.NewGRPCProvider(ctx, "alchemy-grpc", "alchemy.com:443")

// 2. Wrap generated client call into an Operation
op := rpc.NewOperation("GetBlock", func(ctx context.Context) (any, error) {
    client := pb.NewBlockchainClient(grpcProv.Conn())
    return client.GetBlock(ctx, &pb.BlockRequest{Number: 123})
})

// 3. Execute with failover, retry, and monitoring
result, err := client.Execute(ctx, op)
```

---

### HTTP (JSON-RPC) via Operation

```go
// 1. Create HTTP operation
op := rpc.NewHTTPOperation("eth_blockNumber", nil)

// 2. Execute
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

  * Blocked: `0`
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

This package is designed to be **stable and reused** across:

* blockchain indexers
* watcher jobs
* gRPC-based internal services
* future MPC / wallet infrastructure

If you feel the code looks “large”, that is expected.
**The complexity is intentional and centralized here.**
