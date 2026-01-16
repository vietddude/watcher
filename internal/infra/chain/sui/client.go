package sui

import (
	"context"
	"fmt"

	suipb "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client wraps the generated Sui gRPC client
type Client struct {
	conn   *grpc.ClientConn
	ledger suipb.LedgerServiceClient
}

// NewClient creates a new Sui gRPC client
func NewClient(ctx context.Context, endpoint string) (*Client, error) {
	// TODO: Add support for secure credentials if needed
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sui grpc endpoint: %w", err)
	}

	return &Client{
		conn:   conn,
		ledger: suipb.NewLedgerServiceClient(conn),
	}, nil
}

// NewClientFromService creates a new Sui client with a provided LedgerServiceClient (for testing)
func NewClientFromService(service suipb.LedgerServiceClient) *Client {
	return &Client{
		ledger: service,
	}
}

// Close closes the underlying gRPC connection
func (c *Client) Close() error {
	return c.conn.Close()
}

// GetLatestCheckpoint returns the latest executed checkpoint (block)
func (c *Client) GetLatestCheckpoint(ctx context.Context) (*suipb.Checkpoint, error) {
	// GetServiceInfo returns existing service info including latest checkpoint height
	info, err := c.ledger.GetServiceInfo(ctx, &suipb.GetServiceInfoRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service info: %w", err)
	}

	// Now fetch the checkpoint using the height
	if info.CheckpointHeight == nil {
		return nil, fmt.Errorf("service info missing checkpoint height")
	}

	return c.GetCheckpoint(ctx, *info.CheckpointHeight)
}

// GetCheckpoint returns a checkpoint by sequence number
func (c *Client) GetCheckpoint(ctx context.Context, sequenceNumber uint64) (*suipb.Checkpoint, error) {
	resp, err := c.ledger.GetCheckpoint(ctx, &suipb.GetCheckpointRequest{
		CheckpointId: &suipb.GetCheckpointRequest_SequenceNumber{
			SequenceNumber: sequenceNumber,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint %d: %w", sequenceNumber, err)
	}

	if resp.Checkpoint == nil {
		return nil, fmt.Errorf("checkpoint %d not found", sequenceNumber)
	}

	return resp.Checkpoint, nil
}

// GetTransaction returns a transaction by digest
func (c *Client) GetTransaction(ctx context.Context, digest string) (*suipb.ExecutedTransaction, error) {
	resp, err := c.ledger.GetTransaction(ctx, &suipb.GetTransactionRequest{
		Digest: &digest,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction %s: %w", digest, err)
	}

	if resp.Transaction == nil {
		return nil, fmt.Errorf("transaction %s not found", digest)
	}

	return resp.Transaction, nil
}
