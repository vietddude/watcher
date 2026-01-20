// Package rpc provides a resilient RPC client for blockchain networks.
package rpc

import (
	"context"

	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

// NewOperation creates an Operation with a custom Invoke function.
// Use this for gRPC generated clients or custom transports.
func NewOperation(name string, invoke func(ctx context.Context) (any, error)) Operation {
	return provider.Operation{
		Name:   name,
		Cost:   1,
		Invoke: invoke,
	}
}

// NewOperationWithCost creates an Operation with custom cost.
func NewOperationWithCost(name string, cost int, invoke func(ctx context.Context) (any, error)) Operation {
	return provider.Operation{
		Name:   name,
		Cost:   cost,
		Invoke: invoke,
	}
}

// NewHTTPOperation creates an Operation for HTTP JSON-RPC calls.
// HTTPProvider will use Name and Params directly with its Call method.
func NewHTTPOperation(method string, params []any) Operation {
	return provider.Operation{
		Name:   method,
		Cost:   1,
		Params: params,
	}
}

// NewHTTPOperationWithCost creates an HTTP Operation with custom cost.
func NewHTTPOperationWithCost(method string, params []any, cost int) Operation {
	return provider.Operation{
		Name:   method,
		Cost:   cost,
		Params: params,
	}
}
