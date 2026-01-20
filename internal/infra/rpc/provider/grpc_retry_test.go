package provider

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestGRPCProvider_Execute_Retry verifies that Execute retries on transient errors.
func TestGRPCProvider_Execute_Retry(t *testing.T) {
	// 1. Setup minimal provider (no connection needed for Execute logic)
	p := &GRPCProvider{
		BaseProvider: NewBaseProvider("test-grpc"),
	}

	ctx := context.Background()
	callCount := 0

	// 2. Define operation that fails twice then succeeds
	op := Operation{
		Name: "TestRetry",
		Invoke: func(ctx context.Context) (any, error) {
			callCount++
			if callCount <= 2 {
				return nil, status.Error(codes.Unavailable, "transient failure")
			}
			return "success", nil
		},
	}

	// 3. Execute
	start := time.Now()
	result, err := p.Execute(ctx, op)

	// 4. Verify
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result != "success" {
		t.Errorf("Expected 'success', got %v", result)
	}

	if callCount != 3 {
		t.Errorf("Expected 3 calls (2 retries), got %d", callCount)
	}

	if time.Since(start) < 200*time.Millisecond {
		t.Error("Expected execution to take at least 200ms due to backoff (0ms + 100ms + 200ms?)")
		// My logic was:
		// attempt 0: sleep 0
		// attempt 1: sleep 100ms
		// attempt 2: sleep 200ms
		// So total sleep ~300ms.
	}
}

// TestGRPCProvider_Execute_NonRetryable verifies immediate failure on fatal errors.
func TestGRPCProvider_Execute_NonRetryable(t *testing.T) {
	p := &GRPCProvider{
		BaseProvider: NewBaseProvider("test-grpc"),
	}

	callCount := 0
	op := Operation{
		Name: "TestFatal",
		Invoke: func(ctx context.Context) (any, error) {
			callCount++
			return nil, status.Error(codes.InvalidArgument, "fatal error")
		},
	}

	_, err := p.Execute(context.Background(), op)

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if callCount != 1 {
		t.Errorf("Expected 1 call, got %d", callCount)
	}
}
