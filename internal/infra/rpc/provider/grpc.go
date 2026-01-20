package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// GRPCProvider implements Provider for gRPC.
// It does NOT implement RPCProvider because gRPC uses generated clients instead of generic Call().
// Users should get the connection via Conn() and use generated clients.
type GRPCProvider struct {
	*BaseProvider
	endpoint string
	conn     *grpc.ClientConn
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
		BaseProvider: NewBaseProvider(name),
		endpoint:     endpoint,
		conn:         conn,
	}, nil
}

// Conn returns the underlying gRPC connection.
// This allows using generated gRPC clients.
func (p *GRPCProvider) Conn() *grpc.ClientConn {
	return p.conn
}

// Execute performs a gRPC operation with monitoring.
// For gRPC, the operation's Invoke function should wrap the generated client call.
// Example:
//
//	op := provider.Operation{
//	    Name: "GetBlock",
//	    Invoke: func(ctx context.Context) (any, error) {
//	        return grpcClient.GetBlock(ctx, &pb.GetBlockRequest{Number: 123})
//	    },
//	}
//	result, err := provider.Execute(ctx, op)
func (p *GRPCProvider) Execute(ctx context.Context, op Operation) (any, error) {
	if op.Invoke == nil {
		return nil, fmt.Errorf("gRPC operation requires Invoke function")
	}

	const maxRetries = 3
	var lastErr error

	start := time.Now()

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Simple backoff: 100ms, 200ms
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}

		result, err := op.Invoke(ctx)
		if err == nil {
			p.RecordSuccess(time.Since(start))
			return result, nil
		}

		lastErr = err

		// check if retryable
		if !isRetryableGRPCError(err) {
			break
		}
	}

	p.RecordFailure()
	return nil, lastErr
}

func isRetryableGRPCError(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	code := st.Code()
	return code == codes.Unavailable || code == codes.ResourceExhausted
}

// Close cleans up resources.
func (p *GRPCProvider) Close() error {
	return p.conn.Close()
}
