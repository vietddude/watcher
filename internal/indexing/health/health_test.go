package health

import (
	"context"
	"testing"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/storage"
)

// =============================================================================
// Mocks
// =============================================================================

type mockFetcher struct {
	height uint64
	err    error
}

func (m *mockFetcher) GetLatestHeight(ctx context.Context, chainID string) (uint64, error) {
	return m.height, m.err
}

// Stub Cursor Manager
type stubCursorMgr struct {
	lag int64
}

func (s *stubCursorMgr) GetLag(ctx context.Context, c string, l uint64) (int64, error) {
	return s.lag, nil
}
func (s *stubCursorMgr) Get(ctx context.Context, c string) (*domain.Cursor, error) { return nil, nil }
func (s *stubCursorMgr) Initialize(ctx context.Context, c string, st uint64) (*domain.Cursor, error) {
	return nil, nil
}
func (s *stubCursorMgr) Advance(ctx context.Context, c string, b uint64, h string) error { return nil }
func (s *stubCursorMgr) SetState(ctx context.Context, c string, st cursor.State, r string) error {
	return nil
}
func (s *stubCursorMgr) Rollback(ctx context.Context, c string, b uint64, h string) error { return nil }
func (s *stubCursorMgr) Pause(ctx context.Context, c string, r string) error              { return nil }
func (s *stubCursorMgr) Resume(ctx context.Context, c string) error                       { return nil }
func (s *stubCursorMgr) SetMetadata(ctx context.Context, c, k string, v any) error {
	return nil
}
func (s *stubCursorMgr) GetMetrics(c string) cursor.Metrics { return cursor.Metrics{} }
func (s *stubCursorMgr) SetStateChangeCallback(fn func(string, cursor.Transition)) {
}

// Stub Missing Repo
type stubMissingRepo struct {
	count int
}

func (s *stubMissingRepo) Count(ctx context.Context, c string) (int, error)      { return s.count, nil }
func (s *stubMissingRepo) Add(ctx context.Context, v *domain.MissingBlock) error { return nil }
func (s *stubMissingRepo) GetNext(ctx context.Context, c string) (*domain.MissingBlock, error) {
	return nil, nil
}
func (s *stubMissingRepo) MarkProcessing(ctx context.Context, id string) error { return nil }
func (s *stubMissingRepo) MarkCompleted(ctx context.Context, id string) error  { return nil }
func (s *stubMissingRepo) MarkFailed(ctx context.Context, id, msg string) error {
	return nil
}
func (s *stubMissingRepo) GetPending(ctx context.Context, c string) ([]*domain.MissingBlock, error) {
	return nil, nil
}
func (s *stubMissingRepo) FindGaps(ctx context.Context, c string, f, t uint64) ([]storage.Gap, error) {
	return nil, nil
}

// Stub Failed Repo
type stubFailedRepo struct {
	count int
}

func (s *stubFailedRepo) Count(ctx context.Context, c string) (int, error)     { return s.count, nil }
func (s *stubFailedRepo) Add(ctx context.Context, v *domain.FailedBlock) error { return nil }
func (s *stubFailedRepo) GetNext(ctx context.Context, c string) (*domain.FailedBlock, error) {
	return nil, nil
}
func (s *stubFailedRepo) IncrementRetry(ctx context.Context, id string) error { return nil }
func (s *stubFailedRepo) MarkResolved(ctx context.Context, id string) error   { return nil }
func (s *stubFailedRepo) GetAll(ctx context.Context, c string) ([]*domain.FailedBlock, error) {
	return nil, nil
}
func (s *stubFailedRepo) Delete(ctx context.Context, id string) error { return nil }

// =============================================================================
// Tests
// =============================================================================

func TestMonitor_Healthy(t *testing.T) {
	monitor := NewMonitor(
		[]string{"ethereum"},
		&stubCursorMgr{lag: 5},
		&stubMissingRepo{count: 0},
		&stubFailedRepo{count: 0},
		nil,
		&mockFetcher{height: 1000},
	)

	report := monitor.CheckHealth(context.Background())
	health := report["ethereum"]

	if health.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", health.Status)
	}
}

func TestMonitor_Degraded(t *testing.T) {
	monitor := NewMonitor(
		[]string{"ethereum"},
		&stubCursorMgr{lag: 50},
		&stubMissingRepo{count: 0},
		&stubFailedRepo{count: 0},
		nil,
		&mockFetcher{height: 1000},
	)

	report := monitor.CheckHealth(context.Background())
	health := report["ethereum"]

	if health.Status != StatusDegraded {
		t.Errorf("expected degraded, got %s", health.Status)
	}
}

func TestMonitor_Critical(t *testing.T) {
	monitor := NewMonitor(
		[]string{"ethereum"},
		&stubCursorMgr{lag: 200},
		&stubMissingRepo{count: 0},
		&stubFailedRepo{count: 0},
		nil,
		&mockFetcher{height: 1000},
	)

	report := monitor.CheckHealth(context.Background())
	health := report["ethereum"]

	if health.Status != StatusCritical {
		t.Errorf("expected critical, got %s", health.Status)
	}
}
