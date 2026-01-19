package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// GRPCProvider implements Provider for gRPC.
type GRPCProvider struct {
	name     string
	endpoint string
	conn     *grpc.ClientConn

	mu           sync.RWMutex
	health       HealthStatus
	totalLatency time.Duration
	successCount int
	failureCount int
	requestCount int

	Monitor *ProviderMonitor
}

// NewGRPCProvider creates a new gRPC provider.
func NewGRPCProvider(ctx context.Context, name, endpoint string) (*GRPCProvider, error) {
	// Parse endpoint to determine if TLS is needed
	target := endpoint
	var opts []grpc.DialOption

	// Check scheme
	if strings.HasPrefix(endpoint, "https://") || strings.HasSuffix(endpoint, ":443") {
		// Use TLS
		creds := credentials.NewTLS(&tls.Config{})
		opts = append(opts, grpc.WithTransportCredentials(creds))
		// Strip scheme for Dial
		target = strings.TrimPrefix(target, "https://")
	} else {
		// No TLS
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		target = strings.TrimPrefix(target, "http://")
	}

	// We use the context for the initial connection attempt
	// blocking dial to ensure we fail fast if unavailable?
	// But usually we want non-blocking. Let's stick to standard behavior unless specified.
	// But HTTPProvider doesn't dial.
	// Here we Dial.

	// Add some default dial options
	opts = append(opts, grpc.WithBlock()) // Wait for connection

	// Use a timeout for the dial
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, target, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial grpc endpoint %s: %w", target, err)
	}

	return &GRPCProvider{
		name:     name,
		endpoint: endpoint,
		conn:     conn,
		health: HealthStatus{
			Available:     true,
			LastSuccessAt: time.Now(),
		},
		Monitor: NewProviderMonitor(),
	}, nil
}

// Call makes a gRPC call.
func (p *GRPCProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	if len(params) != 2 {
		return nil, fmt.Errorf("grpc call expects 2 params: [request, response_ptr]")
	}

	req := params[0]
	resp := params[1]

	start := time.Now()

	// Pre-call checks
	if status := p.Monitor.CheckProviderStatus(); status == StatusThrottled {
		return nil, fmt.Errorf("provider throttled, retry after: %v", p.Monitor.GetRetryAfter())
	}

	err := p.conn.Invoke(ctx, method, req, resp)
	latency := time.Since(start)

	if err != nil {
		p.recordFailure()

		// Map gRPC status codes to monitor
		st, ok := status.FromError(err)
		if ok {
			// Check for rate limiting or similar
			// Unfortunately gRPC codes for rate limiting vary, usually ResourceExhausted (8) or Unavailable (14)
			// But 'Method not found' is Unimplemented (12)

			// Simple throttle detection
			errMsg := st.Message()
			if p.Monitor.DetectThrottlePattern(errMsg) {
				p.Monitor.RecordThrottle(int(st.Code()), "")
				return nil, fmt.Errorf("throttle detected: %w", err)
			}
		}

		return nil, err
	}

	p.Monitor.RecordRequest(latency)
	p.recordSuccess(latency)

	return resp, nil
}

// BatchCall - gRPC doesn't support generic batching like JSON-RPC.
// We implement it by looping.
func (p *GRPCProvider) BatchCall(
	ctx context.Context,
	requests []BatchRequest,
) ([]BatchResponse, error) {
	// Not truly supported, but we can iterate.
	// However, BatchRequest params are []any. How do we map that?
	// This provider expects params to be [req, resp].
	// BatchRequest has Params []any.
	// If the user uses BatchCall, they must structure requests appropriately.

	responses := make([]BatchResponse, len(requests))
	// Parallelize?
	// Let's do serial for safety first.

	for i, req := range requests {
		// Expect params to be [req, resp]
		res, err := p.Call(ctx, req.Method, req.Params)
		if err != nil {
			responses[i] = BatchResponse{Error: err}
		} else {
			responses[i] = BatchResponse{Result: res}
		}
	}

	return responses, nil
}

func (p *GRPCProvider) GetName() string {
	return p.name
}

func (p *GRPCProvider) GetHealth() HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

func (p *GRPCProvider) Close() error {
	return p.conn.Close()
}

// Copy-paste helpers from HTTPProvider (or refactor to common base later?)
// For now, duplicate to verify it works.

func (p *GRPCProvider) HasQuotaRemaining() bool {
	status := p.Monitor.CheckProviderStatus()
	if status == StatusThrottled || status == StatusBlocked {
		return false
	}
	return p.Monitor.GetStats().UsagePercentage < 95
}

func (p *GRPCProvider) GetUsagePercentage() float64 {
	return p.Monitor.GetStats().UsagePercentage
}

func (p *GRPCProvider) IsAvailable() bool {
	status := p.Monitor.CheckProviderStatus()
	return status == StatusHealthy || status == StatusDegraded
}

func (p *GRPCProvider) recordSuccess(latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.successCount++
	p.requestCount++
	p.totalLatency += latency
	p.health.LastSuccessAt = time.Now()
	p.health.Available = true

	if p.requestCount > 0 {
		p.health.ErrorRate = float64(p.failureCount) / float64(p.requestCount)
	}
	if p.successCount > 0 {
		p.health.Latency = p.totalLatency / time.Duration(p.successCount)
	}
}

func (p *GRPCProvider) recordFailure() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.failureCount++
	p.requestCount++
	p.health.LastFailureAt = time.Now()

	if p.requestCount > 0 {
		p.health.ErrorRate = float64(p.failureCount) / float64(p.requestCount)
	}

	if p.health.ErrorRate > 0.5 {
		p.health.Available = false
	}
}
