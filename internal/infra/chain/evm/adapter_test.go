package evm

import (
	"context"
	"testing"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
)

type MockRPCClient struct {
	CallFunc func(ctx context.Context, method string, params any) (any, error)
}

func (m *MockRPCClient) Call(ctx context.Context, method string, params []any) (any, error) {
	return m.CallFunc(ctx, method, params)
}

func (m *MockRPCClient) Execute(ctx context.Context, op rpc.Operation) (any, error) {
	// HTTP path
	return m.CallFunc(ctx, op.Name, op.Params)
}

func TestEVMAdapter_GetLatestBlock(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "eth_blockNumber" {
				return "0x12d687", nil // 1234567
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
		t.Fatalf("expected 1234567, got %d", height)
	}
}

func TestEVMAdapter_GetBlock(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "eth_getBlockByNumber" {
				return map[string]any{
					"number":     "0x12d687",
					"hash":       "0xabc123",
					"parentHash": "0xabc122",
					"timestamp":  "0x65678900",
					"transactions": []any{
						"tx1", "tx2", "tx3",
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
}

func TestEVMAdapter_GetBlock_NotFound(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			return nil, nil // Return nil result for not found
		},
	}

	adapter := NewEVMAdapter("ethereum", mock, 12)

	block, err := adapter.GetBlock(context.Background(), 1234567)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if block != nil {
		t.Fatal("expected nil block")
	}
}

func TestEVMAdapter_GetTransactions(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "eth_getBlockByNumber" {
				return map[string]any{
					"transactions": []any{
						map[string]any{
							"hash":             "0xtx1",
							"from":             "0xAlice",
							"to":               "0xBob",
							"value":            "0x1",
							"transactionIndex": "0x0",
							"gas":              "0x5208",
							"gasPrice":         "0x3b9aca00",
						},
						map[string]any{
							"hash":             "0xtx2",
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
		Timestamp: 1700000000,
	}

	txs, err := adapter.GetTransactions(context.Background(), block)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("expected 2 txs, got %d", len(txs))
	}

	if txs[0].From != "0xalice" {
		t.Errorf("expected from=0xalice, got %s", txs[0].From)
	}
	if txs[0].To != "0xbob" {
		t.Errorf("expected to=0xbob, got %s", txs[0].To)
	}
	if txs[0].Value != "1" {
		t.Errorf("expected value=1, got %s", txs[0].Value)
	}
	if txs[0].GasPrice != "1000000000" {
		t.Errorf("expected gasPrice=1000000000, got %s", txs[0].GasPrice)
	}
	if txs[0].Type != domain.TxTypeNative {
		t.Errorf("expected type=native, got %s", txs[0].Type)
	}

	if txs[1].Value != "0" {
		t.Errorf("expected value=0, got %s", txs[1].Value)
	}
}

func TestEVMAdapter_FilterTransactions(t *testing.T) {
	adapter := NewEVMAdapter("ethereum", nil, 12)

	txs := []*domain.Transaction{
		{Hash: "tx1", From: "0xalice", To: "0xbob"},
		{Hash: "tx2", From: "0xcharlie", To: "0xdave"},
		{Hash: "tx3", From: "0xbob", To: "0xeve"},
		{Hash: "tx4", From: "0xfrank", To: "0xalice"},
	}

	addresses := []string{"0xalice", "0xbob"}

	filtered, err := adapter.FilterTransactions(context.Background(), txs, addresses)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(filtered) != 3 {
		t.Fatalf("expected 3 txs, got %d", len(filtered))
	}
}

func TestEVMAdapter_EnrichTransaction_ERC20(t *testing.T) {
	mock := &MockRPCClient{
		CallFunc: func(ctx context.Context, method string, params any) (any, error) {
			if method == "eth_getTransactionReceipt" {
				return map[string]any{
					"status":  "0x1",
					"gasUsed": "0x5208",
					"logs": []any{
						map[string]any{
							"address": "0xToken",
							"topics": []any{
								"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
								"0x0000000000000000000000000000000000000000000000000000000000000001", // Alice (fake)
								"0x0000000000000000000000000000000000000000000000000000000000000002", // Bob (fake)
							},
							"data": "0xde0b6b3a7640000", // 1 token
						},
					},
				}, nil
			}
			return nil, nil
		},
	}

	adapter := NewEVMAdapter("ethereum", mock, 12)
	// Add fake Alice to monitored set so enrichment detects it
	// address = last 40 hex chars of topic
	// topic ...001 => address 00...001
	utilsAddr := "0x0000000000000000000000000000000000000001"
	adapter.ensureAddressIndex([]string{utilsAddr})

	tx := &domain.Transaction{
		Hash:    "0xtx1",
		RawData: []byte(`{}`),
		Type:    domain.TxTypeNative,
	}

	err := adapter.EnrichTransaction(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tx.Type != domain.TxTypeERC20 {
		t.Errorf("expected type=erc20, got %s", tx.Type)
	}
	if tx.TokenAddress != "0xtoken" {
		t.Errorf("expected tokenAddress=0xtoken, got %s", tx.TokenAddress)
	}
	if tx.TokenAmount != "1000000000000000000" {
		t.Errorf("expected tokenAmount=1000000000000000000, got %s", tx.TokenAmount)
	}
}

func TestEVMAdapter_SupportsBloomFilter(t *testing.T) {
	adapter := NewEVMAdapter("ethereum", nil, 12)

	if !adapter.SupportsBloomFilter() {
		t.Fatal("EVM adapter should support bloom filter")
	}
}

func TestParseHexString(t *testing.T) {
	tests := []struct {
		in  string
		out uint64
	}{
		{"0x0", 0},
		{"0x1", 1},
		{"0xa", 10},
		{"0xff", 255},
		{"0x12d687", 1234567},
	}

	for _, tt := range tests {
		n, err := parseHexString(tt.in)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tt.in, err)
		}
		if n != tt.out {
			t.Fatalf("for %s: expected %d, got %d", tt.in, tt.out, n)
		}
	}
}
