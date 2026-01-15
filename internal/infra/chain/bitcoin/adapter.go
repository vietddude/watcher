package bitcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	logger "log/slog"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

type BitcoinAdapter struct {
	chainID        string
	rpcProvider    rpc.Provider
	finalityBlocks uint64
	log            logger.Logger
}

func NewBitcoinAdapter(chainID string, provider rpc.Provider, finalityBlocks uint64) *BitcoinAdapter {
	return &BitcoinAdapter{
		chainID:        chainID,
		rpcProvider:    provider,
		finalityBlocks: finalityBlocks,
		log:            *logger.Default(),
	}
}

func (a *BitcoinAdapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	result, err := a.rpcProvider.Call(ctx, "getblockcount", []interface{}{})
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
	hashResult, err := a.rpcProvider.Call(ctx, "getblockhash", []interface{}{blockNumber})
	if err != nil {
		return nil, fmt.Errorf("failed to get block hash: %w", err)
	}

	blockHash, ok := hashResult.(string)
	if !ok {
		return nil, fmt.Errorf("invalid block hash response")
	}

	// Then get block details with verbosity 1 (includes tx hashes)
	result, err := a.rpcProvider.Call(ctx, "getblock", []interface{}{blockHash, 1})
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	blockData, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

func (a *BitcoinAdapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	result, err := a.rpcProvider.Call(ctx, "getblock", []interface{}{blockHash, 1})
	if err != nil {
		return nil, fmt.Errorf("failed to get block by hash: %w", err)
	}

	blockData, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

func (a *BitcoinAdapter) GetTransactions(ctx context.Context, block *domain.Block) ([]*domain.Transaction, error) {
	if block.TxCount == 0 {
		return []*domain.Transaction{}, nil
	}

	// Get block with full transaction details (verbosity 2)
	result, err := a.rpcProvider.Call(ctx, "getblock", []interface{}{block.Hash, 2})
	if err != nil {
		return nil, fmt.Errorf("failed to get block transactions: %w", err)
	}

	blockData, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	txsRaw, ok := blockData["tx"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid transactions format")
	}

	transactions := make([]*domain.Transaction, 0, len(txsRaw))
	for i, txRaw := range txsRaw {
		txData, ok := txRaw.(map[string]interface{})
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

func (a *BitcoinAdapter) FilterTransactions(ctx context.Context, txs []*domain.Transaction, addresses []string) ([]*domain.Transaction, error) {
	// Bitcoin uses UTXO model - filter by addresses in inputs/outputs
	addressMap := make(map[string]bool)
	for _, addr := range addresses {
		addressMap[addr] = true
	}

	filtered := make([]*domain.Transaction, 0)
	for _, tx := range txs {
		if addressMap[tx.From] || addressMap[tx.To] {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
}

func (a *BitcoinAdapter) VerifyBlockHash(ctx context.Context, blockNumber uint64, expectedHash string) (bool, error) {
	hashResult, err := a.rpcProvider.Call(ctx, "getblockhash", []interface{}{blockNumber})
	if err != nil {
		return false, err
	}

	actualHash, ok := hashResult.(string)
	if !ok {
		return false, fmt.Errorf("invalid hash response")
	}

	return actualHash == expectedHash, nil
}

func (a *BitcoinAdapter) GetFinalityDepth() uint64 {
	return a.finalityBlocks
}

func (a *BitcoinAdapter) GetChainID() string {
	return a.chainID
}

func (a *BitcoinAdapter) SupportsBloomFilter() bool {
	return false // Bitcoin uses UTXO model, not account-based
}

// Helper methods

func (a *BitcoinAdapter) parseBlock(blockData map[string]interface{}) (*domain.Block, error) {
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

	txCount := 0
	if txs, ok := blockData["tx"].([]interface{}); ok {
		txCount = len(txs)
	} else if nTx, ok := blockData["nTx"].(float64); ok {
		txCount = int(nTx)
	}

	return &domain.Block{
		ChainID:    a.chainID,
		Number:     uint64(height),
		Hash:       hash,
		ParentHash: previousBlockHash,
		Timestamp:  time.Unix(int64(timestamp), 0),
		TxCount:    txCount,
		Status:     domain.BlockStatusPending,
		Metadata: map[string]interface{}{
			"difficulty": blockData["difficulty"],
			"size":       blockData["size"],
			"weight":     blockData["weight"],
		},
	}, nil
}

func (a *BitcoinAdapter) parseUTXOTransaction(txData map[string]interface{}, block *domain.Block, txIndex int) ([]*domain.Transaction, error) {
	txHash, ok := txData["txid"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid tx hash")
	}

	// Bitcoin transactions can have multiple inputs and outputs
	// We create one domain.Transaction per input->output pair
	transactions := make([]*domain.Transaction, 0)

	// Parse inputs (vin)
	inputs := []string{}
	if vin, ok := txData["vin"].([]interface{}); ok {
		for _, vinRaw := range vin {
			vinData, ok := vinRaw.(map[string]interface{})
			if !ok {
				continue
			}

			// Extract address from scriptPubKey
			if addresses, ok := vinData["prevout"].(map[string]interface{})["scriptPubKey"].(map[string]interface{})["addresses"].([]interface{}); ok {
				if len(addresses) > 0 {
					if addr, ok := addresses[0].(string); ok {
						inputs = append(inputs, addr)
					}
				}
			}
		}
	}

	// Parse outputs (vout)
	if vout, ok := txData["vout"].([]interface{}); ok {
		for _, voutRaw := range vout {
			voutData, ok := voutRaw.(map[string]interface{})
			if !ok {
				continue
			}

			value := "0"
			if val, ok := voutData["value"].(float64); ok {
				// Convert BTC to satoshis
				value = fmt.Sprintf("%.0f", val*100000000)
			}

			// Extract recipient address
			toAddr := ""
			if scriptPubKey, ok := voutData["scriptPubKey"].(map[string]interface{}); ok {
				if addresses, ok := scriptPubKey["addresses"].([]interface{}); ok && len(addresses) > 0 {
					if addr, ok := addresses[0].(string); ok {
						toAddr = addr
					}
				}
			}

			// Create transaction for each input-output pair
			fromAddr := ""
			if len(inputs) > 0 {
				fromAddr = inputs[0] // Simplified: use first input
			}

			rawData, _ := json.Marshal(txData)

			tx := &domain.Transaction{
				ChainID:     a.chainID,
				BlockNumber: block.Number,
				BlockHash:   block.Hash,
				TxHash:      txHash,
				TxIndex:     txIndex,
				From:        fromAddr,
				To:          toAddr,
				Value:       value,
				Status:      domain.TxStatusSuccess,
				Timestamp:   block.Timestamp,
				RawData:     rawData,
			}

			transactions = append(transactions, tx)
		}
	}

	return transactions, nil
}
