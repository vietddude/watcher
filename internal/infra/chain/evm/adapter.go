package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"sync"

	logger "log/slog"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/filter"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"golang.org/x/sync/errgroup"
)

type EVMAdapter struct {
	chainID         domain.ChainID
	client          rpc.RPCClient
	finalityBlocks  uint64
	bloomFilter     *filter.BloomFilter
	addressSet      map[string]struct{}
	lastAddressHash uint64
	filterMu        sync.RWMutex // Protects addressSet and bloomFilter
	log             logger.Logger

	// Transaction cache to avoid redundant RPC calls
	// GetBlock fetches with full transactions, GetTransactions reuses this data
	txCacheMu      sync.Mutex
	cachedBlockNum uint64
	cachedRawTxs   []any
}

func NewEVMAdapter(
	chainID domain.ChainID,
	client rpc.RPCClient,
	finalityBlocks uint64,
) *EVMAdapter {
	return &EVMAdapter{
		chainID:        chainID,
		client:         client,
		finalityBlocks: finalityBlocks,
		bloomFilter:    filter.NewBloomFilter(),
		addressSet:     make(map[string]struct{}),
		log:            *logger.Default(),
	}
}

func (a *EVMAdapter) GetLatestBlock(ctx context.Context) (uint64, error) {
	op := rpc.NewHTTPOperation("eth_blockNumber", nil)
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return 0, fmt.Errorf("eth_blockNumber failed: %w", err)
	}

	blockHex, ok := result.(string)
	if !ok {
		return 0, fmt.Errorf("invalid block number response")
	}

	return parseHexString(blockHex)
}

func (a *EVMAdapter) GetBlock(ctx context.Context, blockNumber uint64) (*domain.Block, error) {
	blockHex := fmt.Sprintf("0x%x", blockNumber)
	op := rpc.NewHTTPOperation("eth_getBlockByNumber", []any{blockHex, true})
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, fmt.Errorf("eth_getBlockByNumber failed: %w", err)
	}
	if result == nil {
		return nil, nil // Not found/future
	}

	rawBlock, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block format")
	}

	// Cache raw transactions to avoid redundant RPC call in GetTransactions
	a.txCacheMu.Lock()
	if rawTxs, ok := rawBlock["transactions"].([]any); ok {
		a.cachedBlockNum = blockNumber
		a.cachedRawTxs = rawTxs
	}
	a.txCacheMu.Unlock()

	return a.parseBlock(rawBlock)
}

func (a *EVMAdapter) parseBlock(raw map[string]any) (*domain.Block, error) {
	number, _ := parseHexString(getString(raw["number"]))
	timestamp, _ := parseHexString(getString(raw["timestamp"]))

	return &domain.Block{
		ChainID:    a.chainID,
		Number:     number,
		Hash:       getString(raw["hash"]),
		ParentHash: getString(raw["parentHash"]),
		Timestamp:  timestamp,
		Status:     domain.BlockStatusProcessed, // Default
	}, nil
}

func (a *EVMAdapter) GetBlockByHash(ctx context.Context, blockHash string) (*domain.Block, error) {
	op := rpc.NewHTTPOperation("eth_getBlockByHash", []any{blockHash, false})
	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return a.parseBlock(result.(map[string]any))
}

func (a *EVMAdapter) VerifyBlockHash(
	ctx context.Context,
	blockNumber uint64,
	expectedHash string,
) (bool, error) {
	block, err := a.GetBlock(ctx, blockNumber)
	if err != nil {
		return false, err
	}
	if block == nil {
		return false, fmt.Errorf("block not found for verification")
	}
	return block.Hash == expectedHash, nil
}

func (a *EVMAdapter) GetFinalityDepth() uint64 {
	return a.finalityBlocks
}

func (a *EVMAdapter) GetChainID() domain.ChainID {
	return a.chainID
}

func (a *EVMAdapter) SupportsBloomFilter() bool {
	return true
}

func (a *EVMAdapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	var rawTxs []any

	// Try to use cached transactions from GetBlock (avoids redundant RPC call)
	a.txCacheMu.Lock()
	if a.cachedBlockNum == block.Number && a.cachedRawTxs != nil {
		rawTxs = a.cachedRawTxs
		// Clear cache after use
		a.cachedRawTxs = nil
	}
	a.txCacheMu.Unlock()

	// Fallback: fetch if cache miss (shouldn't happen in normal flow)
	if rawTxs == nil {
		blockHex := fmt.Sprintf("0x%x", block.Number)
		op := rpc.NewHTTPOperation("eth_getBlockByNumber", []any{blockHex, true})
		result, err := a.client.Execute(ctx, op)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
		}

		rawBlock := result.(map[string]any)
		var ok bool
		rawTxs, ok = rawBlock["transactions"].([]any)
		if !ok {
			return []*domain.Transaction{}, nil
		}
	}

	txs := make([]*domain.Transaction, 0, len(rawTxs))
	for i, txDataRaw := range rawTxs {
		txData, ok := txDataRaw.(map[string]any)
		if !ok {
			continue
		}

		tx, err := a.parseTransaction(txData, block)
		if err != nil {
			a.log.Warn("parse tx failed", "error", err, "index", i)
			continue
		}

		txs = append(txs, tx)
	}

	return txs, nil
}

func (a *EVMAdapter) parseTransaction(
	raw map[string]any,
	block *domain.Block,
) (*domain.Transaction, error) {
	txIndex, _ := parseHexString(getString(raw["transactionIndex"]))

	// Ensure raw data is preserved for heuristic filtering
	rawDataBytes, _ := json.Marshal(raw)

	return &domain.Transaction{
		ChainID:     a.chainID,
		Hash:        getString(raw["hash"]),
		BlockNumber: block.Number,
		BlockHash:   block.Hash,
		Type:        domain.TxTypeNative,
		Index:       int(txIndex),
		From:        strings.ToLower(getString(raw["from"])),
		To:          strings.ToLower(getString(raw["to"])),
		Value:       a.hexToDecimal(getString(raw["value"])),
		GasPrice:    a.hexToDecimal(getString(raw["gasPrice"])),
		Timestamp:   block.Timestamp, // Inherit from block
		RawData:     rawDataBytes,
		Status:      domain.TxStatusSuccess, // Default, will be enriched
	}, nil
}

// FilterTransactions now filters AFTER enrichment or more intelligently
func (a *EVMAdapter) FilterTransactions(
	ctx context.Context,
	txs []*domain.Transaction,
	addresses []string,
) ([]*domain.Transaction, error) {
	// 1. Update filter if needed (Write Lock)
	a.filterMu.Lock()
	a.ensureAddressIndex(addresses)
	a.filterMu.Unlock()

	// 2. Filter transactions (Read Lock)
	a.filterMu.RLock()
	defer a.filterMu.RUnlock()

	filtered := make([]*domain.Transaction, 0)
	for _, tx := range txs {
		if a.isRelevantTransaction(tx) {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
}

// isRelevantTransaction checks if transaction involves monitored addresses
func (a *EVMAdapter) isRelevantTransaction(tx *domain.Transaction) bool {
	// Direct from/to match
	if _, ok := a.addressSet[strings.ToLower(tx.From)]; ok {
		return true
	}
	if _, ok := a.addressSet[strings.ToLower(tx.To)]; ok {
		return true
	}

	// Check if RawData contains ERC20 transfer info (after enrichment)
	var tokenInfo map[string]string
	if err := json.Unmarshal(tx.RawData, &tokenInfo); err == nil {
		if tokenInfo["type"] == "ERC20" {
			// Already marked as relevant during enrichment
			return true
		}
	}

	// Fallback: Check raw input data for address presence
	txDataStr := strings.ToLower(string(tx.RawData))
	for addr := range a.addressSet {
		if len(addr) < 30 {
			continue
		}
		cleanAddr := strings.TrimPrefix(strings.ToLower(addr), "0x")
		if strings.Contains(txDataStr, cleanAddr) {
			return true
		}
	}

	return false
}

func (a *EVMAdapter) ensureAddressIndex(addresses []string) {
	h := hashAddresses(addresses)
	if h == a.lastAddressHash {
		return
	}

	a.bloomFilter.Clear()
	a.bloomFilter.Build(addresses)
	a.addressSet = make(map[string]struct{}, len(addresses))

	for _, addr := range addresses {
		lower := strings.ToLower(addr)
		a.addressSet[lower] = struct{}{}
	}

	a.lastAddressHash = h
}

// EnrichTransaction - FIXED version
func (a *EVMAdapter) EnrichTransaction(ctx context.Context, tx *domain.Transaction) error {
	op := rpc.NewHTTPOperation("eth_getTransactionReceipt", []any{tx.Hash})
	result, err := a.client.Execute(ctx, op)
	if err != nil || result == nil {
		return err
	}

	receipt, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid receipt format")
	}

	a.processReceipt(tx, receipt)
	return nil
}

// EnrichTransactions - parallel batch version with errgroup
func (a *EVMAdapter) EnrichTransactions(ctx context.Context, txs []*domain.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	rpcProvider, ok := a.client.(rpc.RPCProvider)
	if !ok {
		// Fallback to parallel individual calls using errgroup
		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(5) // Limit concurrency to prevent RPC overload

		for _, tx := range txs {
			tx := tx // capture loop variable
			g.Go(func() error {
				if err := a.EnrichTransaction(ctx, tx); err != nil {
					a.log.Warn("Failed to enrich transaction", "tx", tx.Hash, "error", err)
					// Don't fail the entire batch for individual errors
				}
				return nil
			})
		}
		return g.Wait()
	}

	// Use batch RPC calls with parallel chunk processing
	chunkSize := 10 // Increased chunk size for better efficiency
	numChunks := (len(txs) + chunkSize - 1) / chunkSize

	// Process chunks in parallel with limited concurrency
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(3) // Max 3 concurrent batch requests

	for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
		chunkIdx := chunkIdx // capture loop variable
		start := chunkIdx * chunkSize
		end := min(start+chunkSize, len(txs))
		chunkTxs := txs[start:end]

		g.Go(func() error {
			requests := make([]rpc.BatchRequest, len(chunkTxs))
			for i, tx := range chunkTxs {
				requests[i] = rpc.BatchRequest{
					Method: "eth_getTransactionReceipt",
					Params: []any{tx.Hash},
				}
			}

			responses, err := rpcProvider.BatchCall(ctx, requests)
			if err != nil {
				a.log.Warn("batch receipt fetch failed", "chunk", chunkIdx, "error", err)
				return nil // Don't fail entire batch
			}

			for j, resp := range responses {
				if j >= len(chunkTxs) {
					break
				}
				tx := chunkTxs[j]
				if resp.Error != nil {
					a.log.Warn("failed to fetch receipt", "tx", tx.Hash, "error", resp.Error)
					continue
				}
				if resp.Result == nil {
					continue
				}
				receipt, ok := resp.Result.(map[string]any)
				if !ok {
					continue
				}
				a.processReceipt(tx, receipt)
			}
			return nil
		})
	}

	return g.Wait()
}

// processReceipt - FIXED to handle ALL relevant transfers
func (a *EVMAdapter) processReceipt(tx *domain.Transaction, receipt map[string]any) {
	// Update gas and status
	if gu, ok := receipt["gasUsed"].(string); ok {
		tx.GasUsed, _ = parseHexString(gu)
	}
	if st, ok := receipt["status"].(string); ok && st == "0x0" {
		tx.Status = domain.TxStatusFailed
	}

	logs, ok := receipt["logs"].([]any)
	if !ok || len(logs) == 0 {
		return
	}

	// ERC20 Transfer event signature
	const transferEventSig = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

	var relevantTransfers []map[string]string

	for _, logRaw := range logs {
		logData, ok := logRaw.(map[string]any)
		if !ok {
			continue
		}

		topics, ok := logData["topics"].([]any)
		if !ok || len(topics) < 3 {
			continue
		}

		topic0, ok := topics[0].(string)
		if !ok || topic0 != transferEventSig {
			continue
		}

		// Extract addresses - LOWERCASE IMMEDIATELY
		fromTopic, _ := topics[1].(string)
		toTopic, _ := topics[2].(string)
		tokenContract, _ := logData["address"].(string)
		dataHex, _ := logData["data"].(string)

		// Parse addresses and normalize
		tokenFrom := extractAddress(fromTopic)
		tokenTo := extractAddress(toTopic)
		tokenContract = strings.ToLower(tokenContract)

		// Check if ANY part involves monitored addresses
		// Lock map for lookup (EnrichTransaction calls this, may be concurrent with Filter update)
		a.filterMu.RLock()
		_, fromMonitored := a.addressSet[tokenFrom]
		_, toMonitored := a.addressSet[tokenTo]
		_, contractMonitored := a.addressSet[tokenContract]
		a.filterMu.RUnlock()

		if fromMonitored || toMonitored || contractMonitored {
			tokenValue := "0"
			if len(dataHex) > 2 {
				if parsed, err := parseHexToBigInt(dataHex); err == nil {
					tokenValue = parsed.String()
				}
			}

			transferInfo := map[string]string{
				"type":     "ERC20",
				"contract": tokenContract,
				"from":     tokenFrom,
				"to":       tokenTo,
				"value":    tokenValue,
			}

			relevantTransfers = append(relevantTransfers, transferInfo)
		}
	}

	// Store ALL relevant transfers
	if len(relevantTransfers) > 0 {
		tx.Type = domain.TxTypeERC20
		// Use the first transfer as the primary token info for the main columns
		tx.TokenAddress = relevantTransfers[0]["contract"]
		tx.TokenAmount = relevantTransfers[0]["value"]

		// If only one transfer, simplify storage
		if len(relevantTransfers) == 1 {
			tx.RawData, _ = json.Marshal(relevantTransfers[0])
			// Update tx fields to reflect the transfer
			tx.To = relevantTransfers[0]["to"]
			tx.Value = relevantTransfers[0]["value"]
		} else {
			// Multiple transfers - store as array in RawData
			enrichedData := map[string]any{
				"type":      "ERC20_MULTIPLE",
				"transfers": relevantTransfers,
			}
			tx.RawData, _ = json.Marshal(enrichedData)
		}
	}
}

func (a *EVMAdapter) hexToDecimal(hexStr string) string {
	if hexStr == "" || hexStr == "0x" {
		return "0"
	}
	n, err := parseHexToBigInt(hexStr)
	if err != nil {
		return "0"
	}
	return n.String()
}

// extractAddress normalizes a topic to a checksummed address
func extractAddress(topic string) string {
	if len(topic) >= 42 {
		return strings.ToLower("0x" + topic[len(topic)-40:])
	}
	return ""
}

func parseHexToBigInt(hexStr string) (*big.Int, error) {
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimPrefix(hexStr, "0x"), 16); !ok {
		return nil, fmt.Errorf("invalid hex: %s", hexStr)
	}
	return n, nil
}

func parseHexString(hexStr string) (uint64, error) {
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimPrefix(hexStr, "0x"), 16); !ok {
		return 0, fmt.Errorf("invalid hex: %s", hexStr)
	}
	return n.Uint64(), nil
}

func getString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func hashAddresses(addrs []string) uint64 {
	var h uint64
	for _, a := range addrs {
		for i := 0; i < len(a); i++ {
			h = h*131 + uint64(a[i])
		}
	}
	return h
}
