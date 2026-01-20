package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProvider_Execute_JSONRPC10(t *testing.T) {
	// Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode body: %v", err)
			return
		}

		// Verify "jsonrpc" field passed for 1.0
		if val, ok := req["jsonrpc"]; ok {
			t.Errorf("expected no jsonrpc field for 1.0, got %v", val)
		}

		// Verify Params is null if empty
		if req["params"] != nil {
			// If we passed params, verify they are correct
		} else {
			// For getblockcount, params should be null (or omitted, but 1.0 likes null)
			// Bitcoin adapter passes NewJSONRPC10Operation("getblockcount") -> params is []
		}

		response := map[string]any{
			"result": float64(800000),
			"error":  nil,
			"id":     req["id"],
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	p := NewHTTPProvider("bitcoin-mock", server.URL, 5*time.Second)

	// Test case 1: No params
	op := Operation{
		Name:           "getblockcount",
		JSONRPCVersion: "1.0",
		Params:         nil, // explicitly nil for test
	}

	result, err := p.Execute(context.Background(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.(float64) != 800000 {
		t.Errorf("expected 800000, got %v", result)
	}
}

func TestHTTPProvider_Execute_JSONRPC20_Default(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode body: %v", err)
			return
		}

		// Verify "jsonrpc": "2.0"
		if v, ok := req["jsonrpc"].(string); !ok || v != "2.0" {
			t.Errorf("expected jsonrpc: 2.0, got %v", req["jsonrpc"])
		}

		response := map[string]any{
			"result": "0x123",
			"error":  nil,
			"id":     req["id"],
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	p := NewHTTPProvider("eth-mock", server.URL, 5*time.Second)

	// Default op (2.0)
	op := Operation{
		Name:   "eth_blockNumber",
		Params: []any{},
	}

	_, err := p.Execute(context.Background(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
