package reorg

import (
	"context"
	"testing"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
)

type mockCursorManager struct {
	rollbackCalled bool
	setStateCalled bool
	lastState      cursor.State
}

func (m *mockCursorManager) Get(
	ctx context.Context,
	chainID domain.ChainID,
) (*domain.Cursor, error) {
	return nil, nil
}

func (m *mockCursorManager) Initialize(
	ctx context.Context,
	chainID domain.ChainID,
	startBlock uint64,
) (*domain.Cursor, error) {
	return nil, nil
}

func (m *mockCursorManager) Advance(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
	blockHash string,
) error {
	return nil
}

func (m *mockCursorManager) Jump(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
) error {
	return nil
}

func (m *mockCursorManager) SetState(
	ctx context.Context,
	chainID domain.ChainID,
	newState cursor.State,
	reason string,
) error {
	m.setStateCalled = true
	m.lastState = newState
	return nil
}

func (m *mockCursorManager) Rollback(
	ctx context.Context,
	chainID domain.ChainID,
	safeBlock uint64,
	safeHash string,
) error {
	m.rollbackCalled = true
	return nil
}

func (m *mockCursorManager) Pause(
	ctx context.Context,
	chainID domain.ChainID,
	reason string,
) error {
	return nil
}
func (m *mockCursorManager) Resume(ctx context.Context, chainID domain.ChainID) error { return nil }

func (m *mockCursorManager) GetLag(
	ctx context.Context,
	chainID domain.ChainID,
	latestBlock uint64,
) (int64, error) {
	return 0, nil
}

func (m *mockCursorManager) GetMetrics(
	chainID domain.ChainID,
) cursor.Metrics {
	return cursor.Metrics{}
}

func (m *mockCursorManager) SetStateChangeCallback(
	fn func(chainID domain.ChainID, t cursor.Transition),
) {
}

type mockTxRepo struct{}

func (r *mockTxRepo) Create(ctx context.Context, tx *domain.Transaction) error       { return nil }
func (r *mockTxRepo) Save(ctx context.Context, tx *domain.Transaction) error         { return nil }
func (r *mockTxRepo) SaveBatch(ctx context.Context, txs []*domain.Transaction) error { return nil }

func (r *mockTxRepo) GetByHash(
	ctx context.Context,
	chainID domain.ChainID,
	hash string,
) (*domain.Transaction, error) {
	return nil, nil
}

func (r *mockTxRepo) GetByBlock(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
) ([]*domain.Transaction, error) {
	return []*domain.Transaction{}, nil
}

func (r *mockTxRepo) UpdateStatus(
	ctx context.Context,
	chainID domain.ChainID,
	hash string,
	status domain.TxStatus,
) error {
	return nil
}

func (r *mockTxRepo) DeleteByBlock(
	ctx context.Context,
	chainID domain.ChainID,
	blockNumber uint64,
) error {
	return nil
}

func (r *mockTxRepo) DeleteTransactionsOlderThan(
	ctx context.Context,
	chainID domain.ChainID,
	timestamp uint64,
) error {
	return nil
}

func TestHandler_Rollback_ResetsState(t *testing.T) {
	mockCm := &mockCursorManager{}
	mockBlock := newMockBlockRepo() // reusing from reorg_test.go if in same package
	mockTx := &mockTxRepo{}

	handler := &Handler{
		blockRepo: mockBlock,
		txRepo:    mockTx,
		cursorMgr: mockCm,
	}

	ctx := context.Background()
	_, err := handler.Rollback(ctx, domain.ChainID("test_chain"), 100, 90, "safe_hash")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if !mockCm.rollbackCalled {
		t.Error("expected cursor.Rollback to be called")
	}

	if !mockCm.setStateCalled {
		t.Error("expected cursor.SetState to be called")
	}

	if mockCm.lastState != domain.CursorStateScanning {
		t.Errorf("expected final state to be Scanning, got %s", mockCm.lastState)
	}
}
