package evm

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

// MockProvider implements rpc.Provider for testing
type MockProvider struct {
	CallFunc func(ctx context.Context, method string, params []any) (any, error)
}

func (m *MockProvider) Call(ctx context.Context, method string, params []any) (any, error) {
	if m.CallFunc != nil {
		return m.CallFunc(ctx, method, params)
	}
	return nil, nil
}

func (m *MockProvider) BatchCall(
	ctx context.Context,
	requests []rpc.BatchRequest,
) ([]rpc.BatchResponse, error) {
	return nil, nil
}

func (m *MockProvider) GetName() string             { return "mock" }
func (m *MockProvider) GetHealth() rpc.HealthStatus { return rpc.HealthStatus{Available: true} }
func (m *MockProvider) Close() error                { return nil }

func TestEVMAdapter_GetLatestBlock(t *testing.T) {
	mock := &MockProvider{
		CallFunc: func(ctx context.Context, method string, params []any) (any, error) {
			if method == "eth_blockNumber" {
				return "0x12d687", nil // 1234567 in hex
			}
			return nil, nil
		},
	}

	adapter := NewEVMAdapter("ethereum", mock, 12)
	height, err := adapter.GetLatestBlock(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if height != 1234567 {
		t.Errorf("expected height 1234567, got %d", height)
	}
}

func TestEVMAdapter_GetBlock(t *testing.T) {
	mock := &MockProvider{
		CallFunc: func(ctx context.Context, method string, params []any) (any, error) {
			if method == "eth_getBlockByNumber" {
				return map[string]any{
					"number":     "0x12d687",
					"hash":       "0xabc123",
					"parentHash": "0xabc122",
					"timestamp":  "0x65678900",
					"transactions": []any{
						"0xtx1", "0xtx2", "0xtx3",
					},
					"gasUsed":  "0x5208",
					"gasLimit": "0x1234567",
					"miner":    "0xminer",
				}, nil
			}
			return nil, nil
		},
	}

	adapter := NewEVMAdapter("ethereum", mock, 12)
	block, err := adapter.GetBlock(context.Background(), 1234567)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Number != 1234567 {
		t.Errorf("expected block number 1234567, got %d", block.Number)
	}
	if block.Hash != "0xabc123" {
		t.Errorf("unexpected block hash: %s", block.Hash)
	}
	if block.TxCount != 3 {
		t.Errorf("expected tx count 3, got %d", block.TxCount)
	}
}

func TestEVMAdapter_GetTransactions(t *testing.T) {
	mock := &MockProvider{
		CallFunc: func(ctx context.Context, method string, params []any) (any, error) {
			if method == "eth_getBlockByNumber" {
				return map[string]any{
					"number":     "0x12d687",
					"hash":       "0xabc123",
					"parentHash": "0xabc122",
					"timestamp":  "0x65678900",
					"transactions": []any{
						map[string]any{
							"hash":             "0xtx1hash",
							"from":             "0xAlice",
							"to":               "0xBob",
							"value":            "0xde0b6b3a7640000", // 1 ETH
							"transactionIndex": "0x0",
							"gas":              "0x5208",
							"gasPrice":         "0x3b9aca00",
						},
						map[string]any{
							"hash":             "0xtx2hash",
							"from":             "0xCharlie",
							"to":               "0xDave",
							"value":            "0x0",
							"transactionIndex": "0x1",
							"gas":              "0x10000",
							"gasPrice":         "0x3b9aca00",
						},
					},
				}, nil
			}
			return nil, nil
		},
	}

	adapter := NewEVMAdapter("ethereum", mock, 12)
	block := &domain.Block{
		Number:    1234567,
		Hash:      "0xabc123",
		Timestamp: time.Unix(1700000000, 0),
		TxCount:   2,
	}

	txs, err := adapter.GetTransactions(context.Background(), block)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txs))
	}

	// Check first tx
	if txs[0].From != "0xalice" { // Should be lowercased
		t.Errorf("expected from=0xalice, got %s", txs[0].From)
	}
	if txs[0].To != "0xbob" {
		t.Errorf("expected to=0xbob, got %s", txs[0].To)
	}
}

func TestEVMAdapter_FilterTransactions(t *testing.T) {
	adapter := NewEVMAdapter("ethereum", nil, 12)

	txs := []*domain.Transaction{
		{TxHash: "tx1", From: "0xalice", To: "0xbob"},
		{TxHash: "tx2", From: "0xcharlie", To: "0xdave"},
		{TxHash: "tx3", From: "0xbob", To: "0xeve"},
		{TxHash: "tx4", From: "0xfrank", To: "0xalice"},
	}

	watchedAddresses := []string{"0xalice", "0xbob"}

	filtered, err := adapter.FilterTransactions(context.Background(), txs, watchedAddresses)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("expected 3 filtered transactions (tx1, tx3, tx4), got %d", len(filtered))
	}
}

func TestEVMAdapter_SupportsBloomFilter(t *testing.T) {
	adapter := NewEVMAdapter("ethereum", nil, 12)

	if !adapter.SupportsBloomFilter() {
		t.Error("EVM adapter SHOULD support bloom filter")
	}
}

func TestEVMAdapter_ParseHexString(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"0x0", 0},
		{"0x1", 1},
		{"0xa", 10},
		{"0xff", 255},
		{"0x12d687", 1234567},
	}

	for _, tt := range tests {
		result, err := parseHexString(tt.input)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", tt.input, err)
			continue
		}
		if result != tt.expected {
			t.Errorf("for %s: expected %d, got %d", tt.input, tt.expected, result)
		}
	}
}
