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
	return p.callRPC(ctx, method, params, "2.0")
}

// callRPC performs the actual JSON-RPC request with version control.
func (p *HTTPProvider) callRPC(
	ctx context.Context,
	method string,
	params any,
	version string,
) (any, error) {
	start := time.Now()

	// Pre-call checks
	if status := p.Monitor.CheckProviderStatus(); status == StatusThrottled {
		return nil, fmt.Errorf("provider throttled, retry after: %v", p.Monitor.GetRetryAfter())
	}

	reqBody := map[string]any{
		"method": method,
		"params": params,
		"id":     1,
	}

	// For JSON-RPC 1.0, omit "jsonrpc" field or use "1.0" (but typically omitted/ignored).
	// Most 1.0 impls like Bitcoin work better without it or don't care, but strict 2.0 requires it.
	// If version is "1.0", we omit the jsonrpc field to be safe for strict 1.0 parsers.
	// If params is nil, we MUST use null for 1.0.
	if version == "1.0" {
		if params == nil {
			reqBody["params"] = nil
		}
	} else {
		reqBody["jsonrpc"] = version
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

	// Handle REST operations
	if op.IsREST {
		return p.executeREST(ctx, op)
	}

	// Standard HTTP JSON-RPC call using op.Name and op.Params
	// Handle JSON-RPC versions
	// callRPC takes 'any' params, so we can pass op.Params directly.

	// Determine version
	version := op.JSONRPCVersion
	if version == "" {
		version = "2.0"
	}

	return p.callRPC(ctx, op.Name, op.Params, version)
}

// executeREST performs a REST API call.
func (p *HTTPProvider) executeREST(ctx context.Context, op Operation) (any, error) {
	start := time.Now()

	// Pre-call checks
	if status := p.Monitor.CheckProviderStatus(); status == StatusThrottled {
		return nil, fmt.Errorf("provider throttled, retry after: %v", p.Monitor.GetRetryAfter())
	}

	// Construct URL: endpoint + / + method name (path)
	// Remove trailing slash from endpoint and leading slash from op.Name
	url := fmt.Sprintf("%s/%s", p.endpoint, op.Name)

	var bodyReader io.Reader
	if op.Params != nil {
		jsonData, err := json.Marshal(op.Params)
		if err != nil {
			p.RecordFailure()
			return nil, fmt.Errorf("marshal rest params: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, op.RESTMethod, url, bodyReader)
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("create rest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.RecordFailure()
		return nil, fmt.Errorf("rest call: %w", err)
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
		return nil, fmt.Errorf("read rest response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.RecordFailure()
		if p.Monitor.DetectThrottlePattern(string(body)) {
			return nil, fmt.Errorf("throttle detected in response: %s", string(body))
		}
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	var result any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &result); err != nil {
			p.RecordFailure()
			return nil, fmt.Errorf("parse rest response: %w", err)
		}
	}

	p.RecordSuccess(latency)
	return result, nil
}

// Close cleans up resources.
func (p *HTTPProvider) Close() error {
	p.httpClient.CloseIdleConnections()
	return nil
}
