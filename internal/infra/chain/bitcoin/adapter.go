package bitcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	logger "log/slog"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

type BitcoinAdapter struct {
	chainID        domain.ChainID
	client         rpc.RPCClient
	finalityBlocks uint64
	log            logger.Logger
}

func NewBitcoinAdapter(
	chainID domain.ChainID,
	client rpc.RPCClient,
	finalityBlocks uint64,
) *BitcoinAdapter {
	return &BitcoinAdapter{
		chainID:        chainID,
		client:         client,
		finalityBlocks: finalityBlocks,
		log:            *logger.Default(),
	}
}

func (a *BitcoinAdapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	op := rpc.NewJSONRPC10Operation("getblockcount")
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return 0, fmt.Errorf("failed to get block count: %w", err)
	}

	height, ok := result.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid block count response")
	}

	return uint64(height), nil
}

func (a *BitcoinAdapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	// First get block hash
	opHash := rpc.NewJSONRPC10Operation("getblockhash", blockNumber)
	hashResult, err := a.client.Execute(ctx, opHash)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "out of range") ||
			strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get block hash: %w", err)
	}

	blockHash, ok := hashResult.(string)
	if !ok {
		return nil, fmt.Errorf("invalid block hash response")
	}

	// Then get block details with verbosity 1 (includes tx hashes)
	opBlock := rpc.NewJSONRPC10Operation("getblock", blockHash, 1)
	result, err := a.client.Execute(ctx, opBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

func (a *BitcoinAdapter) GetBlockByHash(
	ctx context.Context,
	blockHash string,
) (*domain.Block, error) {
	op := rpc.NewJSONRPC10Operation("getblock", blockHash, 1)
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("failed to get block by hash: %w", err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

func (a *BitcoinAdapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	// Get block with full transaction details (verbosity 2)
	op := rpc.NewJSONRPC10Operation("getblock", block.Hash, 2)
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("failed to get block transactions: %w", err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	txsRaw, ok := blockData["tx"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid transactions format")
	}

	transactions := make([]*domain.Transaction, 0, len(txsRaw))
	for i, txRaw := range txsRaw {
		txData, ok := txRaw.(map[string]any)
		if !ok {
			a.log.Warn("skipping invalid transaction", "index", i)
			continue
		}

		txs, err := a.parseUTXOTransaction(txData, block, i)
		if err != nil {
			a.log.Warn("failed to parse transaction", "error", err, "index", i)
			continue
		}

		transactions = append(transactions, txs...)
	}

	return transactions, nil
}

func (a *BitcoinAdapter) FilterTransactions(
	ctx context.Context,
	txs []*domain.Transaction,
	addresses []string,
) ([]*domain.Transaction, error) {
	// Outputs-only approach: filter by "To" addresses (recipients)
	// No bloom filter needed - Bitcoin gets full data in 1 API call
	addressMap := make(map[string]bool)
	for _, addr := range addresses {
		addressMap[addr] = true
	}

	filtered := make([]*domain.Transaction, 0)
	for _, tx := range txs {
		if addressMap[tx.To] {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
}

func (a *BitcoinAdapter) VerifyBlockHash(
	ctx context.Context,
	blockNumber uint64,
	expectedHash string,
) (bool, error) {
	op := rpc.NewJSONRPC10Operation("getblockhash", blockNumber)
	hashResult, err := a.client.Execute(ctx, op)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "out of range") ||
			strings.Contains(strings.ToLower(err.Error()), "not found") {
			return false, nil
		}
		return false, err
	}

	actualHash, ok := hashResult.(string)
	if !ok {
		return false, fmt.Errorf("invalid hash response")
	}

	return actualHash == expectedHash, nil
}

func (a *BitcoinAdapter) EnrichTransaction(ctx context.Context, tx *domain.Transaction) error {
	// Bitcoin transactions are already "enriched" during GetTransactions (verbosity 2)
	return nil
}

func (a *BitcoinAdapter) GetFinalityDepth() uint64 {
	return a.finalityBlocks
}

func (a *BitcoinAdapter) GetChainID() domain.ChainID {
	return a.chainID
}

func (a *BitcoinAdapter) SupportsBloomFilter() bool {
	return false // Bitcoin uses UTXO model, not account-based
}

// Helper methods

func (a *BitcoinAdapter) parseBlock(blockData map[string]any) (*domain.Block, error) {
	height, ok := blockData["height"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid block height")
	}

	hash, ok := blockData["hash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block hash")
	}

	previousBlockHash := ""
	if pbh, ok := blockData["previousblockhash"].(string); ok {
		previousBlockHash = pbh
	}

	timestamp, ok := blockData["time"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid timestamp")
	}

	// txCount calculation removed as unused

	return &domain.Block{
		ChainID:    a.chainID,
		Number:     uint64(height),
		Hash:       hash,
		ParentHash: previousBlockHash,
		Timestamp:  uint64(timestamp),
		Status:     domain.BlockStatusPending,
	}, nil
}

func (a *BitcoinAdapter) parseUTXOTransaction(
	txData map[string]any,
	block *domain.Block,
	txIndex int,
) ([]*domain.Transaction, error) {
	txHash, ok := txData["txid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid tx hash")
	}

	// Bitcoin transactions have multiple outputs - create one domain.Transaction per output
	// We focus on outputs only (1 RPC call) - no input address resolution needed
	transactions := make([]*domain.Transaction, 0)

	// Parse outputs (vout) - these are the "to" addresses
	vout, ok := txData["vout"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid vout format")
	}

	for _, voutRaw := range vout {
		voutData, ok := voutRaw.(map[string]any)
		if !ok {
			continue
		}

		// Extract value (in BTC, convert to satoshis)
		value := "0"
		if val, ok := voutData["value"].(float64); ok {
			// Convert BTC to satoshis (1 BTC = 100,000,000 satoshis)
			value = fmt.Sprintf("%.0f", val*100000000)
		}

		// Extract recipient address from scriptPubKey
		toAddr := a.extractOutputAddress(voutData)
		if toAddr == "" {
			// Skip outputs without addresses (OP_RETURN, etc.)
			continue
		}

		rawData, _ := json.Marshal(txData)

		tx := &domain.Transaction{
			ChainID:     a.chainID,
			BlockNumber: block.Number,
			BlockHash:   block.Hash,
			Hash:        txHash,
			Type:        domain.TxTypeNative,
			Index:       txIndex,
			From:        "", // Outputs-only approach: skip input resolution
			To:          toAddr,
			Value:       value,
			Status:      domain.TxStatusSuccess,
			Timestamp:   block.Timestamp,
			RawData:     rawData,
		}

		transactions = append(transactions, tx)
	}

	return transactions, nil
}

// extractOutputAddress extracts the address from a vout scriptPubKey.
// Handles both legacy (addresses array) and modern (address field) formats.
func (a *BitcoinAdapter) extractOutputAddress(voutData map[string]any) string {
	scriptPubKey, ok := voutData["scriptPubKey"].(map[string]any)
	if !ok {
		return ""
	}

	// Method 1: Modern format - single "address" field (SegWit, Taproot)
	if addr, ok := scriptPubKey["address"].(string); ok {
		return addr
	}

	// Method 2: Legacy format - "addresses" array
	if addresses, ok := scriptPubKey["addresses"].([]any); ok && len(addresses) > 0 {
		if addr, ok := addresses[0].(string); ok {
			return addr
		}
	}

	// No address found (OP_RETURN, non-standard scripts)
	return ""
}
