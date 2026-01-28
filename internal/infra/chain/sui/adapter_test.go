package sui

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	v2 "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// MockRPCClient implementation
type MockRPCClient struct {
	conn *grpc.ClientConn
}

func (m *MockRPCClient) Execute(ctx context.Context, op rpc.Operation) (any, error) {
	if op.GRPCHandler != nil {
		return op.GRPCHandler(ctx, m.conn)
	}
	return nil, fmt.Errorf("mock only supports grpc")
}

func (m *MockRPCClient) Call(ctx context.Context, method string, params []any) (any, error) {
	return nil, fmt.Errorf("not implemented")
}

// Ensure MockLedgerClient implements v2.LedgerServiceServer
type MockLedgerServer struct {
	v2.UnimplementedLedgerServiceServer
	GetServiceInfoFunc func(context.Context, *v2.GetServiceInfoRequest) (*v2.GetServiceInfoResponse, error)
	GetCheckpointFunc  func(context.Context, *v2.GetCheckpointRequest) (*v2.GetCheckpointResponse, error)
	GetTransactionFunc func(context.Context, *v2.GetTransactionRequest) (*v2.GetTransactionResponse, error)
}

func (m *MockLedgerServer) GetServiceInfo(
	ctx context.Context,
	in *v2.GetServiceInfoRequest,
) (*v2.GetServiceInfoResponse, error) {
	if m.GetServiceInfoFunc != nil {
		return m.GetServiceInfoFunc(ctx, in)
	}
	return nil, fmt.Errorf("GetServiceInfo not implemented")
}

func (m *MockLedgerServer) GetCheckpoint(
	ctx context.Context,
	in *v2.GetCheckpointRequest,
) (*v2.GetCheckpointResponse, error) {
	if m.GetCheckpointFunc != nil {
		return m.GetCheckpointFunc(ctx, in)
	}
	return nil, fmt.Errorf("GetCheckpoint not implemented")
}

// Helpers to setup test environment
func setupTestAdapter(t *testing.T, server *MockLedgerServer) *Adapter {
	// Start listener
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	v2.RegisterLedgerServiceServer(s, server)
	go func() { _ = s.Serve(lis) }()

	// Connect
	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		s.Stop()
	})

	mockRPC := &MockRPCClient{conn: conn}
	return NewAdapter("SUI_TEST", mockRPC)
}

func TestGetLatestBlock(t *testing.T) {
	checkpointHeight := uint64(100)

	mockServer := &MockLedgerServer{
		GetServiceInfoFunc: func(ctx context.Context, in *v2.GetServiceInfoRequest) (*v2.GetServiceInfoResponse, error) {
			return &v2.GetServiceInfoResponse{
				CheckpointHeight: &checkpointHeight,
			}, nil
		},
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest) (*v2.GetCheckpointResponse, error) {
			if in.GetSequenceNumber() != 100 {
				t.Errorf("Expected sequence number 100, got %d", in.GetSequenceNumber())
			}
			return &v2.GetCheckpointResponse{
				Checkpoint: &v2.Checkpoint{
					SequenceNumber: &checkpointHeight,
				},
			}, nil
		},
	}

	adapter := setupTestAdapter(t, mockServer)

	height, err := adapter.GetLatestBlock(context.Background())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if height != 100 {
		t.Errorf("Expected height 100, got %d", height)
	}
}

func TestGetBlock(t *testing.T) {
	seqNum := uint64(123)
	digest := "digest_123"
	prevDigest := "digest_122"
	timestamp := time.Now()

	mockServer := &MockLedgerServer{
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest) (*v2.GetCheckpointResponse, error) {
			if in.GetSequenceNumber() != 123 {
				t.Errorf("Expected sequence number 123, got %d", in.GetSequenceNumber())
			}
			return &v2.GetCheckpointResponse{
				Checkpoint: &v2.Checkpoint{
					SequenceNumber: &seqNum,
					Digest:         &digest,
					Summary: &v2.CheckpointSummary{
						PreviousDigest: &prevDigest,
						Timestamp:      timestamppb.New(timestamp),
					},
					Transactions: []*v2.ExecutedTransaction{
						{Digest: &digest}, // Dummy tx
					},
				},
			}, nil
		},
	}

	adapter := setupTestAdapter(t, mockServer)

	block, err := adapter.GetBlock(context.Background(), 123)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if block == nil {
		t.Fatal("Expected block, got nil")
	}
	if block.Number != seqNum {
		t.Errorf("Expected number %d, got %d", seqNum, block.Number)
	}
	if block.Hash != digest {
		t.Errorf("Expected hash %s, got %s", digest, block.Hash)
	}
	if block.ParentHash != prevDigest {
		t.Errorf("Expected parent hash %s, got %s", prevDigest, block.ParentHash)
	}

}

func TestGetBlock_NotFound(t *testing.T) {
	mockServer := &MockLedgerServer{
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest) (*v2.GetCheckpointResponse, error) {
			return nil, status.Error(codes.NotFound, "not found")
		},
	}

	adapter := setupTestAdapter(t, mockServer)

	block, err := adapter.GetBlock(context.Background(), 123)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if block != nil {
		t.Fatal("Expected nil block for NotFound error")
	}
}

func TestGetTransactions(t *testing.T) {
	seqNum := uint64(123)
	txDigest := "tx_digest_abc"
	sender := "sender_addr"
	success := true
	suiCoinType := "0x2::sui::SUI"
	amount := "1000"

	mockServer := &MockLedgerServer{
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest) (*v2.GetCheckpointResponse, error) {
			return &v2.GetCheckpointResponse{
				Checkpoint: &v2.Checkpoint{
					SequenceNumber: &seqNum,
					Transactions: []*v2.ExecutedTransaction{
						{
							Digest: &txDigest,
							Transaction: &v2.Transaction{
								Sender: &sender,
							},
							Effects: &v2.TransactionEffects{
								Status: &v2.ExecutionStatus{
									Success: &success,
								},
							},
							BalanceChanges: []*v2.BalanceChange{
								{
									Address:  &sender,
									CoinType: &suiCoinType,
									Amount:   &amount,
								},
							},
						},
					},
				},
			}, nil
		},
	}

	adapter := setupTestAdapter(t, mockServer)

	block := &domain.Block{Number: 123}
	txs, err := adapter.GetTransactions(context.Background(), block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("Expected 1 tx, got %d", len(txs))
	}
	if txs[0].Hash != txDigest {
		t.Errorf("Expected tx hash %s, got %s", txDigest, txs[0].Hash)
	}
	if txs[0].Type != domain.TxTypeNative {
		t.Errorf("Expected type native, got %s", txs[0].Type)
	}
	if txs[0].Value != "1000" {
		t.Errorf("Expected value 1000, got %s", txs[0].Value)
	}
}

func TestGetTransactions_Token(t *testing.T) {
	seqNum := uint64(123)
	txDigest := "tx_digest_token"
	sender := "sender_addr"
	success := true
	tokenCoinType := "0xabc::token::TOKEN"
	amount := "-500"

	mockServer := &MockLedgerServer{
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest) (*v2.GetCheckpointResponse, error) {
			return &v2.GetCheckpointResponse{
				Checkpoint: &v2.Checkpoint{
					SequenceNumber: &seqNum,
					Transactions: []*v2.ExecutedTransaction{
						{
							Digest: &txDigest,
							Transaction: &v2.Transaction{
								Sender: &sender,
							},
							Effects: &v2.TransactionEffects{
								Status: &v2.ExecutionStatus{
									Success: &success,
								},
							},
							BalanceChanges: []*v2.BalanceChange{
								{
									Address:  &sender,
									CoinType: &tokenCoinType,
									Amount:   &amount,
								},
							},
						},
					},
				},
			}, nil
		},
	}

	adapter := setupTestAdapter(t, mockServer)

	block := &domain.Block{Number: 123}
	txs, err := adapter.GetTransactions(context.Background(), block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if txs[0].Type != domain.TxTypeToken {
		t.Errorf("Expected type token, got %s", txs[0].Type)
	}
	if txs[0].TokenAddress != tokenCoinType {
		t.Errorf("Expected token addr %s, got %s", tokenCoinType, txs[0].TokenAddress)
	}
	if txs[0].TokenAmount != "500" {
		t.Errorf("Expected token amount 500, got %s", txs[0].TokenAmount)
	}
}
