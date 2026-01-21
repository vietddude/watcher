package health

import (
	"context"
	"testing"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"github.com/vietddude/watcher/internal/infra/rpc/budget"
	"github.com/vietddude/watcher/internal/infra/storage"
)

type mockFetcher struct {
	height uint64
	err    error
}

func (m *mockFetcher) GetLatestHeight(ctx context.Context, chainID domain.ChainID) (uint64, error) {
	return m.height, m.err
}

// Stub Cursor Manager
type stubCursorMgr struct {
	lag int64
}

func (s *stubCursorMgr) GetLag(ctx context.Context, c domain.ChainID, l uint64) (int64, error) {
	return s.lag, nil
}

func (s *stubCursorMgr) Get(
	ctx context.Context,
	c domain.ChainID,
) (*domain.Cursor, error) {
	return nil, nil
}

func (s *stubCursorMgr) Initialize(
	ctx context.Context,
	c domain.ChainID,
	st uint64,
) (*domain.Cursor, error) {
	return nil, nil
}

func (s *stubCursorMgr) Advance(
	ctx context.Context,
	c domain.ChainID,
	b uint64,
	h string,
) error {
	return nil
}

func (s *stubCursorMgr) SetState(
	ctx context.Context,
	c domain.ChainID,
	st cursor.State,
	r string,
) error {
	return nil
}

func (s *stubCursorMgr) Jump(ctx context.Context, c domain.ChainID, b uint64) error {
	return nil
}

func (s *stubCursorMgr) Rollback(
	ctx context.Context,
	c domain.ChainID,
	b uint64,
	h string,
) error {
	return nil
}

func (s *stubCursorMgr) Pause(
	ctx context.Context,
	c domain.ChainID,
	r string,
) error {
	return nil
}

func (s *stubCursorMgr) Resume(
	ctx context.Context,
	c domain.ChainID,
) error {
	return nil
}
func (s *stubCursorMgr) SetMetadata(ctx context.Context, c domain.ChainID, k string, v any) error {
	return nil
}
func (s *stubCursorMgr) GetMetrics(c domain.ChainID) cursor.Metrics { return cursor.Metrics{} }
func (s *stubCursorMgr) SetStateChangeCallback(fn func(domain.ChainID, cursor.Transition)) {
}

// Stub Missing Repo
type stubMissingRepo struct {
	count int
}

func (s *stubMissingRepo) Count(
	ctx context.Context,
	c domain.ChainID,
) (int, error) {
	return s.count, nil
}
func (s *stubMissingRepo) Add(ctx context.Context, v *domain.MissingBlock) error { return nil }

func (s *stubMissingRepo) GetNext(
	ctx context.Context,
	c domain.ChainID,
) (*domain.MissingBlock, error) {
	return nil, nil
}
func (s *stubMissingRepo) MarkProcessing(ctx context.Context, id string) error { return nil }
func (s *stubMissingRepo) MarkCompleted(ctx context.Context, id string) error  { return nil }
func (s *stubMissingRepo) MarkFailed(ctx context.Context, id, msg string) error {
	return nil
}

func (s *stubMissingRepo) GetPending(
	ctx context.Context,
	c domain.ChainID,
) ([]*domain.MissingBlock, error) {
	return nil, nil
}

func (s *stubMissingRepo) FindGaps(
	ctx context.Context,
	c domain.ChainID,
	f, t uint64,
) ([]storage.Gap, error) {
	return nil, nil
}

// Stub Failed Repo
type stubFailedRepo struct {
	count int
}

func (s *stubFailedRepo) Count(
	ctx context.Context,
	c domain.ChainID,
) (int, error) {
	return s.count, nil
}
func (s *stubFailedRepo) Add(ctx context.Context, v *domain.FailedBlock) error { return nil }

func (s *stubFailedRepo) GetNext(
	ctx context.Context,
	c domain.ChainID,
) (*domain.FailedBlock, error) {
	return nil, nil
}
func (s *stubFailedRepo) IncrementRetry(ctx context.Context, id string) error { return nil }
func (s *stubFailedRepo) MarkResolved(ctx context.Context, id string) error   { return nil }

func (s *stubFailedRepo) GetAll(
	ctx context.Context,
	c domain.ChainID,
) ([]*domain.FailedBlock, error) {
	return nil, nil
}
func (s *stubFailedRepo) Delete(ctx context.Context, id string) error { return nil }

// Stub Budget Tracker
type stubBudgetTracker struct{}

func (s *stubBudgetTracker) RecordCall(providerName, method string) {}
func (s *stubBudgetTracker) GetProviderUsage(providerName string) budget.UsageStats {
	return budget.UsageStats{}
}
func (s *stubBudgetTracker) CanMakeCall(providerName string) bool               { return true }
func (s *stubBudgetTracker) GetThrottleDelay(providerName string) time.Duration { return 0 }
func (s *stubBudgetTracker) GetUsagePercent() float64                           { return 0 }
func (s *stubBudgetTracker) Reset()                                             {}

type stubRouter struct {
	rpc.Router
}

func (s *stubRouter) GetProvider(chainID domain.ChainID) (rpc.Provider, error) {
	return nil, nil
}

func TestMonitor_Healthy(t *testing.T) {
	monitor := NewMonitor(
		map[domain.ChainID][]string{domain.ChainID("ethereum"): {"ETH"}},
		&stubCursorMgr{lag: 5},
		&stubMissingRepo{count: 0},
		&stubFailedRepo{count: 0},
		&stubBudgetTracker{},
		&mockFetcher{height: 1000},
		nil,
		map[domain.ChainID]rpc.Router{domain.ChainID("ethereum"): &stubRouter{}},
	)

	report := monitor.CheckHealth(context.Background())
	health := report["ethereum"]

	if health.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", health.Status)
	}
}

func TestMonitor_Degraded(t *testing.T) {
	monitor := NewMonitor(
		map[domain.ChainID][]string{domain.ChainID("ethereum"): {"ETH"}},
		&stubCursorMgr{lag: 50},
		&stubMissingRepo{count: 0},
		&stubFailedRepo{count: 0},
		&stubBudgetTracker{},
		&mockFetcher{height: 1000},
		nil,
		map[domain.ChainID]rpc.Router{domain.ChainID("ethereum"): &stubRouter{}},
	)

	report := monitor.CheckHealth(context.Background())
	health := report["ethereum"]

	if health.Status != StatusDegraded {
		t.Errorf("expected degraded, got %s", health.Status)
	}
}

func TestMonitor_Critical(t *testing.T) {
	monitor := NewMonitor(
		map[domain.ChainID][]string{domain.ChainID("ethereum"): {"ETH"}},
		&stubCursorMgr{lag: 200},
		&stubMissingRepo{count: 0},
		&stubFailedRepo{count: 0},
		&stubBudgetTracker{},
		&mockFetcher{height: 1000},
		nil,
		map[domain.ChainID]rpc.Router{domain.ChainID("ethereum"): &stubRouter{}},
	)

	report := monitor.CheckHealth(context.Background())
	health := report["ethereum"]

	if health.Status != StatusCritical {
		t.Errorf("expected critical, got %s", health.Status)
	}
}
