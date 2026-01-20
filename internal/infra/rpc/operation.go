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
func NewOperationWithCost(
	name string,
	cost int,
	invoke func(ctx context.Context) (any, error),
) Operation {
	return provider.Operation{
		Name:   name,
		Cost:   cost,
		Invoke: invoke,
	}
}

// NewHTTPOperation creates an Operation for HTTP JSON-RPC calls.
// HTTPProvider will use Name and Params directly with its Call method.
func NewHTTPOperation(method string, params any) Operation {
	return provider.Operation{
		Name:   method,
		Cost:   1,
		Params: params,
	}
}

// NewHTTPOperationWithCost creates an HTTP Operation with custom cost.
func NewHTTPOperationWithCost(method string, params any, cost int) Operation {
	return provider.Operation{
		Name:   method,
		Cost:   cost,
		Params: params,
	}
}

// NewRESTOperation creates an Operation for REST API calls.
func NewRESTOperation(path string, method string, body any) Operation {
	return provider.Operation{
		Name:       path,
		Cost:       1,
		Params:     body,
		IsREST:     true,
		RESTMethod: method,
	}
}

// NewJSONRPC10Operation creates an Operation for JSON-RPC 1.0 calls.
func NewJSONRPC10Operation(method string, params ...any) Operation {
	var p any = params
	if len(params) == 0 {
		p = nil
	}
	return provider.Operation{
		Name:           method,
		Cost:           1,
		Params:         p, // 1.0 uses positional params
		JSONRPCVersion: "1.0",
	}
}
