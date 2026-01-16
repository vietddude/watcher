package sui

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	v2 "github.com/vietddude/watcher/internal/infra/chain/sui/generated/sui/rpc/v2"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Manual mock implementation
type MockLedgerClient struct {
	GetServiceInfoFunc func(ctx context.Context, in *v2.GetServiceInfoRequest, opts ...grpc.CallOption) (*v2.GetServiceInfoResponse, error)
	GetCheckpointFunc  func(ctx context.Context, in *v2.GetCheckpointRequest, opts ...grpc.CallOption) (*v2.GetCheckpointResponse, error)
	GetTransactionFunc func(ctx context.Context, in *v2.GetTransactionRequest, opts ...grpc.CallOption) (*v2.GetTransactionResponse, error)
}

func (m *MockLedgerClient) GetServiceInfo(ctx context.Context, in *v2.GetServiceInfoRequest, opts ...grpc.CallOption) (*v2.GetServiceInfoResponse, error) {
	if m.GetServiceInfoFunc != nil {
		return m.GetServiceInfoFunc(ctx, in, opts...)
	}
	return nil, fmt.Errorf("GetServiceInfo not implemented in mock")
}

func (m *MockLedgerClient) GetCheckpoint(ctx context.Context, in *v2.GetCheckpointRequest, opts ...grpc.CallOption) (*v2.GetCheckpointResponse, error) {
	if m.GetCheckpointFunc != nil {
		return m.GetCheckpointFunc(ctx, in, opts...)
	}
	return nil, fmt.Errorf("GetCheckpoint not implemented in mock")
}

func (m *MockLedgerClient) GetTransaction(ctx context.Context, in *v2.GetTransactionRequest, opts ...grpc.CallOption) (*v2.GetTransactionResponse, error) {
	if m.GetTransactionFunc != nil {
		return m.GetTransactionFunc(ctx, in, opts...)
	}
	return nil, fmt.Errorf("GetTransaction not implemented in mock")
}

// Stubs for interface compliance
func (m *MockLedgerClient) GetObject(ctx context.Context, in *v2.GetObjectRequest, opts ...grpc.CallOption) (*v2.GetObjectResponse, error) {
	return nil, nil
}
func (m *MockLedgerClient) BatchGetObjects(ctx context.Context, in *v2.BatchGetObjectsRequest, opts ...grpc.CallOption) (*v2.BatchGetObjectsResponse, error) {
	return nil, nil
}
func (m *MockLedgerClient) BatchGetTransactions(ctx context.Context, in *v2.BatchGetTransactionsRequest, opts ...grpc.CallOption) (*v2.BatchGetTransactionsResponse, error) {
	return nil, nil
}
func (m *MockLedgerClient) GetEpoch(ctx context.Context, in *v2.GetEpochRequest, opts ...grpc.CallOption) (*v2.GetEpochResponse, error) {
	return nil, nil
}

func TestGetLatestBlock(t *testing.T) {
	checkpointHeight := uint64(100)

	mockClient := &MockLedgerClient{
		GetServiceInfoFunc: func(ctx context.Context, in *v2.GetServiceInfoRequest, opts ...grpc.CallOption) (*v2.GetServiceInfoResponse, error) {
			return &v2.GetServiceInfoResponse{
				CheckpointHeight: &checkpointHeight,
			}, nil
		},
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest, opts ...grpc.CallOption) (*v2.GetCheckpointResponse, error) {
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

	client := NewClientFromService(mockClient)
	adapter := NewAdapter("SUI_TEST", client)

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

	mockClient := &MockLedgerClient{
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest, opts ...grpc.CallOption) (*v2.GetCheckpointResponse, error) {
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

	client := NewClientFromService(mockClient)
	adapter := NewAdapter("SUI_TEST", client)

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
	if block.TxCount != 1 {
		t.Errorf("Expected 1 tx, got %d", block.TxCount)
	}
}

func TestGetTransactions(t *testing.T) {
	seqNum := uint64(123)
	txDigest := "tx_digest_abc"
	sender := "sender_addr"
	success := true

	mockClient := &MockLedgerClient{
		GetCheckpointFunc: func(ctx context.Context, in *v2.GetCheckpointRequest, opts ...grpc.CallOption) (*v2.GetCheckpointResponse, error) {
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
						},
					},
				},
			}, nil
		},
	}

	client := NewClientFromService(mockClient)
	adapter := NewAdapter("SUI_TEST", client)

	block := &domain.Block{Number: 123}
	txs, err := adapter.GetTransactions(context.Background(), block)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("Expected 1 tx, got %d", len(txs))
	}
	if txs[0].TxHash != txDigest {
		t.Errorf("Expected tx hash %s, got %s", txDigest, txs[0].TxHash)
	}
	if txs[0].From != sender {
		t.Errorf("Expected sender %s, got %s", sender, txs[0].From)
	}
	if txs[0].Status != domain.TxStatusSuccess {
		t.Errorf("Expected status success, got %v", txs[0].Status)
	}
}
