package control

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vietddude/watcher/internal/core/config"
	"github.com/vietddude/watcher/internal/core/cursor"
	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/core/worker"
	"github.com/vietddude/watcher/internal/indexing/backfill"
	"github.com/vietddude/watcher/internal/indexing/emitter"
	"github.com/vietddude/watcher/internal/indexing/filter"
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
	pruners       map[string]*worker.Pruner
	healthMon     *health.Monitor
	healthServer  *health.Server
	store         *memory.MemoryStorage
	db            *postgres.DB
	redisClient   *redisclient.Client
	log           *slog.Logger
	coordinators  map[string]*rpc.Coordinator
	chainNames    map[string]string
}

// Config holds the application configuration.
type Config struct {
	Port                int
	Chains              []config.ChainConfig
	Backfill            backfill.ProcessorConfig
	Budget              rpc.BudgetConfig
	Reorg               reorg.Config
	Redis               redisclient.Config
	Database            postgres.Config // Add database config
	RescanRangesEnabled bool            // CLI flag
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
	var db *postgres.DB

	simpleFilter := filter.NewMemoryFilter()

	if cfg.Database.URL != "" {
		var err error
		db, err = postgres.NewDB(context.Background(), postgres.Config{
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
		if err := goose.Up(db.DB, "migrations"); err != nil {
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
	coordinators := make(map[string]*rpc.Coordinator)
	chainIDs := make([]string, 0, len(cfg.Chains))
	chainNames := make(map[string]string)
	chainProviders := make(map[string][]string)
	scanIntervals := make(map[string]time.Duration)

	// Initialize with known constants
	for id, name := range domain.ChainIDToName {
		chainNames[id] = name
	}

	for _, chainCfg := range cfg.Chains {
		chainID := chainCfg.ChainID
		if chainCfg.InternalCode != "" {
			chainNames[chainID] = chainCfg.InternalCode
			slog.Info("Mapping ChainID to Name", "chainID", chainID, "name", chainCfg.InternalCode)
		} else {
			slog.Warn("Chain config missing InternalCode", "chainID", chainID)
		}
		scanIntervals[chainID] = chainCfg.ScanInterval

		var providers []string
		for _, p := range chainCfg.Providers {
			providers = append(providers, p.Name)
		}
		chainProviders[chainID] = providers

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
		coordinatedProvider := rpc.NewCoordinatedProvider(
			chainID,
			chainCfg.InternalCode,
			coordinator,
		)
		coordinators[chainID] = coordinator

		// Use EVM adapter by default with CoordinatedProvider
		var adapter chain.Adapter
		if chainCfg.Type == "sui" {
			// Use the coordinated provider (which wraps generic providers)
			// The Sui Client now accepts a rpc.Provider
			adapter = sui.NewAdapter(chainID, coordinatedProvider)
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
				// Ensure ChainID is set
				block.ChainID = chainID
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
			ChainName:       chainCfg.InternalCode,
			ChainType:       chainCfg.Type,
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
		chainNames,
		chainProviders,
		cursorMgr,
		missingRepo,
		failedRepo,
		budgetTracker,
		fetcher,
		scanIntervals,
	)

	healthServer := health.NewServer(healthMon, cfg.Port)

	// 7. Initialize Redis and Rescan Workers
	var redisClient *redisclient.Client
	rescanWorkers := make(map[string]*rescan.Worker)
	pruners := make(map[string]*worker.Pruner)

	// Initialize Pruners
	for _, chainCfg := range cfg.Chains {
		if chainCfg.RetentionPeriod > 0 {
			pruner := worker.NewPruner(chainCfg, blockRepo, txRepo)
			pruners[chainCfg.ChainID] = pruner
		}
	}

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
		db:            db,
		redisClient:   redisClient,
		log:           slog.Default(),
		coordinators:  coordinators,
		chainNames:    chainNames,
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

	// Start Health Monitor Background Tasks
	go w.healthMon.Start(ctx)

	// Start DB Metrics Collector
	if w.db != nil {
		w.db.StartMetricsCollector(ctx)
	}

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
				name := w.chainNames[id]
				if name == "" {
					name = id
				}
				if err := processor.Run(ctx, id, name); err != nil {
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

	// Start Pruners
	for id, p := range w.pruners {
		w.log.Info("Starting pruner", "chain", id)
		go p.Start(ctx)
	}

	// Start RPC Metrics Updater
	go w.runMetricsUpdater(ctx)

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

func (w *Watcher) runMetricsUpdater(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for chainID, coord := range w.coordinators {
				name := w.chainNames[chainID]
				if name == "" {
					name = chainID
				}
				coord.UpdateMetrics(chainID, name)
				slog.Debug("Updating RPC metrics", "chainID", chainID, "label_name", name)
			}
		}
	}
}
