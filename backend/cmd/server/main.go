// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package main is the entrypoint for the AF backend server.
//
// Lifecycle:
//  1. Load config (Viper + .env + env vars)
//  2. Initialize logger (zap)
//  3. Open + migrate the database (GORM)
//  4. Build the notify manager (A4: multi-channel webhooks + retry +
//     circuit breaker)
//  5. Build router (Gin + middleware), passing the DB and notify
//     handlers through
//  6. Run HTTP server with graceful shutdown on SIGINT/SIGTERM
package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	aipkg "github.com/skyzhao/af/internal/ai"
	"github.com/skyzhao/af/internal/auth"
	"github.com/skyzhao/af/internal/calendar"
	"github.com/skyzhao/af/internal/config"
	"github.com/skyzhao/af/internal/database"
	datasourcepkg "github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/datasource/source/akshare"
	"github.com/skyzhao/af/internal/datasource/source/eastmoney"
	"github.com/skyzhao/af/internal/datasource/source/sina"
	executorpkg "github.com/skyzhao/af/internal/executor"
	"github.com/skyzhao/af/internal/executor/nodes"
	"github.com/skyzhao/af/internal/executor/templates"
	"github.com/skyzhao/af/internal/logger"
	"github.com/skyzhao/af/internal/notify"
	"github.com/skyzhao/af/internal/notify/registry"
	"github.com/skyzhao/af/internal/orchestrator"
	"github.com/skyzhao/af/internal/perf"
	"github.com/skyzhao/af/internal/position"
	"github.com/skyzhao/af/internal/review"
	"github.com/skyzhao/af/internal/router"
	"github.com/skyzhao/af/internal/settings"
)

var version = "0.1.0"

func main() {
	configPath := flag.String("config", "", "path to config.yaml (optional)")
	flag.Parse()

	// 1. Config
	loader, err := config.NewLoader(*configPath, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	cfg, err := loader.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse config: %v\n", err)
		os.Exit(1)
	}

	// 2. Logger
	l, err := logger.New(logger.Options{
		Level:    cfg.Log.Level,
		Encoding: cfg.Log.Encoding,
		Output:   cfg.Log.Output,
		FilePath: cfg.Log.FilePath,
		IsDev:    cfg.IsDev(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync(l)

	l.Info("starting af-backend",
		zap.String("env", cfg.App.Env),
		zap.String("version", cfg.App.Version),
		zap.String("app_name", cfg.App.Name),
	)

	// 3. Database
	db, err := initDB(cfg, l)
	if err != nil {
		l.Error("init database failed", zap.Error(err))
		os.Exit(1)
	}
	if db != nil {
		sqlDB, _ := db.DB()
		defer func() {
			if sqlDB != nil {
				_ = sqlDB.Close()
			}
		}()
	}

	// 4. Notify manager (A4)
	notifyMgr, notifyBreakers := registry.New(registry.Options{
		Cfg:    cfg.Notify.Channel,
		Logger: l,
	})
	notifyTestHandler, err := notify.NewTestPingHandler(notifyMgr, l)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init notify test handler: %v\n", err)
		os.Exit(1)
	}
	notifyHealthHandler := notify.NewHealthHandler(notifyBreakers)
	l.Info("notify manager ready",
		zap.Strings("channels", notifyMgr.List()),
	)

	// 4.5 Datasource manager (A3)
	datasourceHealthHandler, datasourceMgr, err := initDatasource(cfg, db, l)
	if err != nil {
		l.Error("init datasource failed", zap.Error(err))
		os.Exit(1)
	}

	// 4.7 Orchestrator (A7-BE1): build the node registry +
	// executor + strategy CRUD handlers. The trial-run handler
	// reuses the same executor. Routes are mounted under
	// /api/v1/strategies (CRUD + trial-run).
	strategyHandler, trialHandler, nodeRegistry, orchExec := initOrchestrator(db, l)

	// 4.8 Executor + scheduler + templates (A7-BE2): wires the
	// run-level executor, cron scheduler, calendar, and 5
	// built-in strategy templates. Routes are mounted under
	// /api/v1/runs + /api/v1/recommendations + the templates
	// sub-routes on /api/v1/strategies.
	execDeps := ExecutorDeps{
		DB:         db,
		Cfg:        cfg.Executor,
		CalCfg:     cfg.Calendar,
		NotifyMgr:  notifyMgr,
		Datasource: datasourceMgr,
		Strategy:   nil, // populated below if db != nil
		OrchExec:   orchExec,
		Registry:   nodeRegistry,
		Logger:     l,
	}
	if db != nil {
		execDeps.Strategy = orchestrator.NewStrategyService(db, l)
	}
	executorRoutes, sched, schedulerStop, err := initExecutor(execDeps, cfg)
	if err != nil {
		l.Error("init executor failed", zap.Error(err))
		os.Exit(1)
	}
	// Wire the cron scheduler into the strategy service so an Update
	// that flips status→active (or changes cron) immediately registers
	// or removes the strategy's cron entry.
	if sched != nil && strategyHandler != nil {
		strategyHandler.SetScheduleReloader(sched)
	}

	// 4.9 Perf engine (§9): post-hoc T+N returns, drawdown, win
	// rate + group-by aggregations. Shares the same calendar +
	// datasource as the executor. Routes are mounted under
	// /api/v1/perf. §9.5 startup backfill runs in a goroutine
	// before the cron (§9.7) takes over.
	var perfRoutes router.RouteRegistrar
	var perfStop func(context.Context) error
	if cfg.Perf.Enabled {
		perfRoutes, perfStop, err = initPerf(initPerfDeps{
			DB:         db,
			Cfg:        cfg.Perf,
			CalCfg:     cfg.Calendar,
			CronCfg:    cfg.Cron,
			Datasource: datasourceMgr,
			Logger:     l,
		}, l)
		if err != nil {
			l.Error("init perf failed", zap.Error(err))
			os.Exit(1)
		}
	} else {
		l.Info("perf engine disabled by config")
	}

	// §10 positions ledger. The quote source is the datasource
	// manager (may be nil if no sources configured — the service
	// then leaves P&L fields empty).
	var quoteSrc position.QuoteSource
	if datasourceMgr != nil {
		quoteSrc = datasourceMgr
	}
	positionRoutes := position.NewHandler(position.NewService(db, quoteSrc), l)

	// §11 AI assistant + §14.9 reviews share one hot-swappable LLM
	// Provider (the settings page can change the backend at runtime).
	// The initial client comes from config; a saved DB setting (loaded
	// below) overrides it.
	var initLLM aipkg.Client
	if cfg.AI.Enabled {
		switch cfg.AI.Provider {
		case "openai":
			if cfg.AI.APIKey == "" || cfg.AI.BaseURL == "" || cfg.AI.Model == "" {
				l.Warn("ai: provider=openai but base_url/api_key/model incomplete; falling back to mock")
				initLLM = aipkg.NewMockClient()
			} else {
				initLLM = aipkg.NewOpenAIClient(cfg.AI.BaseURL, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.Timeout)
			}
		default:
			initLLM = aipkg.NewMockClient()
		}
	}
	llmProvider := aipkg.NewProvider(initLLM)

	// Runtime LLM settings: load any saved row → hot-swap the provider.
	settingsSvc := settings.NewService(db, llmProvider, cfg.AI.Timeout)
	// Load the multi-provider fallback chain (migrates the legacy
	// single-row setting in on first boot). Replaces the old single
	// LoadOnStart.
	settingsSvc.LoadChainOnStart(context.Background())
	settingsRoutes := settings.NewHandler(settingsSvc, l)

	var aiRoutes router.RouteRegistrar
	if cfg.AI.Enabled {
		aiSvc := aipkg.NewService(db, llmProvider, orchestrator.NewStrategyService(db, l))
		aiRoutes = aipkg.NewHandler(aiSvc, l)
		l.Info("ai assistant ready", zap.String("active", llmProvider.Name()))
	} else {
		l.Info("ai assistant disabled by config")
	}

	// §14.9 review reports. Reuses the shared LLM provider for summaries
	// (nil-safe → data-only summary). Scheduler fires daily/weekly.
	var reviewRoutes router.RouteRegistrar
	var reviewStop func(context.Context) error
	if cfg.Review.Enabled {
		reviewSvc := review.NewService(db, llmProvider)
		reviewRoutes = review.NewHandler(reviewSvc, l)
		sched, serr := review.NewScheduler(reviewSvc, review.SchedulerConfig{
			DailyCron:  cfg.Review.DailyCron,
			WeeklyCron: cfg.Review.WeeklyCron,
			Timezone:   cfg.Review.Timezone,
			Logger:     l,
		})
		if serr != nil {
			l.Error("review scheduler init failed", zap.Error(serr))
		} else if err := sched.Start(); err != nil {
			l.Error("review scheduler start failed", zap.Error(err))
		} else {
			reviewStop = sched.Stop
		}
		l.Info("review reports ready")
	} else {
		l.Info("review reports disabled by config")
	}

	// §12 auth: single-admin login gate (multi-user-ready). When
	// disabled, the three router fields stay nil and the whole /api/v1
	// surface remains open (dev default). When enabled we build the
	// service, bootstrap the first admin on an empty DB, and pass the
	// middleware + public(login)/protected(me,change-pw) registrars.
	var authMiddleware gin.HandlerFunc
	var authPublicRoutes, authProtectedRoutes router.RouteRegistrar
	if cfg.Auth.Enabled {
		if db == nil {
			l.Error("auth enabled but DB unavailable; cannot start")
			os.Exit(1)
		}
		secret := cfg.Auth.JWTSecret
		if secret == "" {
			secret = randomSecret(48)
			l.Warn("auth: AUTH_JWT_SECRET empty; generated an ephemeral secret. "+
				"Set a stable secret in config/env or all tokens invalidate on restart.",
				zap.String("generated_secret", secret),
			)
		}
		authSvc := auth.NewService(db, secret, cfg.Auth.TokenTTL)
		genPw, berr := authSvc.Bootstrap(context.Background(), cfg.Auth.AdminUser, cfg.Auth.AdminPassword)
		if berr != nil {
			l.Error("auth: bootstrap admin failed", zap.Error(berr))
			os.Exit(1)
		}
		if genPw != "" {
			user := cfg.Auth.AdminUser
			if user == "" {
				user = "admin"
			}
			l.Warn("auth: created first admin with a GENERATED password — save it now, it is shown only once",
				zap.String("username", user),
				zap.String("password", genPw),
			)
		}
		authHandler := auth.NewHandler(authSvc, l)
		authMiddleware = authSvc.Middleware()
		authPublicRoutes = authHandler.PublicRoutes()
		authProtectedRoutes = authHandler.ProtectedRoutes()
		l.Info("auth enabled", zap.Duration("token_ttl", cfg.Auth.TokenTTL))
	} else {
		l.Info("auth disabled by config; /api/v1 is open")
	}

	// 5. Router
	r := router.New(router.Options{
		APIBasePath:             cfg.Server.APIBasePath,
		Logger:                  l,
		Version:                 version,
		DB:                      db,
		NotifyTestHandler:       notifyTestHandler,
		NotifyHealthHandler:     notifyHealthHandler,
		DatasourceHealthHandler: datasourceHealthHandler,
		StrategyRoutes:          strategyHandler,
		TrialRunRoutes:          trialHandler,
		TemplateRoutes:          nil, // wired through RunRoutes below
		RunRoutes:               executorRoutes,
		PerfRoutes:              perfRoutes,
		PositionRoutes:          positionRoutes,
		AIRoutes:                aiRoutes,
		ReviewRoutes:            reviewRoutes,
		SettingsRoutes:          settingsRoutes,
		AuthMiddleware:          authMiddleware,
		AuthPublicRoutes:        authPublicRoutes,
		AuthProtectedRoutes:     authProtectedRoutes,
	})

	// 6. HTTP server with graceful shutdown
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Run server in a goroutine so the main goroutine can wait for signals.
	serverErr := make(chan error, 1)
	go func() {
		l.Info("http server listening", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for signal or server error.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		l.Info("received signal, shutting down", zap.String("signal", sig.String()))
	case err := <-serverErr:
		if err != nil {
			l.Error("server failed", zap.Error(err))
			os.Exit(1)
		}
	}

	// Graceful shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		l.Error("server forced shutdown", zap.Error(err))
		os.Exit(1)
	}
	// Stop the cron scheduler so in-flight jobs complete
	// before we exit. We reuse the same shutdown context.
	if schedulerStop != nil {
		if err := schedulerStop(shutdownCtx); err != nil {
			l.Warn("scheduler stop", zap.Error(err))
		}
	}
	// Stop the perf scheduler (§9.7) — same idea as the executor
	// scheduler above: drain in-flight fires before the process
	// exits.
	if perfStop != nil {
		if err := perfStop(shutdownCtx); err != nil {
			l.Warn("perf scheduler stop", zap.Error(err))
		}
	}
	if reviewStop != nil {
		if err := reviewStop(shutdownCtx); err != nil {
			l.Warn("review scheduler stop", zap.Error(err))
		}
	}

	l.Info("server stopped cleanly",
		zap.Duration("shutdown_timeout", cfg.Server.ShutdownTimeout),
	)
	_ = time.Second // keep import used in some builds
}

// initDB opens the GORM connection and runs migrations. If DB_DRIVER is set
// to "none" / "disabled", the function returns (nil, nil) so the server can
// still start in a DB-less mode (useful for A1 smoke tests).
func initDB(cfg *config.Config, l *zap.Logger) (*gorm.DB, error) {
	driver := cfg.DB.Driver
	if driver == "none" || driver == "disabled" {
		l.Warn("database disabled via DB_DRIVER; skipping init")
		return nil, nil
	}

	db, err := database.Open(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := database.Ping(db); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if err := database.Migrate(db); err != nil {
		return nil, fmt.Errorf("migrate db: %w", err)
	}
	l.Info("database ready",
		zap.String("driver", cfg.DB.Driver),
		zap.String("dsn_host", cfg.DB.Host),
	)
	return db, nil
}

// initDatasource wires the A3 datasource manager: builds the source
// chain from config, hooks up an in-memory cache (production should
// swap to Redis via cfg.Redis), wires a GORM-backed HealthRepo when a
// DB is available, and returns a Gin handler for
// GET /api/v1/datasource/health. If no sources are configured the
// handler is nil (route not registered).
func initDatasource(cfg *config.Config, db *gorm.DB, l *zap.Logger) (gin.HandlerFunc, datasourcepkg.Manager, error) {
	sources, err := buildDatasourceSources(cfg.Datasource, l)
	if err != nil {
		// No sources configured → do not register the route. We
		// log a warning so operators can spot the misconfig.
		l.Warn("datasource: no sources configured; /api/v1/datasource/health disabled", zap.Error(err))
		return nil, nil, err
	}

	// Cache: prefer Redis when one is reachable; fall back to an
	// in-process cache. The Redis probe is best-effort: if it
	// fails, we still want the manager to operate.
	var cache datasourcepkg.Cache
	{
		addr := fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)
		client := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := client.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			cache = datasourcepkg.NewRedisCache(client)
			l.Info("datasource: using redis cache", zap.String("addr", addr))
		} else {
			l.Warn("datasource: redis ping failed; using in-memory cache", zap.Error(err))
			cache = datasourcepkg.NewMemoryCache()
		}
	}

	// Health repo: GORM-backed when DB is present, no-op otherwise.
	var healthRepo datasourcepkg.HealthRepo
	if db != nil {
		healthRepo = datasourcepkg.NewGormHealthRepo(db)
	} else {
		healthRepo = nil
		l.Warn("datasource: no DB; health persistence disabled")
	}

	mgr := datasourcepkg.NewManager(sources, cache, healthRepo, datasourcepkg.ManagerConfig{
		CacheTTL:             cfg.Datasource.CacheTTL,
		FailThreshold:        cfg.Datasource.FailThreshold,
		FailWindow:           cfg.Datasource.FailWindow,
		Cooldown:             cfg.Datasource.Cooldown,
		ConsistencyThreshold: cfg.Datasource.ConsistencyThreshold,
		UniverseURL:          universeURL(cfg.Datasource),
	}, l)

	l.Info("datasource manager ready",
		zap.Strings("sources", mgr.ListSources()),
	)
	return datasourcepkg.NewHealthHandler(mgr, healthRepo, l).Handle, mgr, nil
}

// universeURL returns the akshare sidecar base URL (its /universe
// endpoint resolves stock pools). Picks the akshare source's base_url
// if configured + enabled, else the akshare package default.
func universeURL(cfg config.DatasourceConfig) string {
	for _, s := range cfg.Sources {
		if s.Name == "akshare" && s.Enabled {
			if s.BaseURL != "" {
				return s.BaseURL
			}
			return akshare.DefaultBaseURL
		}
	}
	return ""
}

// buildDatasourceSources constructs the configured Source chain in
// YAML order. Disabled sources are skipped. Unknown names are
// reported as an error so misconfiguration fails fast.
func buildDatasourceSources(cfg config.DatasourceConfig, l *zap.Logger) ([]datasourcepkg.Source, error) {
	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("datasource: buildDatasourceSources: no sources configured")
	}
	out := make([]datasourcepkg.Source, 0, len(cfg.Sources))
	for _, sc := range cfg.Sources {
		if !sc.Enabled {
			l.Info("datasource: source disabled, skipping", zap.String("name", sc.Name))
			continue
		}
		timeout := sc.Timeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		var s datasourcepkg.Source
		switch sc.Name {
		case "eastmoney":
			s = eastmoney.New(eastmoney.Options{
				BaseURL:  sc.BaseURL,
				KLineURL: sc.BaseURL,
				Timeout:  timeout,
			})
		case "sina":
			s = sina.New(sina.Options{
				BaseURL: sc.BaseURL,
				Timeout: timeout,
			})
		case "akshare":
			s = akshare.New(akshare.Options{
				BaseURL: sc.BaseURL,
				Timeout: timeout,
			})
		default:
			return nil, fmt.Errorf("datasource: unknown source %q", sc.Name)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("datasource: buildDatasourceSources: all configured sources disabled")
	}
	return out, nil
}

// initOrchestrator wires the A7-BE1 strategy + trial-run
// surface. The DB may be nil — in that case the strategy CRUD
// service still constructs (it stores ephemeral in-memory state
// is *not* a goal: the service requires a DB for any meaningful
// call), but trial-runs against in-memory DAGs continue to work.
//
// Returns (strategyHandler, trialRunHandler, nodeRegistry, orchestratorExecutor).
// Either handler may be nil when the DB is unavailable so the
// router skips the routes.
func initOrchestrator(db *gorm.DB, l *zap.Logger) (*orchestrator.StrategyHandler, router.RouteRegistrar, *orchestrator.Registry, *orchestrator.Executor) {
	// Node registry: shared by the executor for resolving DAG node types.
	reg := orchestrator.NewRegistry()
	nodes.RegisterAll(reg)

	exec := orchestrator.NewExecutor(orchestrator.ExecutorConfig{
		Registry:       reg,
		Logger:         l,
		MaxConcurrency: 4,
		DefaultTimeout: 60 * time.Second,
	})

	if db == nil {
		l.Warn("orchestrator: DB unavailable; strategy CRUD disabled")
		return nil, nil, reg, exec
	}

	svc := orchestrator.NewStrategyService(db, l)
	strat := orchestrator.NewStrategyHandler(svc, l)
	trial := orchestrator.NewTrialRunHandler(svc, exec, reg, l)

	l.Info("orchestrator ready",
		zap.Strings("node_types", reg.ListTypes()),
	)
	return strat, trial, reg, exec
}

// ExecutorDeps groups the executor wiring so initExecutor's
// signature stays short. Calendar and Datasource are nullable
// (the executor short-circuits when they're nil).
type ExecutorDeps struct {
	DB         *gorm.DB
	Cfg        config.ExecutorConfig
	Cal        *calendar.Service
	CalCfg     config.CalendarConfig
	NotifyMgr  notify.Manager
	Datasource datasourcepkg.Manager
	Strategy   *orchestrator.StrategyService
	OrchExec   *orchestrator.Executor
	Registry   *orchestrator.Registry
	Logger     *zap.Logger
}

// initExecutor wires the run-level executor + cron scheduler +
// 5 templates + 7 HTTP routes. Returns the executorpkg.Handler
// for the router plus a shutdown hook that stops the scheduler.
func initExecutor(d ExecutorDeps, cfg *config.Config) (router.RouteRegistrar, *executorpkg.Scheduler, func(context.Context) error, error) {
	if d.DB == nil {
		d.Logger.Warn("executor: DB unavailable; run + recommendation routes disabled")
		return nil, nil, nil, nil
	}
	executor := executorpkg.NewExecutor(executorpkg.ExecutorOptions{
		DB:         d.DB,
		Cfg:        d.Cfg,
		Logger:     d.Logger,
		Bus:        orchestrator.NewMemBus(),
		DataSource: d.Datasource,
		Notify:     d.NotifyMgr,
		Strategy:   d.Strategy,
		Executor:   d.OrchExec,
		Registry:   d.Registry,
	})

	// Calendar service: trading-day + session checks. Used by
	// the scheduler and the session_tag node.
	cal, err := calendar.NewService(d.CalCfg, d.DB)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("calendar: %w", err)
	}

	// Scheduler: robfig/cron, gated by Executor.SchedulerEnabled.
	sched, err := executorpkg.NewScheduler(executorpkg.SchedulerConfig{
		DB:       d.DB,
		Logger:   d.Logger,
		Cron:     cfg.Cron,
		Executor: executor,
		Calendar: cal,
		Strategy: d.Strategy,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("scheduler: %w", err)
	}

	// Template loader: bundle the 5 built-in templates and
	// sync them to the strategy_templates table.
	loader, err := templates.NewLoader(d.DB, d.Logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("templates: %w", err)
	}
	if err := loader.SyncToDB(context.Background()); err != nil {
		d.Logger.Warn("templates: sync to db failed", zap.Error(err))
	}

	// HTTP handler.
	runH := executorpkg.NewHandler(executor, d.Logger, d.Cfg)
	tplH := executorpkg.NewTemplateHandler(loader, d.DB, d.Logger)

	// Wire the two registrars into a single composite that
	// satisfies router.RouteRegistrar.
	composite := &executorRegistrar{run: runH, tpl: tplH}

	// Start the scheduler if enabled.
	if d.Cfg.SchedulerEnabled {
		if err := sched.LoadFromDB(context.Background()); err != nil {
			d.Logger.Warn("scheduler: load from db", zap.Error(err))
		}
		sched.Start()
		d.Logger.Info("scheduler started",
			zap.Int("active_strategies", len(sched.ActiveEntries())),
		)
	} else {
		d.Logger.Info("scheduler disabled by config")
	}

	shutdown := func(ctx context.Context) error {
		if d.Cfg.SchedulerEnabled {
			return sched.Stop(ctx)
		}
		return nil
	}

	d.Logger.Info("executor ready",
		zap.Int("templates", len(loader.ListCodes())),
		zap.Duration("default_run_timeout", d.Cfg.DefaultRunTimeout),
		zap.Duration("sse_heartbeat", d.Cfg.SSEHeartbeat),
	)
	return composite, sched, shutdown, nil
}

// executorRegistrar mounts both /runs + /recommendations under
// the API root AND /strategies/templates + /strategies/from-template
// under the /strategies sub-group. The router calls
// RegisterRoutes(api *gin.RouterGroup) once with the API root,
// so we split internally.
type executorRegistrar struct {
	run *executorpkg.Handler
	tpl *executorpkg.TemplateHandler
}

// RegisterRoutes mounts runs + recommendations under api, and
// the template routes under api/strategies. We accept the same
// (api *gin.RouterGroup) signature used by other registrars.
func (r *executorRegistrar) RegisterRoutes(api *gin.RouterGroup) {
	if r.run != nil {
		r.run.RegisterRoutes(api)
	}
	if r.tpl != nil {
		strat := api.Group("/strategies")
		r.tpl.RegisterRoutes(strat)
	}
}

// =============================================================================
// §9 Perf engine
// =============================================================================

// initPerfDeps groups the perf package wiring so initPerf's
// signature stays short. DB, Cal, and Datasource are all
// optional: when DB is nil the routes are mounted but every
// call short-circuits with 503 "perf: db not configured"; when
// Datasource or Calendar is nil the same applies (compute
// needs both to fetch K-line).
type initPerfDeps struct {
	DB         *gorm.DB
	Cfg        config.PerfConfig
	CalCfg     config.CalendarConfig
	CronCfg    config.CronConfig
	Datasource datasourcepkg.Manager
	Logger     *zap.Logger
}

// initPerf builds the §9 perf service + handler. The calendar
// service is local to perf (independent of any other consumer
// — the executor's calendar is fine to share, but we don't
// want a perf startup to fail because the executor is also
// down).
//
// On success it also:
//   - §9.5 fires a one-shot startup backfill in a goroutine
//     over the configured KlineLookback window, so the
//     /api/v1/perf/aggregations view has data on a fresh
//     deploy.
//   - §9.7 starts the end-of-day backfill cron if
//     BackfillCron is non-empty.
//
// Returns (handler, shutdown, err). handler is nil when the DB
// is unavailable; shutdown is nil when no cron was started. err
// is non-nil only on hard init failures (calendar, etc.) — the
// caller decides whether to abort the process.
func initPerf(d initPerfDeps, l *zap.Logger) (router.RouteRegistrar, func(context.Context) error, error) {
	if d.DB == nil {
		l.Warn("perf: DB unavailable; /api/v1/perf routes disabled")
		return nil, nil, nil
	}
	cal, err := calendar.NewService(d.CalCfg, d.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("perf: calendar init: %w", err)
	}
	svc := perf.NewService(perf.Options{
		DB:             d.DB,
		Datasource:     d.Datasource,
		Calendar:       cal,
		Logger:         l,
		KlineLookback:  d.Cfg.KlineLookback,
		KlineTimeout:   d.Cfg.KlineTimeout,
		KlineFuturePad: d.Cfg.KlineFuturePad,
	})
	handler := perf.NewHandler(svc, l)

	// §9.5 startup backfill. We bound the goroutine with the
	// configured StartupBackfillTimeout so a stuck datasource
	// doesn't pin a context forever after a server restart. A
	// zero or negative value falls back to a 5-minute ceiling —
	// the original hardcoded value — so a misconfigured
	// deployment can't run unbounded. processed/errored counts
	// are logged for the operator.
	backfillTimeout := d.Cfg.StartupBackfillTimeout
	if backfillTimeout <= 0 {
		backfillTimeout = 5 * time.Minute
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), backfillTimeout)
		defer cancel()
		processed, errs, berr := svc.Backfill(bg, d.Cfg.KlineLookback)
		if berr != nil {
			l.Warn("perf: startup backfill failed", zap.Error(berr))
			return
		}
		l.Info("perf: startup backfill done",
			zap.Int("processed", processed),
			zap.Int("errored", errs),
			zap.Duration("lookback", d.Cfg.KlineLookback),
			zap.Duration("timeout", backfillTimeout),
		)
	}()

	// §9.7 end-of-day cron. An empty BackfillCron disables it —
	// the operator can still hit POST /perf/calculate manually.
	var schedStop func(context.Context) error
	if d.Cfg.BackfillCron != "" {
		sched, serr := perf.NewScheduler(svc, perf.SchedulerConfig{
			Cron:     d.Cfg.BackfillCron,
			Timezone: d.CronCfg.Timezone,
			Lookback: d.Cfg.KlineLookback,
			Logger:   l,
		})
		if serr != nil {
			l.Error("perf: scheduler init failed", zap.Error(serr))
		} else if serr := sched.Start(); serr != nil {
			l.Error("perf: scheduler start failed", zap.Error(serr))
		} else {
			schedStop = sched.Stop
		}
	} else {
		l.Info("perf: cron disabled (empty backfill_cron)")
	}

	l.Info("perf engine ready",
		zap.Duration("kline_lookback", d.Cfg.KlineLookback),
		zap.Duration("kline_timeout", d.Cfg.KlineTimeout),
		zap.String("backfill_cron", d.Cfg.BackfillCron),
	)
	return handler, schedStop, nil
}

// randomSecret returns a hex-encoded random string of n bytes, used as
// an ephemeral JWT signing secret when none is configured. Tokens
// signed with it invalidate on restart — the caller logs a warning.
func randomSecret(n int) string {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		// Extremely unlikely; fall back to a time-derived value so we
		// still return *something* non-empty rather than crash boot.
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}
