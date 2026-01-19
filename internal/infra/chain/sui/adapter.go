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

// GetBlock returns a block by number (lightweight)
func (a *Adapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	cp, err := a.client.GetCheckpoint(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint %d: %w", blockNumber, err)
	}
	return a.mapCheckpointToBlock(cp)
}

// GetBlockByHash returns a block by digest
func (a *Adapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	// ... existing implementation logic ...
	// Assuming GetCheckpointByDigest is not available yet, we use the raw client.
	// But wait, the raw client change I made previously wasn't in client.go, it was in adapter.go directly accessing ledger.
	// I'll keep the existing logic but ensure it maps correctly.
	// Actually, looking at previous file content, I implemented this using a.client.ledger directly in adapter.
	// I won't change GetBlockByHash unless necessary.
	// Re-reading GetBlockByHash to make sure I don't break it.

	req := &suipb.GetCheckpointRequest{
		CheckpointId: &suipb.GetCheckpointRequest_Digest{
			Digest: blockHash,
		},
		// We probably only need summary here too for verification
		// CheckpointByHash is usually for reorg checking or verification
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
func (a *Adapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	// Fetches the checkpoint details to ensure we have transactions.
	cp, err := a.client.GetCheckpointDetails(ctx, block.Number)
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
func (a *Adapter) FilterTransactions(
	ctx context.Context,
	txs []*domain.Transaction,
	addresses []string,
) ([]*domain.Transaction, error) {
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
func (a *Adapter) VerifyBlockHash(
	ctx context.Context,
	blockNumber uint64,
	expectedHash string,
) (bool, error) {
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

// SupportsBloomFilter returns false for Sui (as it doesn't have a bloom filter in header)
func (a *Adapter) SupportsBloomFilter() bool {
	return false
}

// HasRelevantTransactions checks if the block contains transactions of interest
// This implements the PreFilterAdapter interface for optimization.
func (a *Adapter) HasRelevantTransactions(
	ctx context.Context,
	block *domain.Block,
	addresses []string,
) (bool, error) {
	// 1. Convert addresses to map for O(1) lookup
	addrMap := make(map[string]struct{}, len(addresses))
	for _, addr := range addresses {
		addrMap[addr] = struct{}{}
	}

	// 2. Fetch lightweight checkpoint with ONLY ownership info
	cp, err := a.client.GetCheckpointTransactionOwners(ctx, block.Number)
	if err != nil {
		return false, fmt.Errorf("failed to get checkpoint owners: %w", err)
	}

	// 3. Iterate and check for matches
	for _, tx := range cp.GetTransactions() {
		// Check Sender
		if tx.Transaction != nil {
			if _, ok := addrMap[tx.Transaction.GetSender()]; ok {
				return true, nil
			}
		}

		// Check Recipients (Output Owners)
		if tx.Effects != nil {
			for _, obj := range tx.Effects.GetChangedObjects() {
				owner := obj.GetOutputOwner()
				if owner == nil {
					continue
				}
				// We only care about Address owners (Kind=1)
				// Use the accessor to be safe with proto oneofs
				if owner.GetAddress() != "" {
					if _, ok := addrMap[owner.GetAddress()]; ok {
						return true, nil
					}
				}
			}
		}
	}

	return false, nil
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

func (a *Adapter) mapTransaction(
	execTx *suipb.ExecutedTransaction,
	block *domain.Block,
) *domain.Transaction {
	tx := execTx.GetTransaction()

	status := domain.TxStatusFailed
	if execTx.Effects != nil && execTx.Effects.Status != nil &&
		execTx.Effects.Status.Success != nil &&
		*execTx.Effects.Status.Success {
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
