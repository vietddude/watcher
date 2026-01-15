package control

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/emitter"
	"github.com/vietddude/watcher/internal/indexing/health"
	"github.com/vietddude/watcher/internal/indexing/indexer"
	"github.com/vietddude/watcher/internal/indexing/recovery"
	"github.com/vietddude/watcher/internal/indexing/reorg"
	"github.com/vietddude/watcher/internal/infra/chain"
	"github.com/vietddude/watcher/internal/infra/chain/evm"
	"github.com/vietddude/watcher/internal/infra/rpc/budget"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"github.com/vietddude/watcher/internal/infra/storage/memory"
)

// Watcher is the main application struct that manages the indexer lifecycle.
type Watcher struct {
	cfg          Config
	indexers     map[string]indexer.Indexer
	healthMon    *health.Monitor
	healthServer *health.Server
	store        *memory.MemoryStorage
	log          *slog.Logger
}

// Config holds the application configuration.
type Config struct {
	ServerPort int
	Chains     []ChainConfig
}

// ChainConfig holds configuration for a specific chain.
type ChainConfig struct {
	ChainID        string
	RPCURL         string
	FinalityBlocks uint64
	ScanInterval   time.Duration
}

// NewWatcher creates a new Watcher instance with all dependencies initialized.
func NewWatcher(cfg Config) (*Watcher, error) {
	// 1. Initialize Storage (In-Memory for now)
	store := memory.NewMemoryStorage()
	blockRepo := memory.NewBlockRepo(store)
	txRepo := memory.NewTxRepo(store)
	cursorRepo := memory.NewCursorRepo(store)
	missingRepo := memory.NewMissingRepo(store)
	failedRepo := memory.NewFailedRepo(store)

	// 2. Initialize Shared Components
	cursorMgr := cursor.NewManager(cursorRepo)

	// Default allocation for all chains
	allocation := make(map[string]float64)
	for _, c := range cfg.Chains {
		allocation[c.ChainID] = 1.0 / float64(len(cfg.Chains))
	}
	if len(cfg.Chains) == 0 {
		allocation["default"] = 1.0
	}
	budgetTracker := budget.NewBudgetTracker(10000, allocation)

	fetcher := &MultiChainFetcher{adapters: make(map[string]chain.Adapter)}
	indexers := make(map[string]indexer.Indexer)
	chainIDs := make([]string, 0, len(cfg.Chains))

	for _, chainCfg := range cfg.Chains {
		// 3. Initialize RPC & Adapters
		rpcProvider := provider.NewHTTPProvider(
			chainCfg.ChainID, // Use ChainID as provider name
			chainCfg.RPCURL,
			10*time.Second,
		)

		// Use EVM adapter by default
		adapter := evm.NewEVMAdapter(chainCfg.ChainID, rpcProvider, chainCfg.FinalityBlocks)
		fetcher.adapters[chainCfg.ChainID] = adapter

		// 4. Initialize Components
		reorgDetector := reorg.NewDetector(blockRepo)

		// Create Reorg Handler
		reorgHandler := reorg.NewHandler(blockRepo, txRepo, cursorMgr)

		// Create Recovery Handler (DefaultBackoff)
		recoveryHandler := recovery.NewHandler(failedRepo, nil, recovery.DefaultBackoff(nil))

		// Create Emitter
		baseEmitter := &LogEmitter{}
		finalityEmitter := emitter.NewFinalityBuffer(baseEmitter, chainCfg.FinalityBlocks)

		// Create Filter (Simple)
		simpleFilter := NewSimpleFilter()

		// 5. Create Indexer Pipeline
		idxCfg := indexer.Config{
			ChainID:         chainCfg.ChainID,
			ChainAdapter:    adapter,
			Cursor:          cursorMgr,
			Reorg:           reorgDetector,
			ReorgHandler:    reorgHandler,
			Recovery:        recoveryHandler,
			Emitter:         finalityEmitter,
			Filter:          simpleFilter,
			BlockRepo:       blockRepo,
			TransactionRepo: txRepo,
			ScanInterval:    chainCfg.ScanInterval,
			BatchSize:       10,
		}

		pipeline := indexer.NewPipeline(idxCfg)
		indexers[chainCfg.ChainID] = pipeline
		chainIDs = append(chainIDs, chainCfg.ChainID)
	}

	// 6. Initialize Health Monitor
	healthMon := health.NewMonitor(
		chainIDs,
		cursorMgr,
		missingRepo,
		failedRepo,
		budgetTracker,
		fetcher,
	)

	healthServer := health.NewServer(healthMon, cfg.ServerPort)

	return &Watcher{
		cfg:          cfg,
		indexers:     indexers,
		healthMon:    healthMon,
		healthServer: healthServer,
		store:        store,
		log:          slog.Default(),
	}, nil
}

// Start starts the watcher and all its components.
func (w *Watcher) Start(ctx context.Context) error {
	w.log.Info("Starting Watcher...", "port", w.cfg.ServerPort)

	// Start Health Server
	go func() {
		if err := w.healthServer.Start(); err != nil {
			w.log.Error("Health server failed", "error", err)
		}
	}()

	// Start Indexers
	for id, idx := range w.indexers {
		w.log.Info("Starting indexer", "chain", id)
		go func(id string, i indexer.Indexer) {
			if err := i.Start(ctx); err != nil {
				w.log.Error("Indexer failed", "chain", id, "error", err)
			}
		}(id, idx)
	}

	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop(ctx context.Context) error {
	w.log.Info("Stopping Watcher...")

	// Stop Indexers
	for _, idx := range w.indexers {
		idx.Stop()
	}

	// Stop Health Server
	return w.healthServer.Stop(ctx)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// MultiChainFetcher adapts chain adapters to BlockHeightFetcher interface
type MultiChainFetcher struct {
	adapters map[string]chain.Adapter
}

func (f *MultiChainFetcher) GetLatestHeight(ctx context.Context, chainID string) (uint64, error) {
	if adapter, ok := f.adapters[chainID]; ok {
		return adapter.GetLatestBlock(ctx)
	}
	return 0, fmt.Errorf("chain not found: %s", chainID)
}

// LogEmitter implements Emitter interface for stdout logging
type LogEmitter struct{}

func (e *LogEmitter) Emit(ctx context.Context, event *domain.Event) error {
	fmt.Printf("[EVENT] %s: %s (%s)\n", event.ChainID, event.EventType, event.Transaction.TxHash)
	return nil
}
func (e *LogEmitter) EmitBatch(ctx context.Context, events []*domain.Event) error {
	for _, ev := range events {
		e.Emit(ctx, ev)
	}
	return nil
}
func (e *LogEmitter) EmitRevert(ctx context.Context, event *domain.Event, reason string) error {
	fmt.Printf("[REVERT] %s: %s\n", event.Transaction.TxHash, reason)
	return nil
}
func (e *LogEmitter) Close() error { return nil }

// SimpleFilter implements Filter interface
type SimpleFilter struct {
	addresses map[string]bool
}

func NewSimpleFilter() *SimpleFilter              { return &SimpleFilter{addresses: make(map[string]bool)} }
func (f *SimpleFilter) Contains(addr string) bool { return f.addresses[addr] }
func (f *SimpleFilter) Add(addr string) error     { f.addresses[addr] = true; return nil }
func (f *SimpleFilter) AddBatch(addrs []string) error {
	for _, a := range addrs {
		f.addresses[a] = true
	}
	return nil
}
func (f *SimpleFilter) Remove(addr string) error          { delete(f.addresses, addr); return nil }
func (f *SimpleFilter) Size() int                         { return len(f.addresses) }
func (f *SimpleFilter) Rebuild(ctx context.Context) error { return nil }
