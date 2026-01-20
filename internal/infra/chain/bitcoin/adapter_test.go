package bitcoin

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
	return m.CallFunc(ctx, method, params)
}

func (m *MockRPCClient) Execute(ctx context.Context, op rpc.Operation) (any, error) {
	if op.Invoke != nil {
		return op.Invoke(ctx)
	}
	return m.CallFunc(ctx, op.Name, op.Params)
}

func TestBitcoinAdapter_GetLatestBlock(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "getblockcount" {
				return float64(800000), nil
			}
			return nil, nil
		},
	}

	adapter := NewBitcoinAdapter("bitcoin", mock, 6)
	height, err := adapter.GetLatestBlock(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if height != 800000 {
		t.Errorf("expected height 800000, got %d", height)
	}
}

func TestBitcoinAdapter_GetBlock(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "getblockhash" {
				return "00000000000000000001a2b3c4d5e6f7", nil
			}
			if method == "getblock" {
				return map[string]any{
					"height":            float64(800000),
					"hash":              "00000000000000000001a2b3c4d5e6f7",
					"previousblockhash": "00000000000000000001a2b3c4d5e6f6",
					"time":              float64(1700000000),
					"nTx":               float64(2500),
				}, nil
			}
			return nil, nil
		},
	}

	adapter := NewBitcoinAdapter("bitcoin", mock, 6)
	block, err := adapter.GetBlock(context.Background(), 800000)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block.Number != 800000 {
		t.Errorf("expected block number 800000, got %d", block.Number)
	}
	if block.Hash != "00000000000000000001a2b3c4d5e6f7" {
		t.Errorf("unexpected block hash: %s", block.Hash)
	}
	if block.TxCount != 2500 {
		t.Errorf("expected tx count 2500, got %d", block.TxCount)
	}
}

func TestBitcoinAdapter_ExtractOutputAddress(t *testing.T) {
	adapter := NewBitcoinAdapter("bitcoin", nil, 6)

	tests := []struct {
		name     string
		voutData map[string]any
		expected string
	}{
		{
			name: "modern address field (SegWit)",
			voutData: map[string]any{
				"scriptPubKey": map[string]any{
					"address": "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
				},
			},
			expected: "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
		},
		{
			name: "legacy addresses array",
			voutData: map[string]any{
				"scriptPubKey": map[string]any{
					"addresses": []any{"1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2"},
				},
			},
			expected: "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2",
		},
		{
			name: "OP_RETURN (no address)",
			voutData: map[string]any{
				"scriptPubKey": map[string]any{
					"type": "nulldata",
					"asm":  "OP_RETURN 48656c6c6f",
				},
			},
			expected: "",
		},
		{
			name:     "missing scriptPubKey",
			voutData: map[string]any{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.extractOutputAddress(tt.voutData)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBitcoinAdapter_ParseUTXOTransaction(t *testing.T) {
	adapter := NewBitcoinAdapter("bitcoin", nil, 6)

	block := &domain.Block{
		Number:    800000,
		Hash:      "00000000000000000001a2b3c4d5e6f7",
		Timestamp: 1700000000,
	}

	txData := map[string]any{
		"txid": "abc123def456",
		"vout": []any{
			map[string]any{
				"value": float64(0.5),
				"scriptPubKey": map[string]any{
					"address": "bc1qrecipient1",
				},
			},
			map[string]any{
				"value": float64(0.3),
				"scriptPubKey": map[string]any{
					"addresses": []any{"1ChangeAddress"},
				},
			},
			// OP_RETURN output (should be skipped)
			map[string]any{
				"value": float64(0),
				"scriptPubKey": map[string]any{
					"type": "nulldata",
				},
			},
		},
	}

	txs, err := adapter.parseUTXOTransaction(txData, block, 0)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txs))
	}

	// Check first output
	if txs[0].To != "bc1qrecipient1" {
		t.Errorf("expected To=bc1qrecipient1, got %s", txs[0].To)
	}
	if txs[0].Value != "50000000" { // 0.5 BTC in satoshis
		t.Errorf("expected Value=50000000, got %s", txs[0].Value)
	}
	if txs[0].From != "" {
		t.Errorf("expected From to be empty (outputs-only), got %s", txs[0].From)
	}

	// Check second output
	if txs[1].To != "1ChangeAddress" {
		t.Errorf("expected To=1ChangeAddress, got %s", txs[1].To)
	}
}

func TestBitcoinAdapter_FilterTransactions(t *testing.T) {
	adapter := NewBitcoinAdapter("bitcoin", nil, 6)

	txs := []*domain.Transaction{
		{TxHash: "tx1", To: "bc1qwatched"},
		{TxHash: "tx2", To: "bc1qrandom"},
		{TxHash: "tx3", To: "1WatchedLegacy"},
		{TxHash: "tx4", To: "bc1qanotherrandom"},
	}

	watchedAddresses := []string{"bc1qwatched", "1WatchedLegacy"}

	filtered, err := adapter.FilterTransactions(context.Background(), txs, watchedAddresses)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered transactions, got %d", len(filtered))
	}
	if filtered[0].TxHash != "tx1" {
		t.Errorf("expected tx1, got %s", filtered[0].TxHash)
	}
	if filtered[1].TxHash != "tx3" {
		t.Errorf("expected tx3, got %s", filtered[1].TxHash)
	}
}

func TestBitcoinAdapter_SupportsBloomFilter(t *testing.T) {
	adapter := NewBitcoinAdapter("bitcoin", nil, 6)

	if adapter.SupportsBloomFilter() {
		t.Error("Bitcoin adapter should NOT support bloom filter")
	}
}
