package sui

import (
	"context"
	"fmt"

	v2 "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Client wraps the generated generic gRPC provider to provide typed access
type Client struct {
	ledger v2.LedgerServiceClient
}

// NewClient creates a new typed Sui client using a generic RPC provider
func NewClient(provider rpc.Provider) *Client {
	// Create a Shim that adapts rpc.Provider to grpc.ClientConnInterface
	shim := &ProviderShim{provider: provider}
	return &Client{
		ledger: v2.NewLedgerServiceClient(shim),
	}
}

// NewClientFromService creates a new typed Sui client with a provided LedgerServiceClient (for testing)
func NewClientFromService(service v2.LedgerServiceClient) *Client {
	return &Client{
		ledger: service,
	}
}

// ProviderShim adapts rpc.Provider to grpc.ClientConnInterface
type ProviderShim struct {
	provider rpc.Provider
}

// Invoke delegates to provider.Call
func (s *ProviderShim) Invoke(
	ctx context.Context,
	method string,
	args interface{},
	reply interface{},
	opts ...grpc.CallOption,
) error {
	// We expect the provider to handle the invocation given [args, reply]
	_, err := s.provider.Call(ctx, method, []any{args, reply})
	return err
}

// NewStream is not supported yet
func (s *ProviderShim) NewStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	return nil, fmt.Errorf("streaming not implemented")
}

// GetLatestCheckpoint returns the latest executed checkpoint (block)
func (c *Client) GetLatestCheckpoint(ctx context.Context) (*v2.Checkpoint, error) {
	// GetServiceInfo returns existing service info including latest checkpoint height
	info, err := c.ledger.GetServiceInfo(ctx, &v2.GetServiceInfoRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service info: %w", err)
	}

	// Now fetch the checkpoint using the height
	if info.CheckpointHeight == nil {
		return nil, fmt.Errorf("service info missing checkpoint height")
	}

	return c.GetCheckpoint(ctx, *info.CheckpointHeight)
}

// GetCheckpoint returns a checkpoint by sequence number (lightweight, no transactions)
func (c *Client) GetCheckpoint(ctx context.Context, sequenceNumber uint64) (*v2.Checkpoint, error) {
	// Fetch minimal fields including summary for timestamp
	mask, err := fieldmaskpb.New(&v2.Checkpoint{}, "sequence_number", "digest", "summary")
	if err != nil {
		return nil, fmt.Errorf("failed to create field mask: %w", err)
	}

	req := &v2.GetCheckpointRequest{
		CheckpointId: &v2.GetCheckpointRequest_SequenceNumber{
			SequenceNumber: sequenceNumber,
		},
		ReadMask: mask,
	}

	resp, err := c.ledger.GetCheckpoint(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint %d: %w", sequenceNumber, err)
	}

	if resp.Checkpoint == nil {
		return nil, fmt.Errorf("checkpoint %d not found", sequenceNumber)
	}

	return resp.Checkpoint, nil
}

// GetCheckpointDetails returns a checkpoint with full transaction data
func (c *Client) GetCheckpointDetails(
	ctx context.Context,
	sequenceNumber uint64,
) (*v2.Checkpoint, error) {
	// Include "transactions" in the read mask
	mask, err := fieldmaskpb.New(
		&v2.Checkpoint{},
		"sequence_number",
		"digest",
		"summary",
		"transactions",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create field mask: %w", err)
	}

	req := &v2.GetCheckpointRequest{
		CheckpointId: &v2.GetCheckpointRequest_SequenceNumber{
			SequenceNumber: sequenceNumber,
		},
		ReadMask: mask,
	}

	resp, err := c.ledger.GetCheckpoint(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint details %d: %w", sequenceNumber, err)
	}

	if resp.Checkpoint == nil {
		return nil, fmt.Errorf("checkpoint %d not found", sequenceNumber)
	}

	return resp.Checkpoint, nil
}

// GetCheckpointTransactionOwners returns a checkpoint with only transaction sender and modified object owners
// This is used for pre-filtering blocks based on improved "bloom filter" logic.
func (c *Client) GetCheckpointTransactionOwners(
	ctx context.Context,
	sequenceNumber uint64,
) (*v2.Checkpoint, error) {
	// Construct mask to fetch only ownership information
	mask, err := fieldmaskpb.New(&v2.Checkpoint{},
		"transactions.transaction.sender",
		"transactions.effects.changed_objects.output_owner.address",
		"transactions.effects.changed_objects.output_owner.kind",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create field mask: %w", err)
	}

	req := &v2.GetCheckpointRequest{
		CheckpointId: &v2.GetCheckpointRequest_SequenceNumber{
			SequenceNumber: sequenceNumber,
		},
		ReadMask: mask,
	}

	resp, err := c.ledger.GetCheckpoint(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint owners %d: %w", sequenceNumber, err)
	}

	if resp.Checkpoint == nil {
		return nil, fmt.Errorf("checkpoint %d not found", sequenceNumber)
	}

	return resp.Checkpoint, nil
}

// GetTransaction returns a transaction by digest
func (c *Client) GetTransaction(
	ctx context.Context,
	digest string,
) (*v2.ExecutedTransaction, error) {
	resp, err := c.ledger.GetTransaction(ctx, &v2.GetTransactionRequest{
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
