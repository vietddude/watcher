package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HTTPProvider implements Provider for JSON-RPC over HTTP.
type HTTPProvider struct {
	name       string
	endpoint   string
	httpClient *http.Client

	mu           sync.RWMutex
	health       HealthStatus
	totalLatency time.Duration
	successCount int
	failureCount int
	requestCount int

	Monitor *ProviderMonitor
}

// NewHTTPProvider creates a new HTTP-based RPC provider.
func NewHTTPProvider(name, endpoint string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		name:     name,
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		health: HealthStatus{
			Available:     true,
			LastSuccessAt: time.Now(),
		},
		Monitor: NewProviderMonitor(),
	}
}

// Call makes a single JSON-RPC call.
func (p *HTTPProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	start := time.Now()

	// Pre-call checks
	if status := p.Monitor.CheckProviderStatus(); status == StatusThrottled {
		return nil, fmt.Errorf("provider throttled, retry after: %v", p.Monitor.GetRetryAfter())
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(jsonData))
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	// Rate limit detection
	if resp.StatusCode == 429 {
		retryAfter := resp.Header.Get("Retry-After")
		p.Monitor.RecordThrottle(429, retryAfter)
		p.recordFailure()
		return nil, fmt.Errorf("rate limited (429), retry after: %s", retryAfter)
	}

	// IP blocked detection
	if resp.StatusCode == 403 {
		p.Monitor.RecordThrottle(403, "")
		p.recordFailure()
		return nil, fmt.Errorf("ip blocked (403)")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		p.recordFailure()

		if p.Monitor.DetectThrottlePattern(string(body)) {
			return nil, fmt.Errorf("throttle detected in response: %s", string(body))
		}

		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp struct {
		Result any             `json:"result"`
		Error  *map[string]any `json:"error"`
	}

	if err := json.Unmarshal(body, &rpcResp); err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if rpcResp.Error != nil {
		errMsg := "unknown error"
		if msg, ok := (*rpcResp.Error)["message"].(string); ok {
			errMsg = msg
		}

		if p.Monitor.DetectThrottlePattern(errMsg) {
			p.recordFailure()
			return nil, fmt.Errorf("throttle in rpc error: %s", errMsg)
		}

		p.recordFailure()
		return nil, fmt.Errorf("rpc error: %s", errMsg)
	}

	p.Monitor.RecordRequest(latency)
	p.recordSuccess(latency)

	return rpcResp.Result, nil
}

// BatchCall makes multiple RPC calls in one request.
func (p *HTTPProvider) BatchCall(ctx context.Context, requests []BatchRequest) ([]BatchResponse, error) {
	start := time.Now()

	batchReq := make([]map[string]any, len(requests))
	for i, req := range requests {
		batchReq[i] = map[string]any{
			"jsonrpc": "2.0",
			"method":  req.Method,
			"params":  req.Params,
			"id":      i + 1,
		}
	}

	jsonData, err := json.Marshal(batchReq)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("marshal batch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(jsonData))
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("batch call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("read response: %w", err)
	}

	var batchResp []struct {
		Result any             `json:"result"`
		Error  *map[string]any `json:"error"`
	}

	if err := json.Unmarshal(body, &batchResp); err != nil {
		p.recordFailure()
		return nil, fmt.Errorf("parse batch response: %w", err)
	}

	responses := make([]BatchResponse, len(batchResp))
	for i, r := range batchResp {
		if r.Error != nil {
			errMsg := "unknown error"
			if msg, ok := (*r.Error)["message"].(string); ok {
				errMsg = msg
			}
			responses[i] = BatchResponse{Error: fmt.Errorf("rpc error: %s", errMsg)}
		} else {
			responses[i] = BatchResponse{Result: r.Result}
		}
	}

	p.recordSuccess(time.Since(start))
	return responses, nil
}

// GetName returns the provider's name.
func (p *HTTPProvider) GetName() string {
	return p.name
}

// GetHealth returns the provider's health status.
func (p *HTTPProvider) GetHealth() HealthStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// Close cleans up resources.
func (p *HTTPProvider) Close() error {
	p.httpClient.CloseIdleConnections()
	return nil
}

// HasQuotaRemaining checks if this provider has quota remaining.
func (p *HTTPProvider) HasQuotaRemaining() bool {
	status := p.Monitor.CheckProviderStatus()
	if status == StatusThrottled || status == StatusBlocked {
		return false
	}

	stats := p.Monitor.GetStats()
	return stats.UsagePercentage < 95
}

// GetUsagePercentage returns the current usage percentage.
func (p *HTTPProvider) GetUsagePercentage() float64 {
	return p.Monitor.GetStats().UsagePercentage
}

// IsAvailable checks if the provider is available.
func (p *HTTPProvider) IsAvailable() bool {
	status := p.Monitor.CheckProviderStatus()
	return status == StatusHealthy || status == StatusDegraded
}

func (p *HTTPProvider) recordSuccess(latency time.Duration) {
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

func (p *HTTPProvider) recordFailure() {
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
