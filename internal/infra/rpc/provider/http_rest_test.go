package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProvider_ExecuteREST(t *testing.T) {
	// Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Path
		if r.URL.Path != "/wallet/getnowblock" {
			t.Errorf("expected path /wallet/getnowblock, got %s", r.URL.Path)
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		// Verify Method
		if r.Method != "POST" {
			t.Errorf("expected method POST, got %s", r.Method)
			http.Error(w, "invalid method", http.StatusBadRequest)
			return
		}

		// Check body if present
		var body map[string]any
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode body: %v", err)
				return
			}
		}

		// Respond
		response := map[string]any{
			"blockID": "0000000003abc123",
			"block_header": map[string]any{
				"raw_data": map[string]any{
					"number": float64(62345555),
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create Provider
	p := NewHTTPProvider("tron-mock", server.URL, 5*time.Second)

	// Create REST Operation
	op := Operation{
		Name:       "wallet/getnowblock",
		IsREST:     true,
		RESTMethod: "POST",
		Params:     nil, // No params for getnowblock
	}

	// Execute
	result, err := p.Execute(context.Background(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify Result
	data, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}

	if data["blockID"] != "0000000003abc123" {
		t.Errorf("expected blockID 0000000003abc123, got %v", data["blockID"])
	}
}

func TestHTTPProvider_ExecuteREST_WithParams(t *testing.T) {
	// Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wallet/getblockbynum" {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
			return
		}

		if num, ok := body["num"].(float64); !ok || num != 12345 {
			t.Errorf("expected num=12345, got %v", body["num"])
		}

		response := map[string]any{"status": "ok"}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	p := NewHTTPProvider("tron-mock", server.URL, 5*time.Second)

	op := Operation{
		Name:       "wallet/getblockbynum",
		IsREST:     true,
		RESTMethod: "POST",
		Params:     map[string]any{"num": 12345},
	}

	_, err := p.Execute(context.Background(), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
