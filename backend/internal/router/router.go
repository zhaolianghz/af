// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package router wires the Gin engine, middleware chain, and route registrations.
package router

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/handler"
	"github.com/skyzhao/af/internal/middleware"
	"github.com/skyzhao/af/internal/openapi"
)

// RouteRegistrar is the contract for feature packages that mount
// their own gin.RouterGroup. The strategy + trial-run handlers in
// internal/orchestrator implement this so we don't import that
// package's types here (keeps router lean).
type RouteRegistrar interface {
	RegisterRoutes(g *gin.RouterGroup)
}

// Options configures the router.
type Options struct {
	APIBasePath string // e.g. "/api/v1"
	Logger      *zap.Logger
	Version     string
	DB          *gorm.DB // optional; passed through to the health handler
	// NotifyTestHandler is the POST /api/v1/notify/test handler. May be
	// nil; in that case the route is not registered.
	NotifyTestHandler gin.HandlerFunc
	// NotifyHealthHandler is the GET /api/v1/notify/health handler.
	// May be nil; in that case the route is not registered.
	NotifyHealthHandler gin.HandlerFunc
	// DatasourceHealthHandler is the GET /api/v1/datasource/health
	// handler. May be nil; in that case the route is not registered.
	DatasourceHealthHandler gin.HandlerFunc
	// StrategyRoutes mounts /api/v1/strategies CRUD endpoints. May be
	// nil; in that case the routes are skipped.
	StrategyRoutes RouteRegistrar
	// TrialRunRoutes mounts /api/v1/strategies/:id/trial-run endpoints.
	// May be nil; in that case the routes are skipped. The registrar
	// is mounted under the same /strategies group as StrategyRoutes,
	// so the :id parameter is shared.
	TrialRunRoutes RouteRegistrar
	// TemplateRoutes mounts /api/v1/strategies/templates +
	// /strategies/from-template/:code. Mounted under the same
	// /strategies group.
	TemplateRoutes RouteRegistrar
	// RunRoutes mounts /api/v1/runs/* + /api/v1/recommendations.
	RunRoutes RouteRegistrar
	// PerfRoutes mounts /api/v1/perf/* (post-hoc T+N return,
	// drawdown, win rate + group-by aggregations). §9 of the
	// design doc. May be nil; in that case the routes are
	// skipped.
	PerfRoutes RouteRegistrar
	// PositionRoutes mounts /api/v1/positions/* (user-tracked
	// holdings ledger, §10). May be nil; routes skipped then.
	PositionRoutes RouteRegistrar
	// AIRoutes mounts /api/v1/strategies/:id/ai/* (§11 conversational
	// strategy editing). May be nil; routes skipped then.
	AIRoutes RouteRegistrar
	// ReviewRoutes mounts /api/v1/reviews/* (§14.9 auto review
	// reports). May be nil; routes skipped then.
	ReviewRoutes RouteRegistrar
	// SettingsRoutes mounts /api/v1/settings/* (runtime LLM config).
	// May be nil; routes skipped then.
	SettingsRoutes RouteRegistrar
	// AuthMiddleware, when non-nil, is applied to /api/v1 AFTER the
	// public routes (ping/healthz/openapi/login) so everything else
	// requires a valid Bearer JWT (§12).
	AuthMiddleware gin.HandlerFunc
	// AuthPublicRoutes mounts unauthenticated auth endpoints (login).
	AuthPublicRoutes RouteRegistrar
	// AuthProtectedRoutes mounts authenticated auth endpoints (me,
	// change-password) — registered after the middleware.
	AuthProtectedRoutes RouteRegistrar
}

// New builds a fully wired Gin engine. Health and ping endpoints are always
// registered; feature modules are mounted under <APIBasePath>/<module> by
// future RegisterRoutes callers.
func New(opts Options) *gin.Engine {
	if opts.APIBasePath == "" {
		opts.APIBasePath = "/api/v1"
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Middleware order: RequestID -> Recovery -> Logger -> CORS
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery(opts.Logger))
	r.Use(middleware.Logger(opts.Logger))
	r.Use(middleware.CORS())

	// Health
	var h *handler.Health
	if opts.DB != nil {
		h = handler.NewHealthWithDB(opts.Version, opts.DB)
	} else {
		h = handler.NewHealth(opts.Version)
	}
	// /healthz is the legacy root-level path kept for backward
	// compatibility with load balancers / k8s probes that probe
	// the root. The canonical path under /api/v1 is also wired
	// below for consistency with the rest of the API.
	r.GET("/healthz", h.Healthz)
	// /docs is the Swagger UI page. Mounted at the root (not
	// under /api/v1) so the URL is shareable and not "API-endpoint
	// looking" — docs are a separate concern from the API itself.
	r.GET("/docs", openapi.Docs())
	r.GET("/docs/*filepath", openapi.Docs())

	// API group
	api := r.Group(opts.APIBasePath)
	{
		api.GET("/ping", handler.Ping)
		api.GET("/healthz", h.Healthz)
		// OpenAPI 3 spec (JSON) and Swagger UI (HTML). Both
		// assets are embedded at build time — no filesystem
		// dependency. /docs is also mounted at the root in the
		// docs block below so the URL is easier to share.
		api.GET("/openapi.json", openapi.Spec())
		// §12 auth: login is public; everything registered AFTER the
		// middleware Use below requires a valid Bearer token. The
		// public basics above (ping/healthz/openapi) stay open.
		if opts.AuthPublicRoutes != nil {
			opts.AuthPublicRoutes.RegisterRoutes(api)
		}
		if opts.AuthMiddleware != nil {
			api.Use(opts.AuthMiddleware)
		}
		if opts.AuthProtectedRoutes != nil {
			opts.AuthProtectedRoutes.RegisterRoutes(api)
		}
		// A4 notify routes. Both are optional — when the handler is
		// nil the route is skipped. The notify package supplies the
		// concrete handlers; we just wire them here.
		if opts.NotifyTestHandler != nil {
			api.POST("/notify/test", opts.NotifyTestHandler)
		}
		if opts.NotifyHealthHandler != nil {
			api.GET("/notify/health", opts.NotifyHealthHandler)
		}
		// A3 datasource routes.
		if opts.DatasourceHealthHandler != nil {
			api.GET("/datasource/health", opts.DatasourceHealthHandler)
		}
		// A7 strategies / trial-run routes. Mounted under the same
		// /strategies group so :id is shared between CRUD and
		// trial-run endpoints.
		if opts.StrategyRoutes != nil || opts.TrialRunRoutes != nil || opts.TemplateRoutes != nil {
			strat := api.Group("/strategies")
			if opts.StrategyRoutes != nil {
				opts.StrategyRoutes.RegisterRoutes(strat)
			}
			if opts.TrialRunRoutes != nil {
				opts.TrialRunRoutes.RegisterRoutes(strat)
			}
			if opts.TemplateRoutes != nil {
				opts.TemplateRoutes.RegisterRoutes(strat)
			}
			// §11 AI assistant: /strategies/:id/ai/*.
			if opts.AIRoutes != nil {
				opts.AIRoutes.RegisterRoutes(strat.Group("/:id/ai"))
			}
		}
		// A7 runs + recommendations routes.
		if opts.RunRoutes != nil {
			opts.RunRoutes.RegisterRoutes(api)
		}
		// §9 perf engine routes. Mounted under /api/v1/perf — the
		// handler registers /recommendations/:id, /aggregations, etc.
		// relative to this subgroup (matching the OpenAPI spec and the
		// /perf/* paths in the CHANGELOG). Without the subgroup they
		// collided at /api/v1/recommendations (executor's path) and the
		// documented /api/v1/perf/* 404'd.
		if opts.PerfRoutes != nil {
			opts.PerfRoutes.RegisterRoutes(api.Group("/perf"))
		}
		// §10 positions ledger.
		if opts.PositionRoutes != nil {
			opts.PositionRoutes.RegisterRoutes(api.Group("/positions"))
		}
		// §14.9 review reports.
		if opts.ReviewRoutes != nil {
			opts.ReviewRoutes.RegisterRoutes(api.Group("/reviews"))
		}
		// LLM settings.
		if opts.SettingsRoutes != nil {
			opts.SettingsRoutes.RegisterRoutes(api.Group("/settings"))
		}
	}

	return r
}
