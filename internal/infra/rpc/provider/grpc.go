package provider

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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
	// 1. Parse URL to separate scheme, host, and path
	u, err := url.Parse(endpoint)
	if err != nil {
		u = &url.URL{Host: endpoint}
	}

	target := u.Host
	var opts []grpc.DialOption

	// 2. Handle TLS
	if u.Scheme == "https" || strings.HasSuffix(target, ":443") {
		creds := credentials.NewTLS(&tls.Config{})
		opts = append(opts, grpc.WithTransportCredentials(creds))
		if !strings.Contains(target, ":") {
			target += ":443"
		}
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// 3. Handle Authentication Token from Path (common in Quicknode)
	pathPrefix := strings.TrimSuffix(u.Path, "/")
	if pathPrefix != "" && pathPrefix != "/" {
		slog.Info("Detected authentication token in gRPC path, enabling prefixing", "provider", name, "prefix", pathPrefix)

		// Add interceptors to prefix the method path and add headers for auth
		opts = append(opts, grpc.WithChainUnaryInterceptor(
			func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				token := strings.TrimPrefix(pathPrefix, "/")
				ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", token)
				// Prefix the method with the token path if the proxy requires it
				return invoker(ctx, pathPrefix+method, req, reply, cc, opts...)
			},
		))

		opts = append(opts, grpc.WithChainStreamInterceptor(
			func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				token := strings.TrimPrefix(pathPrefix, "/")
				ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", token)
				return streamer(ctx, desc, cc, pathPrefix+method, opts...)
			},
		))
	}

	conn, err := grpc.NewClient(target, opts...)
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
func (p *GRPCProvider) Conn() *grpc.ClientConn {
	return p.conn
}

// Execute performs a gRPC operation with monitoring.
func (p *GRPCProvider) Execute(ctx context.Context, op Operation) (any, error) {
	if op.Invoke == nil && op.GRPCHandler == nil {
		return nil, fmt.Errorf("gRPC operation requires Invoke or GRPCHandler function")
	}

	const maxRetries = 3
	var lastErr error

	start := time.Now()

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}

		var result any
		var err error
		if op.GRPCHandler != nil {
			result, err = op.GRPCHandler(ctx, p.conn)
		} else {
			result, err = op.Invoke(ctx)
		}

		if err == nil {
			p.RecordSuccess(time.Since(start))
			return result, nil
		}

		lastErr = err

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
	// Do not retry 401/403
	if code == codes.Unauthenticated || code == codes.PermissionDenied {
		return false
	}
	return code == codes.Unavailable || code == codes.ResourceExhausted
}

// Close cleans up resources.
func (p *GRPCProvider) Close() error {
	return p.conn.Close()
}
