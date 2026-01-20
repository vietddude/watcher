package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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

// Close cleans up resources.
func (p *GRPCProvider) Close() error {
	return p.conn.Close()
}
