package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vietddude/watcher/internal/core/domain"
)

type QueueRepo struct {
	db *sql.DB
}

func NewQueueRepo(db *PostgresDB) *QueueRepo {
	return &QueueRepo{db: db.DB}
}

// MissingBlockRepository implementation

func (r *QueueRepo) AddMissing(ctx context.Context, m *domain.MissingBlock) error {
	query := `
		INSERT INTO missing_blocks (chain_id, from_block, to_block, status, retry_attempts, created_at)
		VALUES ($1, $2, $3, 'pending', 0, NOW())
	`
	_, err := r.db.ExecContext(ctx, query, m.ChainID, m.FromBlock, m.ToBlock)
	return err
}

func (r *QueueRepo) GetNextMissing(ctx context.Context, chainID string) (*domain.MissingBlock, error) {
	// Simple queue logic: find oldest pending or retriable processing item
	// For now, just pending
	query := `
		SELECT id, chain_id, from_block, to_block, status, retry_attempts 
		FROM missing_blocks 
		WHERE chain_id = $1 AND status = 'pending' 
		ORDER BY created_at ASC LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, query, chainID)

	var m domain.MissingBlock
	var id int
	err := row.Scan(&id, &m.ChainID, &m.FromBlock, &m.ToBlock, &m.Status, &m.RetryCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.ID = fmt.Sprintf("%d", id)
	return &m, nil
}

func (r *QueueRepo) MarkMissingProcessing(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE missing_blocks SET status = 'processing', updated_at = NOW() WHERE id = $1", id)
	return err
}

func (r *QueueRepo) MarkMissingCompleted(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE missing_blocks SET status = 'completed', updated_at = NOW() WHERE id = $1", id)
	return err
}

func (r *QueueRepo) MarkMissingFailed(ctx context.Context, id string, msg string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE missing_blocks SET status = 'failed', last_error=$2, updated_at = NOW() WHERE id = $1", id, msg)
	return err
}

// Interfaces expect specific methods. Usually these would satisfy multiple repo interfaces.
// We can rename them to match the interface or use a struct adapter.
// The task plan said `queue_repo.go` implements BOTH.
// `MissingBlockRepository` methods: Add, GetNext, MarkProcessing, MarkCompleted, MarkFailed, GetPending, Count.
// I named them slightly differently above (AddMissing). I should check interface.
// Interface: Add(ctx, *domain.MissingBlock) error...
// I will just implement the interface methods directly on QueueRepo.
// But Wait, `FailedBlockRepository` has SAME method names like `Add`.
// Collision!
// Can't implement both interfaces on SAME struct if they have colliding method names but different args.
// Interfaces:
// Missing: Add(..., *MissingBlock) error
// Failed: Add(..., *FailedBlock) error
// Go doesn't support overloading.
// I must separate them into two structs or use adapter pattern.
// I'll split into `MissingBlockRepo` and `FailedBlockRepo` structs in same file.

type MissingBlockRepo struct {
	db *sql.DB
}

func NewMissingBlockRepo(db *PostgresDB) *MissingBlockRepo { return &MissingBlockRepo{db: db.DB} }

func (r *MissingBlockRepo) Add(ctx context.Context, m *domain.MissingBlock) error {
	query := `INSERT INTO missing_blocks (chain_id, from_block, to_block, status) VALUES ($1, $2, $3, $4)`
	_, err := r.db.ExecContext(ctx, query, m.ChainID, m.FromBlock, m.ToBlock, m.Status)
	return err
}
func (r *MissingBlockRepo) GetNext(ctx context.Context, chainID string) (*domain.MissingBlock, error) {
	query := `SELECT id, chain_id, from_block, to_block, status, retry_attempts FROM missing_blocks WHERE chain_id=$1 AND status='pending' ORDER BY created_at ASC LIMIT 1`
	var m domain.MissingBlock
	var id int
	err := r.db.QueryRowContext(ctx, query, chainID).Scan(&id, &m.ChainID, &m.FromBlock, &m.ToBlock, &m.Status, &m.RetryCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.ID = fmt.Sprintf("%d", id)
	return &m, nil
}
func (r *MissingBlockRepo) MarkProcessing(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE missing_blocks SET status='processing' WHERE id=$1", id)
	return err
}
func (r *MissingBlockRepo) MarkCompleted(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE missing_blocks SET status='completed' WHERE id=$1", id)
	return err
}
func (r *MissingBlockRepo) MarkFailed(ctx context.Context, id, msg string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE missing_blocks SET status='failed', last_error=$2 WHERE id=$1", id, msg)
	return err
}
func (r *MissingBlockRepo) GetPending(ctx context.Context, chainID string) ([]*domain.MissingBlock, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, from_block, to_block FROM missing_blocks WHERE chain_id=$1 AND status='pending'", chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*domain.MissingBlock
	for rows.Next() {
		var m domain.MissingBlock
		var id int
		if err := rows.Scan(&id, &m.FromBlock, &m.ToBlock); err != nil {
			return nil, err
		}
		m.ID = fmt.Sprintf("%d", id)
		m.ChainID = chainID
		res = append(res, &m)
	}
	return res, nil
}
func (r *MissingBlockRepo) Count(ctx context.Context, chainID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM missing_blocks WHERE chain_id=$1 AND status='pending'", chainID).Scan(&count)
	return count, err
}

type FailedBlockRepo struct {
	db *sql.DB
}

func NewFailedBlockRepo(db *PostgresDB) *FailedBlockRepo { return &FailedBlockRepo{db: db.DB} }

func (r *FailedBlockRepo) Add(ctx context.Context, f *domain.FailedBlock) error {
	query := `INSERT INTO failed_blocks (chain_id, block_number, error, retry_count, status) VALUES ($1, $2, $3, $4, $5)`
	_, err := r.db.ExecContext(ctx, query, f.ChainID, f.BlockNumber, f.Error, f.RetryCount, f.Status)
	return err
}
func (r *FailedBlockRepo) GetNext(ctx context.Context, chainID string) (*domain.FailedBlock, error) {
	query := `SELECT id, chain_id, block_number, error, retry_count, status FROM failed_blocks WHERE chain_id=$1 AND status='pending' ORDER BY created_at ASC LIMIT 1`
	var f domain.FailedBlock
	var id int
	err := r.db.QueryRowContext(ctx, query, chainID).Scan(&id, &f.ChainID, &f.BlockNumber, &f.Error, &f.RetryCount, &f.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.ID = fmt.Sprintf("%d", id)
	return &f, nil
}
func (r *FailedBlockRepo) IncrementRetry(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE failed_blocks SET retry_count=retry_count+1 WHERE id=$1", id)
	return err
}
func (r *FailedBlockRepo) MarkResolved(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "UPDATE failed_blocks SET status='resolved' WHERE id=$1", id)
	return err
}
func (r *FailedBlockRepo) GetAll(ctx context.Context, chainID string) ([]*domain.FailedBlock, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id, block_number, error FROM failed_blocks WHERE chain_id=$1", chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*domain.FailedBlock
	for rows.Next() {
		var f domain.FailedBlock
		var id int
		if err := rows.Scan(&id, &f.BlockNumber, &f.Error); err != nil {
			return nil, err
		}
		f.ID = fmt.Sprintf("%d", id)
		f.ChainID = chainID
		res = append(res, &f)
	}
	return res, nil
}
func (r *FailedBlockRepo) Count(ctx context.Context, chainID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM failed_blocks WHERE chain_id=$1 AND status='pending'", chainID).Scan(&count)
	return count, err
}
