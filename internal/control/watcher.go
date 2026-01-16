package control

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/indexing/backfill"
	"github.com/vietddude/watcher/internal/indexing/emitter"
	"github.com/vietddude/watcher/internal/indexing/health"
	"github.com/vietddude/watcher/internal/indexing/indexer"
	"github.com/vietddude/watcher/internal/indexing/recovery"
	"github.com/vietddude/watcher/internal/indexing/reorg"
	"github.com/vietddude/watcher/internal/indexing/rescan"
	"github.com/vietddude/watcher/internal/infra/chain"
	"github.com/vietddude/watcher/internal/infra/chain/evm"
	"github.com/vietddude/watcher/internal/infra/chain/sui"
	redisclient "github.com/vietddude/watcher/internal/infra/redis"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"github.com/vietddude/watcher/internal/infra/storage"
	"github.com/vietddude/watcher/internal/infra/storage/memory"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"

	"github.com/pressly/goose/v3"
)

// Watcher is the main application struct that manages the indexer lifecycle.
type Watcher struct {
	cfg           Config
	indexers      map[string]indexer.Indexer
	backfillers   map[string]*backfill.Processor
	rescanWorkers map[string]*rescan.Worker
	healthMon     *health.Monitor
	healthServer  *health.Server
	store         *memory.MemoryStorage
	redisClient   *redisclient.Client
	log           *slog.Logger
}

// Config holds the application configuration.
type Config struct {
	Port                int
	Chains              []ChainConfig
	Backfill            backfill.ProcessorConfig
	Budget              rpc.BudgetConfig
	Reorg               reorg.Config
	Redis               redisclient.Config
	Database            postgres.Config // Add database config
	RescanRangesEnabled bool            // CLI flag
}

// ChainConfig holds configuration for a specific chain.
type ChainConfig struct {
	ChainID        string        `yaml:"id" mapstructure:"id"`
	Type           string        `yaml:"type" mapstructure:"type"` // "evm" or "sui"
	InternalCode   string        `yaml:"internal_code" mapstructure:"internal_code"`
	FinalityBlocks uint64        `yaml:"finality_blocks" mapstructure:"finality_blocks"`
	ScanInterval   time.Duration `yaml:"scan_interval" mapstructure:"scan_interval"`
	RescanRanges   bool          `yaml:"rescan_ranges" mapstructure:"rescan_ranges"` // Enable rescan worker
	Providers      []ProviderConfig
}

// ProviderConfig holds configuration for an RPC provider.
type ProviderConfig struct {
	Name string
	URL  string
}

// NewWatcher creates a new Watcher instance with all dependencies initialized.
func NewWatcher(cfg Config) (*Watcher, error) {

	// 1. Initialize Storage
	var blockRepo storage.BlockRepository
	var txRepo storage.TransactionRepository
	var cursorRepo storage.CursorRepository
	var missingRepo storage.MissingBlockRepository
	var failedRepo storage.FailedBlockRepository
	var store *memory.MemoryStorage // Only for cleanup if used

	simpleFilter := NewSimpleFilter()

	if cfg.Database.URL != "" {
		db, err := postgres.NewDB(context.Background(), postgres.Config{
			URL:      cfg.Database.URL,
			MaxConns: cfg.Database.MaxConns,
			MinConns: cfg.Database.MinConns,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to init db: %w", err)
		}

		// Run migrations
		// Note: Goose needs direct *sql.DB which sqlx.DB wraps
		if err := goose.SetDialect("postgres"); err != nil {
			return nil, err
		}
		// Assuming migrations are in "migrations" folder relative to CWD
		if err := goose.Up(db.DB.DB, "migrations"); err != nil {
			return nil, fmt.Errorf("failed to migrate db: %w", err)
		}

		blockRepo = postgres.NewBlockRepo(db)
		txRepo = postgres.NewTxRepo(db)
		cursorRepo = postgres.NewCursorRepo(db)
		missingRepo = postgres.NewMissingBlockRepo(db)
		failedRepo = postgres.NewFailedBlockRepo(db)

		// Initialize Wallet Repo and load filter
		walletRepo := postgres.NewWalletRepo(db)
		wallets, err := walletRepo.GetAll(context.Background())
		if err != nil {
			slog.Warn("Failed to load wallet addresses", "error", err)
		}

		// Populate filter
		for _, w := range wallets {
			simpleFilter.Add(w.Address)
		}
		slog.Info("Loaded wallet addresses into filter", "count", len(wallets))

		slog.Info("Using PostgreSQL storage")
	} else {
		store = memory.NewMemoryStorage()
		blockRepo = memory.NewBlockRepo(store)
		txRepo = memory.NewTxRepo(store)
		cursorRepo = memory.NewCursorRepo(store)
		missingRepo = memory.NewMissingRepo(store)
		failedRepo = memory.NewFailedRepo(store)

		// Empty filter for memory mode
		// simpleFilter initialized below
		slog.Info("Using Memory storage")
	}

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
	// Use config for budget quota if available, otherwise default
	quota := cfg.Budget.DailyQuota
	if quota == 0 {
		quota = 10000
	}
	budgetTracker := rpc.NewBudgetTracker(quota, allocation)

	fetcher := &MultiChainFetcher{adapters: make(map[string]chain.Adapter)}
	indexers := make(map[string]indexer.Indexer)
	backfillers := make(map[string]*backfill.Processor)
	chainIDs := make([]string, 0, len(cfg.Chains))

	for _, chainCfg := range cfg.Chains {
		chainID := chainCfg.ChainID

		// 3. Initialize RPC Router & Coordinator
		router := rpc.NewRouter(budgetTracker)

		for _, p := range chainCfg.Providers {
			// Check if this is a sui chain to use GRPC provider
			if chainCfg.Type == "sui" {
				// Use GRPC Provider
				grpcProvider, err := rpc.NewGRPCProvider(context.Background(), p.Name, p.URL)
				if err != nil {
					return nil, fmt.Errorf("failed to create grpc provider: %w", err)
				}
				router.AddProvider(chainID, grpcProvider)
			} else {
				// Use HTTP Provider (EVM default)
				rpcProvider := rpc.NewHTTPProvider(
					p.Name,
					p.URL,
					10*time.Second,
				)
				router.AddProvider(chainID, rpcProvider)
			}
		}

		coordinator := rpc.NewCoordinator(router, budgetTracker)
		coordinatedProvider := rpc.NewCoordinatedProvider(chainID, coordinator)

		// Use EVM adapter by default with CoordinatedProvider
		var adapter chain.Adapter
		if chainCfg.Type == "sui" {
			// Use the coordinated provider (which wraps generic providers)
			// The Sui Client now accepts a rpc.Provider
			client := sui.NewClient(coordinatedProvider)
			adapter = sui.NewAdapter(chainID, client)
		} else {
			adapter = evm.NewEVMAdapter(chainID, coordinatedProvider, chainCfg.FinalityBlocks)
		}
		fetcher.adapters[chainID] = adapter

		// 4. Initialize Components
		reorgDetector := reorg.NewDetector(cfg.Reorg, blockRepo)

		// Create Reorg Handler
		reorgHandler := reorg.NewHandler(blockRepo, txRepo, cursorMgr)

		// Create Recovery Handler (DefaultBackoff)
		recoveryHandler := recovery.NewHandler(failedRepo, nil, recovery.DefaultBackoff(nil))

		// Create Backfill Processor
		bfProcessor := backfill.NewProcessor(
			cfg.Backfill,
			missingRepo,
			func(chainID string, blockNum uint64) error {
				// Re-use adapter to fetch block
				block, err := adapter.GetBlock(context.Background(), blockNum)
				if err != nil {
					return err
				}
				if block == nil {
					return fmt.Errorf("block %d not found", blockNum)
				}
				return blockRepo.Save(context.Background(), block)
			},
		)
		bfProcessor.SetBudgetTracker(budgetTracker)
		backfillers[chainID] = bfProcessor

		// Create Emitter
		baseEmitter := &LogEmitter{}
		finalityEmitter := emitter.NewFinalityBuffer(baseEmitter, chainCfg.FinalityBlocks)

		// 5. Create Indexer Pipeline
		idxCfg := indexer.Config{
			ChainID:         chainID,
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
		indexers[chainID] = pipeline
		chainIDs = append(chainIDs, chainID)
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

	healthServer := health.NewServer(healthMon, cfg.Port)

	// 7. Initialize Redis and Rescan Workers
	var redisClient *redisclient.Client
	rescanWorkers := make(map[string]*rescan.Worker)

	if cfg.Redis.URL != "" && cfg.RescanRangesEnabled {
		var err error
		redisClient, err = redisclient.NewClient(cfg.Redis)
		if err != nil {
			slog.Warn("Failed to connect to Redis, rescan disabled", "error", err)
		} else {
			// Initialize rescan workers for chains with rescan_ranges enabled
			for _, chainCfg := range cfg.Chains {
				if chainCfg.RescanRanges && chainCfg.InternalCode != "" {
					worker := rescan.NewWorker(
						rescan.DefaultConfig(),
						chainCfg.InternalCode,
						redisClient,
						fetcher.adapters[chainCfg.ChainID],
						blockRepo,
						txRepo,
					)
					rescanWorkers[chainCfg.InternalCode] = worker
					slog.Info("Rescan worker initialized", "chain", chainCfg.InternalCode)
				}
			}
		}
	}

	return &Watcher{
		cfg:           cfg,
		indexers:      indexers,
		backfillers:   backfillers,
		rescanWorkers: rescanWorkers,
		healthMon:     healthMon,
		healthServer:  healthServer,
		store:         store,
		redisClient:   redisClient,
		log:           slog.Default(),
	}, nil
}

// Start starts the watcher and all its components.
func (w *Watcher) Start(ctx context.Context) error {
	// Start Health Server
	go func() {
		if err := w.healthServer.Start(); err != nil {
			w.log.Error("Health server failed", "error", err)
		}
	}()

	// Start Indexers and Backfillers
	for id, idx := range w.indexers {
		w.log.Info("Starting indexer", "chain", id)
		go func(id string, i indexer.Indexer) {
			if err := i.Start(ctx); err != nil {
				w.log.Error("Indexer failed", "chain", id, "error", err)
			}
		}(id, idx)

		// Start Backfiller
		if bf, ok := w.backfillers[id]; ok {
			w.log.Info("Starting backfill processor", "chain", id)
			go func(id string, processor *backfill.Processor) {
				if err := processor.Run(ctx, id); err != nil {
					w.log.Error("Backfill processor failed", "chain", id, "error", err)
				}
			}(id, bf)
		}
	}

	// Start Rescan Workers
	for code, worker := range w.rescanWorkers {
		w.log.Info("Starting rescan worker", "chain", code)
		go func(code string, wrk *rescan.Worker) {
			if err := wrk.Run(ctx); err != nil {
				w.log.Error("Rescan worker failed", "chain", code, "error", err)
			}
		}(code, worker)
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

	// Close Redis
	if w.redisClient != nil {
		if err := w.redisClient.Close(); err != nil {
			w.log.Warn("Failed to close Redis", "error", err)
		}
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
func (f *SimpleFilter) Addresses() []string {
	addrs := make([]string, 0, len(f.addresses))
	for a := range f.addresses {
		addrs = append(addrs, a)
	}
	return addrs
}
