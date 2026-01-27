package tron

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	logger "log/slog"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/filter"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

// TronAdapter implements chain.Adapter for the Tron network.
// Tron uses HTTP API (not JSON-RPC), so we adapt the calls accordingly.
// API docs: https://developers.tron.network/reference
type TronAdapter struct {
	chainID        domain.ChainID
	client         rpc.RPCClient
	bloomFilter    *filter.BloomFilter
	finalityBlocks uint64
	log            logger.Logger
}

// NewTronAdapter creates a new Tron adapter.
// Note: Tron uses HTTP endpoints like /wallet/getnowblock, /wallet/getblockbynum
// The rpcProvider should be configured to call Tron's HTTP API.
func NewTronAdapter(
	chainID domain.ChainID,
	client rpc.RPCClient,
	finalityBlocks uint64,
) *TronAdapter {
	return &TronAdapter{
		chainID:        chainID,
		client:         client,
		bloomFilter:    filter.NewBloomFilter(),
		finalityBlocks: finalityBlocks,
		log:            *logger.Default(),
	}
}

// GetLatestBlock returns the latest block number on Tron.
func (a *TronAdapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	// Tron API: POST /wallet/getnowblock
	op := rpc.NewRESTOperation("wallet/getnowblock", "POST", nil)
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest block: %w", err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("invalid block response format")
	}

	// Extract block header
	blockHeader, ok := blockData["block_header"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("missing block_header")
	}

	rawData, ok := blockHeader["raw_data"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("missing raw_data in block_header")
	}

	number, ok := rawData["number"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid block number")
	}

	return uint64(number), nil
}

// GetBlock returns block data for a specific block number.
func (a *TronAdapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	// Tron API: POST /wallet/getblockbynum with {"num": blockNumber}
	op := rpc.NewRESTOperation("wallet/getblockbynum", "POST", map[string]any{"num": blockNumber})
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("failed to get block %d: %w", blockNumber, err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

// GetBlockByHash returns block data for a specific block hash.
func (a *TronAdapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	// Tron API: POST /wallet/getblockbyid with {"value": blockHash}
	op := rpc.NewRESTOperation("wallet/getblockbyid", "POST", map[string]any{"value": blockHash})
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("failed to get block by hash %s: %w", blockHash, err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	return a.parseBlock(blockData)
}

// GetTransactions returns all transactions in a block.
func (a *TronAdapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	// Get full block data with transactions
	op := rpc.NewRESTOperation("wallet/getblockbynum", "POST", map[string]any{"num": block.Number})
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("failed to get block transactions: %w", err)
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block data format")
	}

	txsRaw, ok := blockData["transactions"].([]any)
	if !ok {
		// No transactions in block
		return []*domain.Transaction{}, nil
	}

	transactions := make([]*domain.Transaction, 0, len(txsRaw))
	for i, txRaw := range txsRaw {
		txData, ok := txRaw.(map[string]any)
		if !ok {
			a.log.Warn("skipping invalid transaction", "index", i)
			continue
		}

		tx, err := a.parseTransaction(txData, block, i)
		if err != nil {
			a.log.Warn("failed to parse transaction", "error", err, "index", i)
			continue
		}

		if tx != nil {
			transactions = append(transactions, tx)
		}
	}

	return transactions, nil
}

// FilterTransactions filters transactions by watched addresses.
// Uses bloom filter for fast rejection, then exact match verification.
func (a *TronAdapter) FilterTransactions(
	ctx context.Context,
	txs []*domain.Transaction,
	addresses []string,
) ([]*domain.Transaction, error) {
	// Build/rebuild bloom filter if needed
	if !a.bloomFilter.IsInitialized() {
		a.bloomFilter.Build(addresses)
	}

	// Build exact match set
	addressMap := make(map[string]bool)
	for _, addr := range addresses {
		addressMap[addr] = true
	}

	filtered := make([]*domain.Transaction, 0)
	for _, tx := range txs {
		// Fast path: bloom filter check
		mayMatchFrom := a.bloomFilter.MayContain(tx.From)
		mayMatchTo := a.bloomFilter.MayContain(tx.To)
		if !mayMatchFrom && !mayMatchTo {
			continue // Definitely not a match
		}

		// Slow path: exact check (only for bloom filter hits)
		if addressMap[tx.From] || addressMap[tx.To] {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
}

// VerifyBlockHash verifies that a block number has the expected hash.
func (a *TronAdapter) VerifyBlockHash(
	ctx context.Context,
	blockNumber uint64,
	expectedHash string,
) (bool, error) {
	block, err := a.GetBlock(ctx, blockNumber)
	if err != nil {
		return false, err
	}

	return block.Hash == expectedHash, nil
}

// GetFinalityDepth returns the number of blocks for finality.
func (a *TronAdapter) GetFinalityDepth() uint64 {
	return a.finalityBlocks
}

// GetChainID returns the chain identifier.
func (a *TronAdapter) GetChainID() domain.ChainID {
	return a.chainID
}

// SupportsBloomFilter returns false - Tron doesn't support bloom filters.
func (a *TronAdapter) SupportsBloomFilter() bool {
	return false
}

// EnrichTransaction fetches additional transaction info (energy, bandwidth, logs).
// Call this for transactions that match your filter to get full details.
func (a *TronAdapter) EnrichTransaction(ctx context.Context, tx *domain.Transaction) error {
	// API: POST /wallet/gettransactioninfobyid
	op := rpc.NewRESTOperation(
		"wallet/gettransactioninfobyid",
		"POST",
		map[string]any{"value": tx.Hash},
	)
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return fmt.Errorf("failed to get transaction info: %w", err)
	}

	info, ok := result.(map[string]any)
	if !ok || len(info) == 0 {
		return nil // No info available
	}

	// Extract fee (in SUN)
	if fee, ok := info["fee"].(float64); ok {
		tx.GasPrice = strconv.FormatInt(int64(fee), 10)
	}

	// Extract energy used from receipt
	if receipt, ok := info["receipt"].(map[string]any); ok {
		var totalEnergy uint64
		if energyUsage, ok := receipt["energy_usage"].(float64); ok {
			totalEnergy += uint64(energyUsage)
		}
		if energyFee, ok := receipt["energy_fee"].(float64); ok {
			// Energy fee is in SUN, convert to energy units (1 energy = ~420 SUN)
			totalEnergy += uint64(energyFee) / 420
		}
		tx.GasUsed = totalEnergy
	}

	return nil
}

// GetTransactionLogs fetches event logs for a transaction.
// Useful for TRC20 Transfer events.
func (a *TronAdapter) GetTransactionLogs(
	ctx context.Context,
	txHash string,
) ([]map[string]any, error) {
	op := rpc.NewRESTOperation(
		"wallet/gettransactioninfobyid",
		"POST",
		map[string]any{"value": txHash},
	)
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction info: %w", err)
	}

	info, ok := result.(map[string]any)
	if !ok {
		return nil, nil
	}

	logs, ok := info["log"].([]any)
	if !ok {
		return nil, nil
	}

	result_logs := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		if logMap, ok := log.(map[string]any); ok {
			result_logs = append(result_logs, logMap)
		}
	}

	return result_logs, nil
}

// parseBlock parses Tron block data into domain.Block.
func (a *TronAdapter) parseBlock(blockData map[string]any) (*domain.Block, error) {
	// Extract blockID (hash)
	blockID, _ := blockData["blockID"].(string)

	// Extract block header
	blockHeader, ok := blockData["block_header"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing block_header")
	}

	rawData, ok := blockHeader["raw_data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing raw_data in block_header")
	}

	number, ok := rawData["number"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid block number")
	}

	parentHash, _ := rawData["parentHash"].(string)

	timestamp, ok := rawData["timestamp"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid timestamp")
	}

	// txCount calculation removed as unused

	return &domain.Block{
		ChainID:    a.chainID,
		Number:     uint64(number),
		Hash:       blockID,
		ParentHash: parentHash,
		Timestamp:  uint64(timestamp) / 1000,
		Status:     domain.BlockStatusPending,
	}, nil
}

// parseTransaction parses Tron transaction data into domain.Transaction.
func (a *TronAdapter) parseTransaction(
	txData map[string]any,
	block *domain.Block,
	txIndex int,
) (*domain.Transaction, error) {
	txID, ok := txData["txID"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid txID")
	}

	// Get raw_data which contains the contract info
	rawData, ok := txData["raw_data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing raw_data")
	}

	// Tron transactions have contracts array
	contracts, ok := rawData["contract"].([]any)
	if !ok || len(contracts) == 0 {
		return nil, nil // Skip transactions without contracts
	}

	// Get first contract (most common case)
	contract, ok := contracts[0].(map[string]any)
	if !ok {
		return nil, nil
	}

	// Extract addresses based on contract type
	from, to, value, txType, tokenAddr, tokenAmount := a.extractTransferInfo(contract)

	rawBytes, _ := json.Marshal(txData)

	return &domain.Transaction{
		ChainID:      a.chainID,
		BlockNumber:  block.Number,
		BlockHash:    block.Hash,
		Hash:         txID,
		Type:         txType,
		TokenAddress: tokenAddr,
		TokenAmount:  tokenAmount,
		Index:        txIndex,
		From:         from,
		To:           to,
		Value:        value,
		Status:       a.getTxStatus(txData),
		Timestamp:    block.Timestamp,
		RawData:      rawBytes,
	}, nil
}

// extractTransferInfo extracts from/to/value and type info from a Tron contract.
func (a *TronAdapter) extractTransferInfo(
	contract map[string]any,
) (from, to, value string, txType domain.TxType, tokenAddr, tokenAmount string) {
	txType = domain.TxTypeNative
	contractType, _ := contract["type"].(string)
	parameter, ok := contract["parameter"].(map[string]any)
	if !ok {
		return "", "", "0", txType, "", ""
	}

	paramValue, ok := parameter["value"].(map[string]any)
	if !ok {
		return "", "", "0", txType, "", ""
	}

	switch contractType {
	case "TransferContract":
		// TRX transfer
		from, _ = paramValue["owner_address"].(string)
		to, _ = paramValue["to_address"].(string)
		if amount, ok := paramValue["amount"].(float64); ok {
			value = strconv.FormatInt(int64(amount), 10)
		}

	case "TransferAssetContract":
		// TRC10 token transfer
		txType = domain.TxTypeTRC10
		from, _ = paramValue["owner_address"].(string)
		to, _ = paramValue["to_address"].(string)
		tokenAddr, _ = paramValue["asset_name"].(string)
		if amount, ok := paramValue["amount"].(float64); ok {
			tokenAmount = strconv.FormatInt(int64(amount), 10)
			value = "0" // Native value is 0
		}

	case "TriggerSmartContract":
		// TRC20 token transfer (smart contract call) - basic info
		txType = domain.TxTypeTRC20
		from, _ = paramValue["owner_address"].(string)
		to, _ = paramValue["contract_address"].(string)
		tokenAddr = to // Contract address is the token address
		value = "0"

		// Try to extract amount from data if it's a standard transfer(address,uint256)
		if data, ok := paramValue["data"].(string); ok {
			// sighash 0xa9059cbb = transfer(address,uint256)
			if len(data) >= 72 && data[:8] == "a9059cbb" {
				// Address is 32 bytes (64 chars) at offset 8
				// Amount is 32 bytes (64 chars) at offset 72
				amountHex := data[72 : 72+64]
				if n, err := strconv.ParseUint(amountHex, 16, 64); err == nil {
					tokenAmount = strconv.FormatUint(n, 10)
				}
			}
		}

	default:
		// Other contract types
		from, _ = paramValue["owner_address"].(string)
	}

	if value == "" {
		value = "0"
	}

	return
}

// getTxStatus returns the transaction status.
func (a *TronAdapter) getTxStatus(txData map[string]any) domain.TxStatus {
	// Check ret field for transaction result
	ret, ok := txData["ret"].([]any)
	if !ok || len(ret) == 0 {
		return domain.TxStatusSuccess // Default to success if no ret
	}

	retData, ok := ret[0].(map[string]any)
	if !ok {
		return domain.TxStatusSuccess
	}

	contractRet, _ := retData["contractRet"].(string)
	if contractRet == "SUCCESS" {
		return domain.TxStatusSuccess
	}

	return domain.TxStatusFailed
}
