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
	"github.com/vietddude/watcher/internal/indexing/throttle"
	"github.com/vietddude/watcher/internal/infra/chain"
	"github.com/vietddude/watcher/internal/infra/chain/bitcoin"
	"github.com/vietddude/watcher/internal/infra/chain/evm"
	"github.com/vietddude/watcher/internal/infra/chain/sui"
	redisclient "github.com/vietddude/watcher/internal/infra/redis"
	"github.com/vietddude/watcher/internal/infra/rpc"
	"github.com/vietddude/watcher/internal/infra/storage"
	"github.com/vietddude/watcher/internal/infra/storage/memory"
	"github.com/vietddude/watcher/internal/infra/storage/postgres"
)

// Watcher is the main application struct that manages the indexer lifecycle.
type Watcher struct {
	cfg           Config
	indexers      map[domain.ChainID]indexer.Indexer
	backfillers   map[domain.ChainID]*backfill.Processor
	rescanWorkers map[domain.ChainID]*rescan.Worker
	pruners       map[domain.ChainID]*worker.Pruner
	healthMon     *health.Monitor
	healthServer  *health.Server
	store         *memory.MemoryStorage
	db            *postgres.DB
	redisClient   *redisclient.Client
	log           *slog.Logger
	coordinators  map[domain.ChainID]*rpc.Coordinator
}

// Config holds the application configuration.
type Config struct {
	Port                int
	Chains              []config.ChainConfig
	Backfill            backfill.ProcessorConfig
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

		slog.Info("Using Memory storage")
	}

	// Build provider quota map from config (provider name -> daily quota)
	budgetTracker := rpc.NewBudgetTracker()
	for _, c := range cfg.Chains {
		for _, p := range c.Providers {
			budgetTracker.SetProviderQuota(p.Name, p.DailyQuota)
			if p.IntervalLimit > 0 && p.IntervalDuration != "" {
				dur, err := time.ParseDuration(p.IntervalDuration)
				if err == nil {
					budgetTracker.SetProviderLimit(p.Name, p.IntervalLimit, dur)
				} else {
					slog.Warn("Invalid interval_duration for provider", "provider", p.Name, "duration", p.IntervalDuration, "error", err)
				}
			}
		}
	}

	// Initialize Cursor Manager
	cursorMgr := cursor.NewManager(cursorRepo)

	fetcher := &MultiChainFetcher{adapters: make(map[domain.ChainID]chain.Adapter)}
	indexers := make(map[domain.ChainID]indexer.Indexer)
	backfillers := make(map[domain.ChainID]*backfill.Processor)
	coordinators := make(map[domain.ChainID]*rpc.Coordinator)
	chainIDs := make([]domain.ChainID, 0, len(cfg.Chains))
	chainProviders := make(map[domain.ChainID][]string)
	scanIntervals := make(map[domain.ChainID]time.Duration)
	routers := make(map[domain.ChainID]rpc.Router)

	for _, chainCfg := range cfg.Chains {
		chainID := chainCfg.ChainID
		scanIntervals[chainID] = chainCfg.ScanInterval

		var providers []string
		for _, p := range chainCfg.Providers {
			providers = append(providers, p.Name)
		}
		chainProviders[chainID] = providers

		// 3. Initialize RPC Router & Coordinator
		router := rpc.NewRouter(budgetTracker)

		hasActiveProvider := false
		for _, p := range chainCfg.Providers {
			switch chainCfg.Type {
			case domain.ChainTypeSui:
				// Use GRPC Provider
				grpcProvider, err := rpc.NewGRPCProvider(context.Background(), p.Name, p.URL)
				if err != nil {
					slog.Error(
						"Failed to initialize GRPC provider",
						"chain",
						chainID,
						"provider",
						p.Name,
						"error",
						err,
					)
					continue
				}
				router.AddProvider(chainID, grpcProvider)
				hasActiveProvider = true
			default:
				// Use HTTP Provider (EVM default)
				rpcProvider := rpc.NewHTTPProvider(
					p.Name,
					p.URL,
					5*time.Second, // Reduced timeout for faster failover
				)
				router.AddProvider(chainID, rpcProvider)
				hasActiveProvider = true
			}
		}

		if !hasActiveProvider {
			return nil, fmt.Errorf("no functional providers found for chain %s", chainID)
		}

		router.SetRotationStrategy(rpc.RotationProactive)
		routers[chainID] = router

		coordinator := rpc.NewCoordinator(router, budgetTracker)
		coordinatedProvider := rpc.NewCoordinatedProvider(
			chainID,
			coordinator,
		)
		coordinators[chainID] = coordinator

		var adapter chain.Adapter
		switch chainCfg.Type {
		case domain.ChainTypeSui:
			// Use the coordinated provider (which wraps generic providers)
			// The Sui Client now accepts a rpc.Provider
			adapter = sui.NewAdapter(chainID, coordinatedProvider)
		case domain.ChainTypeBitcoin:
			adapter = bitcoin.NewBitcoinAdapter(chainID, coordinatedProvider, chainCfg.FinalityBlocks)
		default:
			// Use EVM adapter by default
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
			func(chainID domain.ChainID, blockNum uint64) error {
				// Re-use adapter to fetch block
				ctx := context.Background()
				block, err := adapter.GetBlock(ctx, blockNum)
				if err != nil {
					return err
				}
				if block == nil {
					return fmt.Errorf("block %d not found", blockNum)
				}
				// Ensure ChainID is set
				block.ChainID = domain.ChainID(chainID)
				if err := blockRepo.Save(ctx, block); err != nil {
					return err
				}

				// Also fetch and save relevant transactions for backfilled blocks
				txs, err := adapter.GetTransactions(ctx, block)
				if err != nil {
					slog.Warn("Failed to fetch transactions for backfill block", "chain", chainID, "block", blockNum, "error", err)
					return nil // Don't fail backfill just because tx fetch failed
				}

				var relevantTxs []*domain.Transaction
				for _, tx := range txs {
					if simpleFilter.Contains(tx.From) || simpleFilter.Contains(tx.To) {
						tx.ChainID = block.ChainID
						relevantTxs = append(relevantTxs, tx)
					}
				}

				if len(relevantTxs) > 0 {
					if err := txRepo.SaveBatch(ctx, relevantTxs); err != nil {
						slog.Warn("Failed to save transactions for backfill block", "chain", chainID, "block", blockNum, "error", err)
					}
				}

				return nil
			},
		)
		bfProcessor.SetBudgetTracker(budgetTracker)
		backfillers[chainID] = bfProcessor

		// Create Emitter
		baseEmitter := &emitter.LogEmitter{}
		finalityEmitter := emitter.NewFinalityBuffer(baseEmitter, chainCfg.FinalityBlocks)

		// Initialize Adaptive Throttling (if configured)
		var controller *throttle.AdaptiveController
		var headCache *throttle.HeadCache
		if chainCfg.AdaptiveThrottling != nil && chainCfg.AdaptiveThrottling.Enabled {
			// Build adaptive config from chain config
			adaptiveCfg := throttle.DefaultConfig()
			if chainCfg.AdaptiveThrottling.MinScanInterval > 0 {
				adaptiveCfg.MinScanInterval = chainCfg.AdaptiveThrottling.MinScanInterval
			}
			if chainCfg.AdaptiveThrottling.MaxScanInterval > 0 {
				adaptiveCfg.MaxScanInterval = chainCfg.AdaptiveThrottling.MaxScanInterval
			}
			if chainCfg.AdaptiveThrottling.HeadCacheTTL > 0 {
				adaptiveCfg.HeadCacheTTL = chainCfg.AdaptiveThrottling.HeadCacheTTL
			}
			if chainCfg.AdaptiveThrottling.MaxBatchSize > 0 {
				adaptiveCfg.MaxBatchSize = chainCfg.AdaptiveThrottling.MaxBatchSize
			}
			if chainCfg.AdaptiveThrottling.LagBurstThreshold > 0 {
				adaptiveCfg.LagBurstThreshold = chainCfg.AdaptiveThrottling.LagBurstThreshold
			}
			adaptiveCfg.BatchEnabled = chainCfg.AdaptiveThrottling.BatchEnabled

			controller = throttle.NewAdaptiveController(
				chainID,
				chainCfg.ScanInterval,
				adaptiveCfg,
			)
			headCache = throttle.NewHeadCache(adapter, adaptiveCfg.HeadCacheTTL)

			slog.Info("Adaptive throttling enabled", "chain", chainID)
		}

		// 5. Create Indexer Pipeline
		idxCfg := indexer.Config{
			ChainID:         domain.ChainID(chainID),
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
			Controller:      controller,
			HeadCache:       headCache,
		}

		pipeline := indexer.NewPipeline(idxCfg)
		indexers[chainID] = pipeline
		chainIDs = append(chainIDs, chainID)
	}

	// 6. Initialize Health Monitor
	healthMon := health.NewMonitor(
		chainProviders,
		cursorMgr,
		missingRepo,
		failedRepo,
		budgetTracker,
		fetcher,
		scanIntervals,
		routers,
	)

	healthServer := health.NewServer(healthMon, cfg.Port)

	// 7. Initialize Redis and Rescan Workers
	var redisClient *redisclient.Client
	rescanWorkers := make(map[domain.ChainID]*rescan.Worker)
	pruners := make(map[domain.ChainID]*worker.Pruner)

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
				if chainCfg.RescanRanges {
					worker := rescan.NewWorker(
						rescan.DefaultConfig(),
						string(chainCfg.ChainID),
						redisClient,
						fetcher.adapters[chainCfg.ChainID],
						blockRepo,
						txRepo,
					)
					rescanWorkers[chainCfg.ChainID] = worker
					slog.Info("Rescan worker initialized", "chain", chainCfg.ChainID)
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
		go func(id domain.ChainID, i indexer.Indexer) {
			if err := i.Start(ctx); err != nil {
				w.log.Error("Indexer failed", "chain", id, "error", err)
			}
		}(id, idx)

		// Start Backfiller
		if bf, ok := w.backfillers[id]; ok {
			w.log.Info("Starting backfill processor", "chain", id)
			go func(id domain.ChainID, processor *backfill.Processor) {
				if err := processor.Run(ctx, id); err != nil {
					w.log.Error("Backfill processor failed", "chain", id, "error", err)
				}
			}(id, bf)
		}
	}

	// Start Rescan Workers
	for id, worker := range w.rescanWorkers {
		w.log.Info("Starting rescan worker", "chain", id)
		go func(id domain.ChainID, wrk *rescan.Worker) {
			if err := wrk.Run(ctx); err != nil {
				w.log.Error("Rescan worker failed", "chain", id, "error", err)
			}
		}(id, worker)
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
	adapters map[domain.ChainID]chain.Adapter
}

func (f *MultiChainFetcher) GetLatestHeight(
	ctx context.Context,
	chainID domain.ChainID,
) (uint64, error) {
	if adapter, ok := f.adapters[chainID]; ok {
		return adapter.GetLatestBlock(ctx)
	}
	return 0, fmt.Errorf("chain not found: %s", chainID)
}

func (w *Watcher) runMetricsUpdater(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for chainID, coord := range w.coordinators {
				name, _ := domain.ChainNameFromID(chainID)
				coord.UpdateMetrics(chainID)
				slog.Debug("Updating RPC metrics", "chainID", chainID, "label_name", name)
			}
		}
	}
}
