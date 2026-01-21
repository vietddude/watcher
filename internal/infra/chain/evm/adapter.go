package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/filter"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

type EVMAdapter struct {
	chainID        domain.ChainID
	client         rpc.RPCClient
	finalityBlocks uint64

	// filtering
	bloomFilter     *filter.BloomFilter
	addressSet      map[string]struct{}
	lastAddressHash uint64
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
	op := rpc.NewHTTPOperation("eth_getBlockByNumber", []any{blockHex, false})

	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block format")
	}

	return a.parseBlock(blockData)
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

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block format")
	}

	return a.parseBlock(blockData)
}

func (a *EVMAdapter) GetTransactions(
	ctx context.Context,
	block *domain.Block,
) ([]*domain.Transaction, error) {
	blockHex := fmt.Sprintf("0x%x", block.Number)
	op := rpc.NewHTTPOperation("eth_getBlockByNumber", []any{blockHex, true})

	result, err := a.client.Execute(ctx, op)
	if err != nil {
		return nil, err
	}

	blockData, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid block format")
	}

	rawTxs, ok := blockData["transactions"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid transactions format")
	}

	txs := make([]*domain.Transaction, 0, len(rawTxs))
	for i, raw := range rawTxs {
		txData, ok := raw.(map[string]any)
		if !ok {
			slog.Warn("skip invalid tx", "index", i)
			continue
		}

		tx, err := a.parseTransaction(txData, block)
		if err != nil {
			slog.Warn("parse tx failed", "error", err, "index", i)
			continue
		}

		txs = append(txs, tx)
	}

	return txs, nil
}

func (a *EVMAdapter) FilterTransactions(
	ctx context.Context,
	txs []*domain.Transaction,
	addresses []string,
) ([]*domain.Transaction, error) {

	a.ensureAddressIndex(addresses)

	filtered := make([]*domain.Transaction, 0)

	for _, tx := range txs {
		// fast path
		if !a.bloomFilter.MayContain(tx.From) && !a.bloomFilter.MayContain(tx.To) {
			continue
		}

		// exact match (O(1))
		if _, ok := a.addressSet[tx.From]; ok {
			filtered = append(filtered, tx)
			continue
		}
		if _, ok := a.addressSet[tx.To]; ok {
			filtered = append(filtered, tx)
		}
	}

	return filtered, nil
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
		a.addressSet[strings.ToLower(addr)] = struct{}{}
	}

	a.lastAddressHash = h
}

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

	if gu, ok := receipt["gasUsed"].(string); ok {
		tx.GasUsed, _ = parseHexString(gu)
	}
	if st, ok := receipt["status"].(string); ok && st == "0x0" {
		tx.Status = domain.TxStatusFailed
	}

	return nil
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
	return strings.EqualFold(block.Hash, expectedHash), nil
}

func (a *EVMAdapter) GetFinalityDepth() uint64   { return a.finalityBlocks }
func (a *EVMAdapter) GetChainID() domain.ChainID { return a.chainID }
func (a *EVMAdapter) SupportsBloomFilter() bool  { return true }

func (a *EVMAdapter) parseBlock(data map[string]any) (*domain.Block, error) {
	number, err := parseHexString(data["number"].(string))
	if err != nil {
		return nil, err
	}

	block := &domain.Block{
		ChainID:    a.chainID,
		Number:     number,
		Hash:       data["hash"].(string),
		ParentHash: data["parentHash"].(string),
		Status:     domain.BlockStatusPending,
	}

	if ts, ok := data["timestamp"].(string); ok {
		block.Timestamp, _ = parseHexString(ts)
	}
	if _, ok := data["transactions"].([]any); ok {
		// Optimization: We could store tx count if needed primarily, but field is removed
	}

	// Metadata removed

	return block, nil
}

func (a *EVMAdapter) parseTransaction(
	data map[string]any,
	block *domain.Block,
) (*domain.Transaction, error) {

	raw, _ := json.Marshal(data)

	tx := &domain.Transaction{
		ChainID:     a.chainID,
		BlockNumber: block.Number,
		BlockHash:   block.Hash,
		Hash:        data["hash"].(string),
		From:        strings.ToLower(data["from"].(string)),
		To:          strings.ToLower(getString(data["to"])),
		Value:       getString(data["value"]),
		GasPrice:    getString(data["gasPrice"]),
		Status:      domain.TxStatusSuccess,
		Timestamp:   block.Timestamp,
		RawData:     raw,
	}

	if idx, ok := data["transactionIndex"].(string); ok {
		tx.Index, _ = intFromHex(idx)
	}
	if gas, ok := data["gas"].(string); ok {
		tx.GasUsed, _ = parseHexString(gas)
	}

	return tx, nil
}

func parseHexString(hexStr string) (uint64, error) {
	n := new(big.Int)
	if _, ok := n.SetString(strings.TrimPrefix(hexStr, "0x"), 16); !ok {
		return 0, fmt.Errorf("invalid hex: %s", hexStr)
	}
	return n.Uint64(), nil
}

func intFromHex(hexStr string) (int, error) {
	n, err := parseHexString(hexStr)
	return int(n), err
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
