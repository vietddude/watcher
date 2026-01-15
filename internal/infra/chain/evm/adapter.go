package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	logger "log/slog"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

type EVMAdapter struct {
	chainID        string
	rpcProvider    rpc.Provider
	bloomFilter    *BloomFilter
	finalityBlocks uint64
	log            logger.Logger
}

func NewEVMAdapter(chainID string, provider rpc.Provider, finalityBlocks uint64) *EVMAdapter {
	return &EVMAdapter{
		chainID:        chainID,
		rpcProvider:    provider,
		bloomFilter:    NewBloomFilter(),
		finalityBlocks: finalityBlocks,
		log:            *logger.Default(),
	}
}

func (a *EVMAdapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := a.rpcProvider.Call(ctx, "eth_blockNumber", []any{})
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block: %w", err)
	}

	blockHex, ok := result.(string)
	if !ok {
		return 0, fmt.Errorf("invalid block number response")
	}

	blockNum := new(big.Int)
	blockNum.SetString(strings.TrimPrefix(blockHex, "0x"), 16)

	return blockNum.Uint64(), nil
}

func (a *EVMAdapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	blockHex := fmt.Sprintf("0x%x", blockNumber)

	result, err := a.rpcProvider.Call(ctx, "eth_getBlockByNumber", []any{blockHex, false})
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d: %w", blockNumber, err)
	}

	if result == nil {
		return nil, fmt.Errorf("block %d not found", blockNumber)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

func (a *EVMAdapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	result, err := a.rpcProvider.Call(ctx, "eth_getBlockByHash", []any{blockHash, false})
	if err != nil {
		return nil, fmt.Errorf("failed to get block by hash %s: %w", blockHash, err)
	}

	if result == nil {
		return nil, fmt.Errorf("block with hash %s not found", blockHash)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

func (a *EVMAdapter) GetTransactions(ctx context.Context, block *domain.Block) ([]*domain.Transaction, error) {
	if block.TxCount == 0 {
		return []*domain.Transaction{}, nil
	}

	blockHex := fmt.Sprintf("0x%x", block.Number)
	result, err := a.rpcProvider.Call(ctx, "eth_getBlockByNumber", []any{blockHex, true})
	if err != nil {
		return nil, fmt.Errorf("failed to get block transactions: %w", err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	txsRaw, ok := blockData["transactions"].([]any)
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

		tx, err := a.parseTransaction(txData, block)
		if err != nil {
			a.log.Warn("failed to parse transaction", "error", err, "index", i)
			continue
		}

		transactions = append(transactions, tx)
	}

	return transactions, nil
}

func (a *EVMAdapter) FilterTransactions(ctx context.Context, txs []*domain.Transaction, addresses []string) ([]*domain.Transaction, error) {
	// Use bloom filter for fast filtering
	if !a.bloomFilter.IsInitialized() {
		a.bloomFilter.Build(addresses)
	}

	filtered := make([]*domain.Transaction, 0)

	for _, tx := range txs {
		// First check bloom filter (fast path)
		if !a.bloomFilter.MayContain(tx.From) && !a.bloomFilter.MayContain(tx.To) {
			continue
		}

		// Then check actual address set (slow path)
		if a.containsAddress(addresses, tx.From) || a.containsAddress(addresses, tx.To) {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
}

func (a *EVMAdapter) VerifyBlockHash(ctx context.Context, blockNumber uint64, expectedHash string) (bool, error) {
	block, err := a.GetBlock(ctx, blockNumber)
	if err != nil {
		return false, err
	}

	return strings.EqualFold(block.Hash, expectedHash), nil
}

func (a *EVMAdapter) GetFinalityDepth() uint64 {
	return a.finalityBlocks
}

func (a *EVMAdapter) GetChainID() string {
	return a.chainID
}

func (a *EVMAdapter) SupportsBloomFilter() bool {
	return true
}

// Helper methods

func (a *EVMAdapter) parseBlock(blockData map[string]interface{}) (*domain.Block, error) {
	number, err := parseHexUint64(blockData["number"].(map[string]any))
	if err != nil {
		return nil, fmt.Errorf("invalid block number: %w", err)
	}

	hash, ok := blockData["hash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block hash")
	}

	parentHash, ok := blockData["parentHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid parent hash")
	}

	timestamp, err := parseHexUint64(blockData["timestamp"].(map[string]any))
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %w", err)
	}

	txCount := 0
	if txs, ok := blockData["transactions"].([]interface{}); ok {
		txCount = len(txs)
	}

	return &domain.Block{
		ChainID:    a.chainID,
		Number:     number,
		Hash:       hash,
		ParentHash: parentHash,
		Timestamp:  time.Unix(int64(timestamp), 0),
		TxCount:    txCount,
		Status:     domain.BlockStatusPending,
		Metadata: map[string]any{
			"gasUsed":  blockData["gasUsed"],
			"gasLimit": blockData["gasLimit"],
			"miner":    blockData["miner"],
		},
	}, nil
}

func (a *EVMAdapter) parseTransaction(txData map[string]interface{}, block *domain.Block) (*domain.Transaction, error) {
	txHash, ok := txData["hash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid tx hash")
	}

	from, ok := txData["from"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid from address")
	}

	to := ""
	if toAddr, ok := txData["to"].(string); ok {
		to = toAddr
	}

	value := "0"
	if val, ok := txData["value"].(string); ok {
		value = val
	}

	txIndex := 0
	if idx, ok := txData["transactionIndex"].(string); ok {
		idxInt, _ := parseHexUint64(map[string]any{"val": idx})
		txIndex = int(idxInt)
	}

	gasPrice := "0"
	if gp, ok := txData["gasPrice"].(string); ok {
		gasPrice = gp
	}

	// Get transaction receipt for gas used and status
	receipt, err := a.getTransactionReceipt(context.Background(), txHash)
	gasUsed := uint64(0)
	status := domain.TxStatusSuccess

	if err == nil && receipt != nil {
		if gu, ok := receipt["gasUsed"].(string); ok {
			gasUsed, _ = parseHexUint64(map[string]any{"val": gu})
		}
		if st, ok := receipt["status"].(string); ok && st == "0x0" {
			status = domain.TxStatusFailed
		}
	}

	rawData, _ := json.Marshal(txData)

	return &domain.Transaction{
		ChainID:     a.chainID,
		BlockNumber: block.Number,
		BlockHash:   block.Hash,
		TxHash:      txHash,
		TxIndex:     txIndex,
		From:        strings.ToLower(from),
		To:          strings.ToLower(to),
		Value:       value,
		GasUsed:     gasUsed,
		GasPrice:    gasPrice,
		Status:      status,
		Timestamp:   block.Timestamp,
		RawData:     rawData,
	}, nil
}

func (a *EVMAdapter) getTransactionReceipt(ctx context.Context, txHash string) (map[string]any, error) {
	result, err := a.rpcProvider.Call(ctx, "eth_getTransactionReceipt", []any{txHash})
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, fmt.Errorf("receipt not found")
	}

	receipt, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid receipt format")
	}

	return receipt, nil
}

func (a *EVMAdapter) containsAddress(addresses []string, target string) bool {
	target = strings.ToLower(target)
	for _, addr := range addresses {
		if strings.EqualFold(addr, target) {
			return true
		}
	}
	return false
}

func parseHexUint64(data map[string]any) (uint64, error) {
	var hexStr string

	// Try different keys
	if val, ok := data["number"].(string); ok {
		hexStr = val
	} else if val, ok := data["timestamp"].(string); ok {
		hexStr = val
	} else if val, ok := data["val"].(string); ok {
		hexStr = val
	} else {
		return 0, fmt.Errorf("no valid hex value found")
	}

	num := new(big.Int)
	num.SetString(strings.TrimPrefix(hexStr, "0x"), 16)
	return num.Uint64(), nil
}
