// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package perf — handler.go
//
// HTTP surface for §9.4 of the A7 spec. Mounted under /api/v1/perf by
// the router.
//
//   GET  /api/v1/perf/recommendations/:id          Latest snapshot
//   GET  /api/v1/perf/recommendations/:id/history  All snapshots
//   POST /api/v1/perf/calculate                    Manual compute trigger
//                                                  (single rec by ID, or
//                                                  a date range)
//   GET  /api/v1/perf/aggregations                 Group-by aggregates
package perf

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/httpresp"
	"github.com/skyzhao/af/internal/model"
)

// Handler exposes the perf HTTP surface.
type Handler struct {
	svc    *Service
	logger *zap.Logger
}

// NewHandler wires a handler against a service.
func NewHandler(svc *Service, l *zap.Logger) *Handler {
	if l == nil {
		l = zap.NewNop()
	}
	return &Handler{svc: svc, logger: l}
}

// RegisterRoutes mounts the perf routes onto the given router group.
// The group prefix is the caller's responsibility — the router wires
// it under /api/v1/perf.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/recommendations/:id", h.Latest)
	r.GET("/recommendations/:id/history", h.History)
	r.POST("/calculate", h.Calculate)
	r.GET("/aggregations", h.Aggregations)
}

// =============================================================================
// Response envelopes
// =============================================================================
//
// The envelope is centralised in internal/httpresp — every handler
// in the backend now uses httpresp.OK/Created/Err, so error
// responses get a consistent `request_id` field that ties back to
// the request_id in zap logs (see middleware.RequestID).
//
// =============================================================================
// Handlers
// =============================================================================

// Latest handles GET /perf/recommendations/:id — returns the most
// recent snapshot for a recommendation. 404 when none exists.
func (h *Handler) Latest(c *gin.Context) {
	recID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	snap, err := h.svc.LatestSnapshotForRec(c.Request.Context(), recID)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	if snap == nil {
		httpresp.Err(c,apperr.NotFound("no snapshot for this recommendation yet"))
		return
	}
	httpresp.OK(c,gin.H{"snapshot": snap})
}

// History handles GET /perf/recommendations/:id/history — returns
// every snapshot ever saved for a recommendation, newest first.
func (h *Handler) History(c *gin.Context) {
	recID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpresp.Err(c,apperr.InvalidArg("id must be uint64"))
		return
	}
	snaps, err := h.svc.HistoryForRec(c.Request.Context(), recID)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{"items": snaps, "total": len(snaps)})
}

// CalculateRequest is the body shape for POST /perf/calculate.
//
//   - RecommendationID set: recompute that one recommendation.
//   - From + To set:        recompute every recommendation in
//                            [from, to] (inclusive).
//
// The two modes are mutually exclusive. From/To take ISO-8601 dates
// (YYYY-MM-DD); Time-of-day is ignored.
type CalculateRequest struct {
	RecommendationID uint64 `json:"recommendation_id,omitempty"`
	From             string `json:"from,omitempty"`
	To               string `json:"to,omitempty"`
}

// Calculate handles POST /perf/calculate — manual compute trigger.
func (h *Handler) Calculate(c *gin.Context) {
	var in CalculateRequest
	// Body is optional. ShouldBindJSON returns an error on empty
	// body, which we treat as "no input" rather than 400.
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&in); err != nil {
			httpresp.Err(c,apperr.InvalidArg("invalid request body: "+err.Error()))
			return
		}
	}

	ctx := c.Request.Context()
	if in.RecommendationID != 0 {
		snap, err := h.computeOneByID(ctx, in.RecommendationID)
		if err != nil {
			httpresp.Err(c,err)
			return
		}
		httpresp.Created(c,gin.H{"snapshot": snap})
		return
	}

	if in.From != "" || in.To != "" {
		if in.From == "" || in.To == "" {
			httpresp.Err(c,apperr.InvalidArg("from and to must both be set"))
			return
		}
		from, err := parseDate(in.From)
		if err != nil {
			httpresp.Err(c,apperr.InvalidArg("from: "+err.Error()))
			return
		}
		to, err := parseDate(in.To)
		if err != nil {
			httpresp.Err(c,apperr.InvalidArg("to: "+err.Error()))
			return
		}
		processed, errs, err := h.svc.ComputeRange(ctx, from, to)
		if err != nil {
			httpresp.Err(c,err)
			return
		}
		httpresp.OK(c,gin.H{
			"processed": processed,
			"errored":   errs,
			"from":      in.From,
			"to":        in.To,
		})
		return
	}

	httpresp.Err(c,apperr.InvalidArg("either recommendation_id or from+to is required"))
}

func (h *Handler) computeOneByID(ctx context.Context, recID uint64) (*model.PerformanceSnapshot, error) {
	if h.svc.db == nil {
		return nil, apperr.Unavailable("perf: db not configured")
	}
	var rec model.Recommendation
	if err := h.svc.db.WithContext(ctx).First(&rec, recID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.NotFound("recommendation not found")
		}
		return nil, err
	}
	return h.svc.ComputeAndSave(ctx, &rec)
}

// AggregationsQuery describes the supported query string knobs for
// GET /perf/aggregations.
type AggregationsQuery struct {
	GroupBy  string // "strategy" | "session_tag" | "stock" — default "strategy"
	From     string // YYYY-MM-DD
	To       string // YYYY-MM-DD
	Page     int    // 1-based, default 1
	PageSize int    // default 20, max 200
}

// Aggregations handles GET /perf/aggregations — group-by aggregate
// stats over the latest saved snapshot per recommendation. Supports
// group_by=strategy|session_tag|stock.
func (h *Handler) Aggregations(c *gin.Context) {
	q := AggregationsQuery{
		GroupBy:  defaultStr(c.Query("group_by"), "strategy"),
		From:     c.Query("from"),
		To:       c.Query("to"),
		Page:     atoiDefault(c.Query("page"), 1),
		PageSize: atoiDefault(c.Query("page_size"), 20),
	}
	if q.PageSize <= 0 || q.PageSize > 200 {
		q.PageSize = 20
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	switch q.GroupBy {
	case "strategy", "session_tag", "stock":
	default:
		httpresp.Err(c,apperr.InvalidArg("group_by must be one of: strategy, session_tag, stock"))
		return
	}

	var from, to time.Time
	if q.From != "" {
		t, err := parseDate(q.From)
		if err != nil {
			httpresp.Err(c,apperr.InvalidArg("from: "+err.Error()))
			return
		}
		from = t
	}
	if q.To != "" {
		t, err := parseDate(q.To)
		if err != nil {
			httpresp.Err(c,apperr.InvalidArg("to: "+err.Error()))
			return
		}
		to = t
	}
	rows, total, err := h.aggregate(c.Request.Context(), q, from, to)
	if err != nil {
		httpresp.Err(c,err)
		return
	}
	httpresp.OK(c,gin.H{
		"items":     rows,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
		"group_by":  q.GroupBy,
	})
}

// =============================================================================
// Aggregation
//
// Implementation: pull the latest snapshot per recommendation in the
// date range via a single SELECT, then group + aggregate in Go
// memory. We deliberately do NOT push the per-rec "latest snapshot"
// filter into SQL — the ROW_NUMBER() OVER PARTITION BY pattern
// diverges between MySQL 8 and SQLite 3.25+ and the working set is
// bounded (one row per rec, not per snapshot) so the in-memory
// walk is fine.
//
// Means and win-rate are over recs with the relevant data:
// AvgT1 averages T+1 returns over recs that HAVE T+1; the
// "no rec in the group has T+1 → AvgT1 = null" rule preserves
// the source-snapshot nil-when-undefined signal rather than
// silently folding it to 0. WinRateT5 follows the same rule —
// denominator is "recs with a known T+5".
// =============================================================================

// aggregationRow is one row of the /aggregations response.
//
// All *float64 fields are JSON-`null` (Go nil) when the underlying
// statistic is undefined for the group — e.g. win_rate_t5 is null
// when no rec in the group has a known T+5 return, NOT 0.0.
// Consumers MUST distinguish "null" from "0.0" to avoid a silent
// "0% win rate" misread. The nil-when-undefined signal flows
// from the source snapshots (suspended stocks, source errors,
// future T+N) and is preserved by the aggregator's
// "denominator = recs with the relevant data" rule.
type aggregationRow struct {
	Key         string   `json:"key"`
	Count       int64    `json:"count"`
	AvgT1       *float64 `json:"avg_t1_return"`    // nil when no rec in the group has a T+1 return
	AvgT3       *float64 `json:"avg_t3_return"`    // nil when no rec in the group has a T+3 return
	AvgT5       *float64 `json:"avg_t5_return"`    // nil when no rec in the group has a T+5 return
	WinRateT5   *float64 `json:"win_rate_t5"`      // nil when no rec has T+5; denominator is "recs with known T+5"
	AvgDrawdown *float64 `json:"avg_max_drawdown"` // nil when no rec has a drawdown (>=2 finite closes)
}

// aggregateKey is the per-(rec, group) tuple we use during the
// in-memory walk. We need the rec-id so session_tag grouping can
// fan-out to one row per tag.
type aggregateKey struct {
	group string // the user-facing group key (strategy / stock / tag)
	recID uint64 // recommendation id, used as a tiebreaker
}

func (h *Handler) aggregate(ctx context.Context, q AggregationsQuery, from, to time.Time) ([]aggregationRow, int64, error) {
	if h.svc.db == nil {
		return nil, 0, apperr.Unavailable("perf: db not configured")
	}

	// joined carries the per-snapshot row plus the rec fields we
	// need for grouping. The struct is flat (no nested Snap) so
	// GORM Scan treats every field as a primitive column and
	// doesn't infer a relation on the embedded struct. The SELECT
	// is the narrow projection: only 4 columns from
	// performance_snapshots (T1Return / T3Return / T5Return /
	// MaxDrawdown — the aggregator's only reads) plus 3 rec-level
	// aliases. We do NOT pull the 10 other columns of
	// performance_snapshots or scan into a full
	// model.PerformanceSnapshot. At 1M rows the wire footprint
	// and the struct-copy pass are both ~70% smaller.
	type joined struct {
		RecID        uint64
		StrategyCode string
		StockCode    string
		T1Return     *float64
		T3Return     *float64
		T5Return     *float64
		MaxDrawdown  *float64
	}

	// 1. Load snapshots + rec fields, sorted so the latest per rec
	// is encountered first. Then dedupe in the walk below.
	//
	// Aliasing: `ps.id` (snapshot row's PK) is NOT selected; we
	// use `r.id AS rec_id` (recommendation's PK) for the dedupe
	// key. `ps.recommendation_id` is also omitted — it's the same
	// value as r.id. Don't add `ps.recommendation_id` back into
	// the SELECT without also adding a RecommendationID field to
	// the joined struct.
	var rows []joined
	q1 := h.svc.db.WithContext(ctx).
		Table("performance_snapshots ps").
		Select(`ps.t1_return, ps.t3_return, ps.t5_return, ps.max_drawdown,
		       r.id AS rec_id, r.strategy_code AS strategy_code, r.stock_code AS stock_code`).
		Joins("JOIN recommendations r ON r.id = ps.recommendation_id")
	if !from.IsZero() {
		q1 = q1.Where("r.date >= ?", from)
	}
	if !to.IsZero() {
		q1 = q1.Where("r.date <= ?", to)
	}
	if err := q1.Order("ps.recommendation_id ASC, ps.snapshot_date DESC, ps.id DESC").Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	// 2. Keep only the latest snapshot per rec.
	latest := make([]joined, 0, len(rows))
	seen := make(map[uint64]bool, len(rows))
	for _, j := range rows {
		if seen[j.RecID] {
			continue
		}
		seen[j.RecID] = true
		latest = append(latest, j)
	}

	// 3. For session_tag, we need a rec -> [tag] map. Single query,
	// scoped to the recs we kept.
	var tagsByRec map[uint64][]string
	if q.GroupBy == "session_tag" && len(latest) > 0 {
		recIDs := make([]uint64, 0, len(latest))
		for _, j := range latest {
			recIDs = append(recIDs, j.RecID)
		}
		var tags []model.RecommendationTag
		if err := h.svc.db.WithContext(ctx).
			Where("recommendation_id IN ?", recIDs).
			Find(&tags).Error; err != nil {
			return nil, 0, err
		}
		tagsByRec = make(map[uint64][]string, len(latest))
		for _, t := range tags {
			tagsByRec[t.RecommendationID] = append(tagsByRec[t.RecommendationID], t.Tag)
		}
	}

	// 4. Group + aggregate. We expand the latest row per tag when
	// group_by=session_tag (a rec with 2 tags contributes to 2
	// groups; without a tag, it falls into UNGROUPED).
	groups := make(map[string]*groupAccum)
	var keys []string
	for _, j := range latest {
		groupKeys := h.groupKeysFor(q.GroupBy, j.RecID, j.StrategyCode, j.StockCode, tagsByRec)
		for _, gk := range groupKeys {
			g, ok := groups[gk]
			if !ok {
				g = &groupAccum{key: gk}
				groups[gk] = g
				keys = append(keys, gk)
			}
			g.add(aggregateCol{
				T1Return:    j.T1Return,
				T3Return:    j.T3Return,
				T5Return:    j.T5Return,
				MaxDrawdown: j.MaxDrawdown,
			})
		}
	}

	// 5. Sort: by count desc, then key asc.
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := groups[keys[i]], groups[keys[j]]
		if a.count != b.count {
			return a.count > b.count
		}
		return a.key < b.key
	})

	total := int64(len(keys))
	offset := (q.Page - 1) * q.PageSize
	if offset > len(keys) {
		offset = len(keys)
	}
	end := offset + q.PageSize
	if end > len(keys) {
		end = len(keys)
	}
	out := make([]aggregationRow, 0, end-offset)
	for _, k := range keys[offset:end] {
		out = append(out, groups[k].snapshot())
	}
	return out, total, nil
}

// groupKeysFor returns every group key the given rec contributes to.
// For strategy/stock it's exactly one. For session_tag it's one per
// tag, falling back to UNGROUPED when the rec has no tags. The
// snapshot itself is not part of the key — only the rec-level
// fields (strategy_code / stock_code) and the rec-id (for tag
// lookup) are.
func (h *Handler) groupKeysFor(groupBy string, recID uint64, strategyCode, stockCode string, tagsByRec map[uint64][]string) []string {
	switch groupBy {
	case "strategy":
		k := strategyCode
		if k == "" {
			return []string{"UNGROUPED"}
		}
		return []string{k}
	case "stock":
		k := stockCode
		if k == "" {
			return []string{"UNGROUPED"}
		}
		return []string{k}
	case "session_tag":
		tags := tagsByRec[recID]
		if len(tags) == 0 {
			return []string{"UNGROUPED"}
		}
		out := make([]string, 0, len(tags))
		for _, t := range tags {
			if t == "" {
				continue
			}
			out = append(out, t)
		}
		if len(out) == 0 {
			return []string{"UNGROUPED"}
		}
		return out
	}
	return []string{"UNGROUPED"}
}

// aggregateCol is the narrow projection the aggregator reads from
// each joined row. It carries only the four numeric fields the
// aggregator consumes (T1Return / T3Return / T5Return / MaxDrawdown)
// — the rec-level fields (RecID / StrategyCode / StockCode) are
// consumed by the grouping step, not by groupAccum.add, so they
// stay on `joined` and are not duplicated here. Each call to
// add() takes a fresh aggregateCol built from a single joined row.
type aggregateCol struct {
	T1Return    *float64
	T3Return    *float64
	T5Return    *float64
	MaxDrawdown *float64
}

// groupAccum is a running aggregate over the latest snapshots
// contributing to one group. We track the count + sum for each
// numeric field; means are computed in snapshot().
type groupAccum struct {
	key   string
	count int64
	// Running sums over the finite entries only. We split into sum
	// + count to handle the "nil = no data" case without losing
	// precision.
	sumT1, nT1                float64
	sumT3, nT3                float64
	sumT5, nT5                float64
	sumDD, nDD                float64
	sumWin, nWin              float64 // win is 1.0 / 0.0 per t5 hit
}

func (g *groupAccum) add(snap aggregateCol) {
	g.count++
	if snap.T1Return != nil {
		g.sumT1 += *snap.T1Return
		g.nT1++
	}
	if snap.T3Return != nil {
		g.sumT3 += *snap.T3Return
		g.nT3++
	}
	if snap.T5Return != nil {
		g.sumT5 += *snap.T5Return
		g.nT5++
		if *snap.T5Return > 0 {
			g.sumWin += 1.0
		} else {
			g.sumWin += 0.0
		}
		g.nWin++
	}
	if snap.MaxDrawdown != nil {
		g.sumDD += *snap.MaxDrawdown
		g.nDD++
	}
}

func (g *groupAccum) snapshot() aggregationRow {
	out := aggregationRow{Key: g.key, Count: g.count}
	if g.nT1 > 0 {
		v := g.sumT1 / g.nT1
		out.AvgT1 = &v
	}
	if g.nT3 > 0 {
		v := g.sumT3 / g.nT3
		out.AvgT3 = &v
	}
	if g.nT5 > 0 {
		v := g.sumT5 / g.nT5
		out.AvgT5 = &v
	}
	if g.nWin > 0 {
		v := g.sumWin / g.nWin
		out.WinRateT5 = &v
	}
	if g.nDD > 0 {
		v := g.sumDD / g.nDD
		out.AvgDrawdown = &v
	}
	return out
}

// =============================================================================
// Small helpers
// =============================================================================

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
