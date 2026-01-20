package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPProvider implements Provider for JSON-RPC over HTTP.
type HTTPProvider struct {
	*BaseProvider
	endpoint   string
	httpClient *http.Client
}

// NewHTTPProvider creates a new HTTP-based RPC provider.
func NewHTTPProvider(name, endpoint string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		BaseProvider: NewBaseProvider(name),
		endpoint:     endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
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
		p.RecordFailure()
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(jsonData))
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	// Rate limit detection
	if resp.StatusCode == 429 {
		retryAfter := resp.Header.Get("Retry-After")
		p.Monitor.RecordThrottle(429, retryAfter)
		p.RecordFailure()
		return nil, fmt.Errorf("rate limited (429), retry after: %s", retryAfter)
	}

	// IP blocked detection
	if resp.StatusCode == 403 {
		p.Monitor.RecordThrottle(403, "")
		p.RecordFailure()
		return nil, fmt.Errorf("ip blocked (403)")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		p.RecordFailure()

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
		p.RecordFailure()
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if rpcResp.Error != nil {
		errMsg := "unknown error"
		if msg, ok := (*rpcResp.Error)["message"].(string); ok {
			errMsg = msg
		}

		if p.Monitor.DetectThrottlePattern(errMsg) {
			p.RecordFailure()
			return nil, fmt.Errorf("throttle in rpc error: %s", errMsg)
		}

		p.RecordFailure()
		return nil, fmt.Errorf("rpc error: %s", errMsg)
	}

	p.RecordSuccess(latency)

	return rpcResp.Result, nil
}

// BatchCall makes multiple RPC calls in one request.
func (p *HTTPProvider) BatchCall(
	ctx context.Context,
	requests []BatchRequest,
) ([]BatchResponse, error) {
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
		p.RecordFailure()
		return nil, fmt.Errorf("marshal batch: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(jsonData))
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("batch call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("read response: %w", err)
	}

	var batchResp []struct {
		Result any             `json:"result"`
		Error  *map[string]any `json:"error"`
	}

	if err := json.Unmarshal(body, &batchResp); err != nil {
		p.RecordFailure()
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

	p.RecordSuccess(time.Since(start))
	return responses, nil
}

// Execute performs an operation using HTTP JSON-RPC.
// For HTTP provider, it uses op.Name and op.Params to make a Call.
// If op.Invoke is set and op.Params is nil, it will use Invoke instead.
func (p *HTTPProvider) Execute(ctx context.Context, op Operation) (any, error) {
	// If Invoke is set and Params is nil, use Invoke (for custom operations)
	if op.Invoke != nil && op.Params == nil {
		start := time.Now()
		result, err := op.Invoke(ctx)
		if err != nil {
			p.RecordFailure()
			return nil, err
		}
		p.RecordSuccess(time.Since(start))
		return result, nil
	}

	// Standard HTTP JSON-RPC call using op.Name and op.Params
	return p.Call(ctx, op.Name, op.Params)
}

// Close cleans up resources.
func (p *HTTPProvider) Close() error {
	p.httpClient.CloseIdleConnections()
	return nil
}
