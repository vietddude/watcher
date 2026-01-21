package tron

import (
	"context"
	"testing"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

// MockRPCClient implements rpc.RPCClient for testing
type MockRPCClient struct {
	CallFunc func(ctx context.Context, method string, params any) (any, error)
}

func (m *MockRPCClient) Call(ctx context.Context, method string, params []any) (any, error) {
	if m.CallFunc != nil {
		return m.CallFunc(ctx, method, params)
	}
	return nil, nil
}

func (m *MockRPCClient) Execute(ctx context.Context, op rpc.Operation) (any, error) {
	if op.Invoke != nil {
		return op.Invoke(ctx)
	}
	if m.CallFunc != nil {
		return m.CallFunc(ctx, op.Name, op.Params)
	}
	return nil, nil
}

func TestTronAdapter_GetLatestBlock(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "wallet/getnowblock" {
				return map[string]any{
					"blockID": "0000000003abc123",
					"block_header": map[string]any{
						"raw_data": map[string]any{
							"number":    float64(62345555),
							"timestamp": float64(1700000000000),
						},
					},
				}, nil
			}
			return nil, nil
		},
	}

	adapter := NewTronAdapter("tron", mock, 19)
	height, err := adapter.GetLatestBlock(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if height != 62345555 {
		t.Errorf("expected height 62345555, got %d", height)
	}
}

func TestTronAdapter_GetBlock(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "wallet/getblockbynum" {
				return map[string]any{
					"blockID": "0000000003abc123",
					"block_header": map[string]any{
						"raw_data": map[string]any{
							"number":          float64(62345555),
							"timestamp":       float64(1700000000000),
							"parentHash":      "0000000003abc122",
							"witness_address": "TRX...",
						},
					},
					"transactions": []any{
						map[string]any{"txID": "tx1"},
						map[string]any{"txID": "tx2"},
					},
				}, nil
			}
			return nil, nil
		},
	}

	adapter := NewTronAdapter("tron", mock, 19)
	block, err := adapter.GetBlock(context.Background(), 62345555)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Number != 62345555 {
		t.Errorf("expected block number 62345555, got %d", block.Number)
	}
	if block.Hash != "0000000003abc123" {
		t.Errorf("unexpected block hash: %s", block.Hash)
	}
}

func TestTronAdapter_ParseTransaction_TransferContract(t *testing.T) {
	adapter := NewTronAdapter("tron", nil, 19)

	block := &domain.Block{
		Number:    62345555,
		Hash:      "0000000003abc123",
		Timestamp: 1700000000,
	}

	txData := map[string]any{
		"txID": "abc123txid",
		"raw_data": map[string]any{
			"contract": []any{
				map[string]any{
					"type": "TransferContract",
					"parameter": map[string]any{
						"value": map[string]any{
							"owner_address": "TFromAddress",
							"to_address":    "TToAddress",
							"amount":        float64(1000000), // 1 TRX
						},
					},
				},
			},
		},
		"ret": []any{
			map[string]any{"contractRet": "SUCCESS"},
		},
	}

	tx, err := adapter.parseTransaction(txData, block, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Hash != "abc123txid" {
		t.Errorf("expected txID=abc123txid, got %s", tx.Hash)
	}
	if tx.From != "TFromAddress" {
		t.Errorf("expected from=TFromAddress, got %s", tx.From)
	}
	if tx.To != "TToAddress" {
		t.Errorf("expected to=TToAddress, got %s", tx.To)
	}
	if tx.Value != "1000000" {
		t.Errorf("expected value=1000000, got %s", tx.Value)
	}
	if tx.Status != domain.TxStatusSuccess {
		t.Errorf("expected status=success, got %s", tx.Status)
	}
}

func TestTronAdapter_FilterTransactions(t *testing.T) {
	adapter := NewTronAdapter("tron", nil, 19)

	txs := []*domain.Transaction{
		{Hash: "tx1", From: "TWatched", To: "TRandom"},
		{Hash: "tx2", From: "TRandom", To: "TRandom2"},
		{Hash: "tx3", From: "TRandom3", To: "TWatched"},
	}

	watchedAddresses := []string{"TWatched"}

	filtered, err := adapter.FilterTransactions(context.Background(), txs, watchedAddresses)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered transactions, got %d", len(filtered))
	}
}

func TestTronAdapter_SupportsBloomFilter(t *testing.T) {
	adapter := NewTronAdapter("tron", nil, 19)

	if adapter.SupportsBloomFilter() {
		t.Error("Tron adapter should NOT support bloom filter")
	}
}

func TestTronAdapter_GetTxStatus(t *testing.T) {
	adapter := NewTronAdapter("tron", nil, 19)

	tests := []struct {
		name     string
		txData   map[string]any
		expected domain.TxStatus
	}{
		{
			name: "success",
			txData: map[string]any{
				"ret": []any{
					map[string]any{"contractRet": "SUCCESS"},
				},
			},
			expected: domain.TxStatusSuccess,
		},
		{
			name: "failed",
			txData: map[string]any{
				"ret": []any{
					map[string]any{"contractRet": "REVERT"},
				},
			},
			expected: domain.TxStatusFailed,
		},
		{
			name:     "no ret field",
			txData:   map[string]any{},
			expected: domain.TxStatusSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.getTxStatus(tt.txData)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
