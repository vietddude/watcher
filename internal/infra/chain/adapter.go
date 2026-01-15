package chain

import (
	"context"

	"github.com/vietddude/watcher/internal/core/domain"
)

// Adapter defines the interface that all chain implementations must satisfy
type Adapter interface {
	// GetLatestBlock returns the latest block number on the chain
	GetLatestBlock(ctx context.Context) (uint64, error)

	// GetBlock fetches a block by number
	GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error)

	// GetBlockByHash fetches a block by hash (for verification)
	GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error)

	// GetTransactions fetches all transactions in a block
	GetTransactions(ctx context.Context, block *domain.Block) ([]*domain.Transaction, error)

	// FilterTransactions filters transactions based on tracked addresses
	// Uses chain-specific optimization (bloom filter for EVM, sender filter for Sui, UTXO for Bitcoin)
	FilterTransactions(ctx context.Context, txs []*domain.Transaction, addresses []string) ([]*domain.Transaction, error)

	// VerifyBlockHash verifies if a block hash matches the one on chain
	VerifyBlockHash(ctx context.Context, blockNumber uint64, expectedHash string) (bool, error)

	// EnrichTransaction fetches additional details (receipt) for a transaction
	// Only call this for transactions you care about (matched by filter)
	EnrichTransaction(ctx context.Context, tx *domain.Transaction) error

	// GetFinalityDepth returns the number of confirmations for finality
	GetFinalityDepth() uint64

	// GetChainID returns the chain identifier
	GetChainID() string

	// SupportsBloomFilter indicates if chain supports bloom filter optimization
	SupportsBloomFilter() bool
}
