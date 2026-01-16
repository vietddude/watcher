package sui

import (
	"context"
	"fmt"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/chain"
	suipb "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
)

// Adapter implements chain.Adapter for Sui using gRPC
type Adapter struct {
	client  *Client
	chainID string
}

// Ensure Adapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)

// NewAdapter creates a new Sui adapter
func NewAdapter(chainID string, client *Client) *Adapter {
	return &Adapter{
		client:  client,
		chainID: chainID,
	}
}

// GetLatestBlock returns the latest checkpoint sequence number
func (a *Adapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	cp, err := a.client.GetLatestCheckpoint(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest checkpoint: %w", err)
	}
	return cp.GetSequenceNumber(), nil
}

// GetBlock returns a block by number
func (a *Adapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	cp, err := a.client.GetCheckpoint(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint %d: %w", blockNumber, err)
	}
	return a.mapCheckpointToBlock(cp)
}

// GetBlockByHash returns a block by digest
func (a *Adapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	// Sui requires checking by digest if not using sequence number
	// Our client implementation needs update to support get by digest or we can use generic GetCheckpoint with digest
	// Checking client.go, GetCheckpoint takes sequenceNumber. I should update it or use the low level client.

	// Updating logic to use the low level client directly for now to avoid changing client.go signature if possible,
	// but better to add a method to client.go. For now I will assume I can add it or doing it here.

	// Actually, looking at client.go I implemented GetCheckpoint taking uint64.
	// I should probably add GetCheckpointByDigest to Client.
	// But let's verify if I can just implement it here using a.client.ledger.

	req := &suipb.GetCheckpointRequest{
		CheckpointId: &suipb.GetCheckpointRequest_Digest{
			Digest: blockHash,
		},
	}

	resp, err := a.client.ledger.GetCheckpoint(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint by digest %s: %w", blockHash, err)
	}

	if resp.Checkpoint == nil {
		return nil, fmt.Errorf("checkpoint %s not found", blockHash)
	}

	return a.mapCheckpointToBlock(resp.Checkpoint)
}

// GetTransactions returns all executed transactions in a block
func (a *Adapter) GetTransactions(ctx context.Context, block *domain.Block) ([]*domain.Transaction, error) {
	// Fetches the checkpoint again to ensure we have transactions.
	// Ideally we optimization this if the block already has them (but domain.Block is generic).
	cp, err := a.client.GetCheckpoint(ctx, block.Number)
	if err != nil {
		return nil, err
	}

	var txs []*domain.Transaction
	for _, execTx := range cp.GetTransactions() {
		tx := a.mapTransaction(execTx, block)
		txs = append(txs, tx)
	}

	return txs, nil
}

// FilterTransactions filters transactions based on sender (Sui optimization)
func (a *Adapter) FilterTransactions(ctx context.Context, txs []*domain.Transaction, addresses []string) ([]*domain.Transaction, error) {
	// In-memory filter for now.
	// If we wanted to use Sui specific filtering at query time, we would need a different API.
	// But the interface assumes we already have the transactions.

	addressSet := make(map[string]bool)
	for _, addr := range addresses {
		addressSet[addr] = true
	}

	var filtered []*domain.Transaction
	for _, tx := range txs {
		if addressSet[tx.From] || addressSet[tx.To] {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
}

// VerifyBlockHash checks if the block hash matches
func (a *Adapter) VerifyBlockHash(ctx context.Context, blockNumber uint64, expectedHash string) (bool, error) {
	cp, err := a.client.GetCheckpoint(ctx, blockNumber)
	if err != nil {
		return false, err
	}
	return cp.GetDigest() == expectedHash, nil
}

// EnrichTransaction is a no-op for Sui as executed transaction already contains effects/events usually
func (a *Adapter) EnrichTransaction(ctx context.Context, tx *domain.Transaction) error {
	return nil
}

// GetFinalityDepth returns 0 because Sui has instant finality
func (a *Adapter) GetFinalityDepth() uint64 {
	return 0
}

// GetChainID returns the chain identifier
func (a *Adapter) GetChainID() string {
	return a.chainID
}

// SupportsBloomFilter returns false for Sui
func (a *Adapter) SupportsBloomFilter() bool {
	return false
}

// Helper mappings

func (a *Adapter) mapCheckpointToBlock(cp *suipb.Checkpoint) (*domain.Block, error) {
	timestamp := time.Unix(0, 0)
	if cp.Summary != nil && cp.Summary.Timestamp != nil {
		timestamp = cp.Summary.Timestamp.AsTime()
	}

	return &domain.Block{
		Number:     cp.GetSequenceNumber(),
		Hash:       cp.GetDigest(),
		ParentHash: cp.Summary.GetPreviousDigest(),
		Timestamp:  timestamp,
		TxCount:    len(cp.GetTransactions()),
	}, nil
}

func (a *Adapter) mapTransaction(execTx *suipb.ExecutedTransaction, block *domain.Block) *domain.Transaction {
	tx := execTx.GetTransaction()

	status := domain.TxStatusFailed
	if execTx.Effects != nil && execTx.Effects.Status != nil && execTx.Effects.Status.Success != nil && *execTx.Effects.Status.Success {
		status = domain.TxStatusSuccess
	}

	// Map sender
	sender := ""
	if tx != nil {
		sender = tx.GetSender()
	}

	// For "To" address, it's complicated in Sui (programmable txs).
	// We might use the first recipient or leave empty if not applicable.
	// For now, we leave To empty or try to infer from effects?
	// Real implementation might iterate inputs/objects.

	return &domain.Transaction{
		TxHash:      execTx.GetDigest(),
		BlockNumber: block.Number,
		BlockHash:   block.Hash,
		From:        sender,
		To:          "",  // Complex to determine "To" in Move
		Value:       "0", // Value is also complex
		Status:      status,
		Timestamp:   block.Timestamp,
	}
}
