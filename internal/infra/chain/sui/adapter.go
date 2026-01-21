package sui

import (
	"context"
	"fmt"
	"time"

	"sync"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/chain"
	suipb "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Adapter implements chain.Adapter for Sui using gRPC
type Adapter struct {
	client  rpc.RPCClient
	chainID domain.ChainID

	// Single-slot cache for the current block being processed
	mu               sync.RWMutex
	cachedCheckpoint *suipb.Checkpoint

	// Parallel Prefetching
	prefetchMu      sync.Mutex
	prefetchBuffer  map[uint64]*suipb.Checkpoint
	prefetchLoading map[uint64]chan struct{} // channel to wait for ongoing fetches
}

const prefetchWindow = 20

// Ensure Adapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)

// NewAdapter creates a new Sui adapter
func NewAdapter(chainID domain.ChainID, client rpc.RPCClient) *Adapter {
	return &Adapter{
		client:          client,
		chainID:         chainID,
		prefetchBuffer:  make(map[uint64]*suipb.Checkpoint),
		prefetchLoading: make(map[uint64]chan struct{}),
	}
}

// GetLatestBlock returns the latest checkpoint sequence number
func (a *Adapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	op := rpc.NewGRPCOperation(
		"GetServiceInfo",
		func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
			return suipb.NewLedgerServiceClient(conn).
				GetServiceInfo(ctx, &suipb.GetServiceInfoRequest{})
		},
	)

	res, err := a.client.Execute(ctx, op)
	if err != nil {
		return 0, fmt.Errorf("failed to get service info: %w", err)
	}

	info, ok := res.(*suipb.GetServiceInfoResponse)
	if !ok {
		return 0, fmt.Errorf("invalid response type: %T", res)
	}

	if info.CheckpointHeight == nil {
		return 0, fmt.Errorf("service info missing checkpoint height")
	}
	return *info.CheckpointHeight, nil
}

// GetBlock returns a block by number (lightweight)
func (a *Adapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	// 1. Check current cache (in case of re-requests)
	a.mu.RLock()
	if a.cachedCheckpoint != nil && a.cachedCheckpoint.GetSequenceNumber() == blockNumber {
		defer a.mu.RUnlock()
		return a.mapCheckpointToBlock(a.cachedCheckpoint)
	}
	a.mu.RUnlock()

	// 2. Check Prefetch Buffer
	var cp *suipb.Checkpoint
	a.prefetchMu.Lock()
	if cached, ok := a.prefetchBuffer[blockNumber]; ok {
		cp = cached
		delete(a.prefetchBuffer, blockNumber)
		a.prefetchMu.Unlock()
	} else if waitCh, loading := a.prefetchLoading[blockNumber]; loading {
		// Ongoing fetch - wait for it
		a.prefetchMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCh:
			// Recurse to pick up from buffer
			return a.GetBlock(ctx, blockNumber)
		}
	} else {
		a.prefetchMu.Unlock()
		// Cache miss - Fetch Manually
		var err error
		cp, err = a.fetchCheckpoint(ctx, blockNumber)
		if err != nil {
			return nil, err
		}
	}

	if cp == nil {
		return nil, nil
	}

	// 3. Update current cache
	a.mu.Lock()
	a.cachedCheckpoint = cp
	a.mu.Unlock()

	// 4. Trigger next prefetch window
	go a.prefetch(blockNumber + 1)

	return a.mapCheckpointToBlock(cp)
}

func (a *Adapter) fetchCheckpoint(ctx context.Context, blockNumber uint64) (*suipb.Checkpoint, error) {
	op := rpc.NewGRPCOperation(
		"GetCheckpoint",
		func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
			mask, err := fieldmaskpb.New(
				&suipb.Checkpoint{},
				"sequence_number",
				"digest",
				"summary",
				"transactions",
			)
			if err != nil {
				return nil, err
			}
			req := &suipb.GetCheckpointRequest{
				CheckpointId: &suipb.GetCheckpointRequest_SequenceNumber{
					SequenceNumber: blockNumber,
				},
				ReadMask: mask,
			}
			return suipb.NewLedgerServiceClient(conn).GetCheckpoint(ctx, req)
		},
	)

	res, err := a.client.Execute(ctx, op)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get checkpoint %d: %w", blockNumber, err)
	}

	resp, ok := res.(*suipb.GetCheckpointResponse)
	if !ok {
		return nil, fmt.Errorf("invalid response type: %T", res)
	}
	return resp.Checkpoint, nil
}

func (a *Adapter) prefetch(start uint64) {
	for i := uint64(0); i < prefetchWindow; i++ {
		num := start + i

		a.prefetchMu.Lock()
		if _, exists := a.prefetchBuffer[num]; exists {
			a.prefetchMu.Unlock()
			continue
		}
		if _, loading := a.prefetchLoading[num]; loading {
			a.prefetchMu.Unlock()
			continue
		}

		// Start prefetch
		waitCh := make(chan struct{})
		a.prefetchLoading[num] = waitCh
		a.prefetchMu.Unlock()

		go func(n uint64, ch chan struct{}) {
			defer close(ch)
			defer func() {
				a.prefetchMu.Lock()
				delete(a.prefetchLoading, n)
				a.prefetchMu.Unlock()
			}()

			// Background context for prefetching
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cp, err := a.fetchCheckpoint(ctx, n)
			if err == nil && cp != nil {
				a.prefetchMu.Lock()
				a.prefetchBuffer[n] = cp
				// Cleanup old buffer if it gets too large
				if len(a.prefetchBuffer) > prefetchWindow*2 {
					for k := range a.prefetchBuffer {
						if k < n-prefetchWindow {
							delete(a.prefetchBuffer, k)
						}
					}
				}
				a.prefetchMu.Unlock()
			}
		}(num, waitCh)
	}
}

// GetBlockByHash returns a block by digest
func (a *Adapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	op := rpc.NewGRPCOperation(
		"GetCheckpoint",
		func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
			req := &suipb.GetCheckpointRequest{
				CheckpointId: &suipb.GetCheckpointRequest_Digest{
					Digest: blockHash,
				},
				// Default mask or similar to GetBlock
			}
			return suipb.NewLedgerServiceClient(conn).GetCheckpoint(ctx, req)
		},
	)

	res, err := a.client.Execute(ctx, op)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get checkpoint by digest %s: %w", blockHash, err)
	}

	resp, ok := res.(*suipb.GetCheckpointResponse)
	if !ok {
		return nil, fmt.Errorf("invalid response type: %T", res)
	}

	if resp.Checkpoint == nil {
		return nil, nil
	}

	return a.mapCheckpointToBlock(resp.Checkpoint)
}

// GetTransactions returns all executed transactions in a block
func (a *Adapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	// Fetches the checkpoint details to ensure we have transactions.
	// Check Cache First
	a.mu.RLock()
	cached := a.cachedCheckpoint
	a.mu.RUnlock()

	var cp *suipb.Checkpoint
	if cached != nil && cached.GetSequenceNumber() == block.Number {
		// Cache Hit!
		cp = cached
	} else {
		// Cache Miss - Fetch
		op := rpc.NewGRPCOperation(
			"GetCheckpointDetails",
			func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
				mask, err := fieldmaskpb.New(
					&suipb.Checkpoint{},
					"sequence_number",
					"digest",
					"summary",
					"transactions",
				)
				if err != nil {
					return nil, err
				}

				req := &suipb.GetCheckpointRequest{
					CheckpointId: &suipb.GetCheckpointRequest_SequenceNumber{
						SequenceNumber: block.Number,
					},
					ReadMask: mask,
				}
				return suipb.NewLedgerServiceClient(conn).GetCheckpoint(ctx, req)
			},
		)

		res, err := a.client.Execute(ctx, op)
		if err != nil {
			return nil, err
		}

		resp, ok := res.(*suipb.GetCheckpointResponse)
		if !ok {
			return nil, fmt.Errorf("invalid response type: %T", res)
		}
		cp = resp.Checkpoint
		if cp == nil {
			return nil, fmt.Errorf("checkpoint %d not found", block.Number)
		}
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
	// Filter transactions in-memory.

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
	op := rpc.NewGRPCOperation(
		"GetCheckpoint",
		func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
			mask, err := fieldmaskpb.New(&suipb.Checkpoint{}, "sequence_number", "digest")
			if err != nil {
				return nil, err
			}
			req := &suipb.GetCheckpointRequest{
				CheckpointId: &suipb.GetCheckpointRequest_SequenceNumber{
					SequenceNumber: blockNumber,
				},
				ReadMask: mask,
			}
			return suipb.NewLedgerServiceClient(conn).GetCheckpoint(ctx, req)
		},
	)

	res, err := a.client.Execute(ctx, op)
	if err != nil {
		return false, err
	}

	resp, ok := res.(*suipb.GetCheckpointResponse)
	if !ok {
		return false, fmt.Errorf("invalid response type: %T", res)
	}
	cp := resp.Checkpoint
	if cp == nil {
		return false, fmt.Errorf("checkpoint not found")
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
func (a *Adapter) GetChainID() domain.ChainID {
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
	// 2. Check Cache
	a.mu.RLock()
	cached := a.cachedCheckpoint
	a.mu.RUnlock()

	var cp *suipb.Checkpoint
	if cached != nil && cached.GetSequenceNumber() == block.Number {
		// Cache Hit!
		cp = cached
	} else {
		// Cache Miss - Fetch lightweight checkpoint with ONLY ownership info
		op := rpc.NewGRPCOperation(
			"GetCheckpointOwners",
			func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
				mask, err := fieldmaskpb.New(&suipb.Checkpoint{},
					"transactions",
				)
				if err != nil {
					return nil, err
				}

				req := &suipb.GetCheckpointRequest{
					CheckpointId: &suipb.GetCheckpointRequest_SequenceNumber{
						SequenceNumber: block.Number,
					},
					ReadMask: mask,
				}
				return suipb.NewLedgerServiceClient(conn).GetCheckpoint(ctx, req)
			},
		)

		res, err := a.client.Execute(ctx, op)
		if err != nil {
			return false, fmt.Errorf("failed to get checkpoint owners: %w", err)
		}

		resp, ok := res.(*suipb.GetCheckpointResponse)
		if !ok {
			return false, fmt.Errorf("invalid response type: %T", res)
		}
		cp = resp.Checkpoint
		if cp == nil {
			return false, fmt.Errorf("checkpoint not found")
		}
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
	var timestamp uint64
	if cp.Summary != nil && cp.Summary.Timestamp != nil {
		timestamp = uint64(cp.Summary.Timestamp.Seconds)
	}

	return &domain.Block{
		ChainID:    a.chainID,
		Number:     cp.GetSequenceNumber(),
		Hash:       cp.GetDigest(),
		ParentHash: cp.Summary.GetPreviousDigest(),
		Timestamp:  timestamp,
		Status:     domain.BlockStatusProcessed, // Checkpoints are final
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

	// "To" address is complex in Sui due to programmable transactions; leaving empty for now.

	return &domain.Transaction{
		Hash:        execTx.GetDigest(),
		BlockNumber: block.Number,
		BlockHash:   block.Hash,
		From:        sender,
		To:          "",  // Complex to determine "To" in Move
		Value:       "0", // Value is also complex
		Status:      status,
		Timestamp:   block.Timestamp,
	}
}
