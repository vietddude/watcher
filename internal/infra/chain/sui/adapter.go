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

// Executor defines the interface required for executing RPC operations
type Executor interface {
	Execute(ctx context.Context, op rpc.Operation) (any, error)
}

// Adapter implements chain.Adapter for Sui using gRPC
type Adapter struct {
	client  Executor
	chainID domain.ChainID

	// Single-slot cache for the current block being processed
	mu               sync.RWMutex
	cachedCheckpoint *suipb.Checkpoint

	// Streaming support
	streamMu           sync.RWMutex
	streamHeight       uint64
	streamBuffer       map[uint64]*suipb.Checkpoint
	streamDisconnected bool
}

const (
	streamBufSize = 100
)

// Ensure Adapter implements chain.Adapter
var _ chain.Adapter = (*Adapter)(nil)

// NewAdapter creates a new Sui adapter
func NewAdapter(chainID domain.ChainID, client Executor) *Adapter {
	a := &Adapter{
		client:       client,
		chainID:      chainID,
		streamBuffer: make(map[uint64]*suipb.Checkpoint),
	}

	// Start streaming in background (fire and forget for now, as lifecycle is app-bound)
	go a.startStreaming(context.Background())

	return a
}

func (a *Adapter) startStreaming(ctx context.Context) {
	for {
		err := a.subscribe(ctx)
		if err != nil {
			a.streamMu.Lock()
			a.streamDisconnected = true
			a.streamMu.Unlock()
			// Backoff before reconnecting
			time.Sleep(5 * time.Second)
		}
	}
}

func (a *Adapter) subscribe(ctx context.Context) error {
	op := rpc.NewGRPCOperation(
		"SubscribeCheckpoints",
		func(ctx context.Context, conn grpc.ClientConnInterface) (any, error) {
			mask, err := fieldmaskpb.New(
				&suipb.SubscribeCheckpointsResponse{},
				"checkpoint.sequence_number",
				"checkpoint.digest",
				"checkpoint.summary",
				"checkpoint.transactions",
			)
			if err != nil {
				return nil, err
			}
			req := &suipb.SubscribeCheckpointsRequest{
				ReadMask: mask,
			}
			return suipb.NewSubscriptionServiceClient(conn).SubscribeCheckpoints(ctx, req)
		},
	)

	res, err := a.client.Execute(ctx, op)
	if err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	stream, ok := res.(grpc.ServerStreamingClient[suipb.SubscribeCheckpointsResponse])
	if !ok {
		return fmt.Errorf("invalid response type: %T", res)
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			return err
		}

		// Public nodes often return empty checkpoint details but provide the sequence number in the cursor.
		// We prioritize using the cursor to track the latest block height (streamHeight) for low latency.
		cursor := resp.GetCursor()

		checkpoint := resp.Checkpoint
		seq := uint64(0)
		if checkpoint != nil {
			seq = checkpoint.GetSequenceNumber()
		}

		// If explicit sequence number is missing (0), fallback to cursor
		if seq == 0 {
			seq = cursor
		}

		if seq > 0 {
			a.streamMu.Lock()
			a.streamDisconnected = false
			if seq > a.streamHeight {
				a.streamHeight = seq
			}

			// Only cache if we have actual checkpoint data (digest is a good indicator)
			if checkpoint != nil && checkpoint.GetDigest() != "" {
				a.streamBuffer[seq] = checkpoint
				// Prune old buffer
				if len(a.streamBuffer) > streamBufSize {
					for k := range a.streamBuffer {
						if k < seq-streamBufSize {
							delete(a.streamBuffer, k)
						}
					}
				}
			}
			a.streamMu.Unlock()
		}
	}
}

// GetLatestBlock returns the latest checkpoint sequence number
func (a *Adapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	// 1. Unconditionally try to use stream height if healthy
	a.streamMu.RLock()
	height := a.streamHeight
	disconnected := a.streamDisconnected
	a.streamMu.RUnlock()

	if height > 0 && !disconnected {
		return height, nil
	}

	// 2. Fallback to RPC
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
	// 1. Check Stream Buffer first
	a.streamMu.RLock()
	if cp, ok := a.streamBuffer[blockNumber]; ok {
		// Verify if it has enough data (e.g. Digest)
		// If Digest is empty (as seen in test), treat as cache miss to force fetch
		if cp.GetDigest() != "" {
			a.streamMu.RUnlock()
			return a.mapCheckpointToBlock(cp)
		}
	}
	a.streamMu.RUnlock()

	// 2. Check current cache (in case of re-requests)
	a.mu.RLock()
	if a.cachedCheckpoint != nil && a.cachedCheckpoint.GetSequenceNumber() == blockNumber {
		defer a.mu.RUnlock()
		return a.mapCheckpointToBlock(a.cachedCheckpoint)
	}
	a.mu.RUnlock()

	// 3. Cache miss - Fetch Manually
	// Optimization: Try to fetch immediately. If it fails (node lagging behind stream),
	// wait briefly and retry once. This avoids penalizing the happy path.

	// Attempt 1: Immediate
	cp, err := a.fetchCheckpoint(ctx, blockNumber)

	// If error is NotFound or similar, and we believe it exists (<= streamHeight), try waiting
	if err != nil {
		a.streamMu.RLock()
		streamTip := a.streamHeight
		a.streamMu.RUnlock()

		// Only retry if the stream says this block SHOULD exist
		if streamTip > 0 && blockNumber <= streamTip {
			// Check if error is related to not found (simple check for now, can be improved)
			// For generic robustnees, we'll try once more after a delay for ANY error
			// in this critical "tip" zone.
			time.Sleep(500 * time.Millisecond)
			var retryErr error
			cp, retryErr = a.fetchCheckpoint(ctx, blockNumber)
			if retryErr == nil {
				err = nil // Recovered
			}
			// If retryErr != nil, we return the original or retry error
		}
	}

	if err != nil {
		return nil, err
	}

	if cp == nil {
		return nil, nil
	}

	// 4. Update current cache
	a.mu.Lock()
	a.cachedCheckpoint = cp
	a.mu.Unlock()

	return a.mapCheckpointToBlock(cp)
}

func (a *Adapter) fetchCheckpoint(
	ctx context.Context,
	blockNumber uint64,
) (*suipb.Checkpoint, error) {
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
