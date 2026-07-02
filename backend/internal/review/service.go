// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package review implements §14.9 automated daily/weekly performance
// reviews: gather the period's recommendations + their T+N outcomes,
// compute headline metrics, and have the AI assistant summarize them
// into a narrative report stored for the UI.
package review

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/model"
)

// Summarizer is the slice of the AI client this package needs.
type Summarizer interface {
	Summarize(ctx context.Context, system, user string) (string, error)
	Name() string
}

// pnlRow is one stock's latest T+5 outcome, used for the summary prompt.
type pnlRow struct {
	code string
	t5   *float64
}

// Service generates and serves review reports.
type Service struct {
	db  *gorm.DB
	llm Summarizer
}

// NewService wires the service. llm may be nil → reports get a
// data-only summary (no narrative).
func NewService(db *gorm.DB, llm Summarizer) *Service {
	return &Service{db: db, llm: llm}
}

const reviewSystemPrompt = `你是 A 股量化选股的复盘分析师。根据给定的某时段推荐与其 T+N 收益数据,
写一段简洁的中文复盘:命中率与平均收益怎么样、表现最好/最差的标的、可能的归因、下一步注意点。
不超过 300 字,不要编造数据里没有的数字。`

// Generate builds a review for [periodStart, periodEnd) of the given
// kind ("daily"/"weekly") and persists it. It is idempotent-friendly:
// callers may dedupe on (kind, period_start) if desired.
func (s *Service) Generate(ctx context.Context, kind string, periodStart, periodEnd time.Time) (*model.ReviewReport, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("review: db not configured")
	}
	ps := periodStart.UTC().Truncate(24 * time.Hour)
	pe := periodEnd.UTC().Truncate(24 * time.Hour)
	if !pe.After(ps) {
		return nil, apperr.InvalidArg("period_end must be after period_start")
	}

	var recs []model.Recommendation
	if err := s.db.WithContext(ctx).
		Where("date >= ? AND date < ?", ps, pe).
		Order("id").Find(&recs).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "review: load recs", err)
	}

	rep := &model.ReviewReport{
		Kind: kind, PeriodStart: ps, PeriodEnd: pe,
		RecommendationCount: len(recs),
	}

	// Latest snapshot per rec → T5 metrics + per-stock rows for the
	// LLM prompt.
	rows := make([]pnlRow, 0, len(recs))
	var t5Sum float64
	var t5Known, t5Wins int
	if len(recs) > 0 {
		ids := make([]uint64, len(recs))
		codeByRec := make(map[uint64]string, len(recs))
		for i, r := range recs {
			ids[i] = r.ID
			codeByRec[r.ID] = r.StockCode
		}
		var snaps []model.PerformanceSnapshot
		if err := s.db.WithContext(ctx).
			Where("recommendation_id IN ?", ids).
			Order("recommendation_id, snapshot_date DESC").
			Find(&snaps).Error; err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "review: load snapshots", err)
		}
		latest := make(map[uint64]model.PerformanceSnapshot, len(snaps))
		for _, sn := range snaps {
			if _, ok := latest[sn.RecommendationID]; !ok {
				latest[sn.RecommendationID] = sn // first = latest (DESC)
			}
		}
		for _, r := range recs {
			rr := pnlRow{code: r.StockCode}
			if sn, ok := latest[r.ID]; ok && sn.T5Return != nil {
				rr.t5 = sn.T5Return
				t5Sum += *sn.T5Return
				t5Known++
				if *sn.T5Return > 0 {
					t5Wins++
				}
			}
			rows = append(rows, rr)
		}
	}
	if t5Known > 0 {
		wr := float64(t5Wins) / float64(t5Known)
		avg := t5Sum / float64(t5Known)
		rep.WinRateT5 = &wr
		rep.AvgT5Return = &avg
	}

	rep.Summary, rep.LLM = s.summarize(ctx, kind, ps, pe, rep, rows)

	if err := s.db.WithContext(ctx).Create(rep).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "review: persist", err)
	}
	return rep, nil
}

// summarize builds the data prompt and asks the LLM. With no LLM (or on
// error) it falls back to the structured data block so a report always
// has a body.
func (s *Service) summarize(ctx context.Context, kind string, ps, pe time.Time, rep *model.ReviewReport, rows []pnlRow) (string, string) {
	dataBlock := buildDataBlock(kind, ps, pe, rep, rows)
	if s.llm == nil {
		return dataBlock, "none"
	}
	out, err := s.llm.Summarize(ctx, reviewSystemPrompt, dataBlock)
	if err != nil || strings.TrimSpace(out) == "" {
		return dataBlock, "none"
	}
	return out, s.llm.Name()
}

func buildDataBlock(kind string, ps, pe time.Time, rep *model.ReviewReport, rows []pnlRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "复盘类型: %s\n时段: %s ~ %s\n推荐数: %d\n",
		kind, ps.Format("2006-01-02"), pe.AddDate(0, 0, -1).Format("2006-01-02"), rep.RecommendationCount)
	if rep.WinRateT5 != nil {
		fmt.Fprintf(&b, "T+5 命中率: %.1f%%\nT+5 平均收益: %.2f%%\n",
			*rep.WinRateT5*100, *rep.AvgT5Return*100)
	} else {
		b.WriteString("T+5 数据尚未就绪(推荐未满 5 个交易日)\n")
	}
	// Top/bottom by T5.
	withT5 := make([]pnlRow, 0, len(rows))
	for _, r := range rows {
		if r.t5 != nil {
			withT5 = append(withT5, r)
		}
	}
	sort.SliceStable(withT5, func(i, j int) bool { return *withT5[i].t5 > *withT5[j].t5 })
	if len(withT5) > 0 {
		b.WriteString("个股 T+5:\n")
		for _, r := range withT5 {
			fmt.Fprintf(&b, "  %s: %.2f%%\n", r.code, *r.t5*100)
		}
	}
	return b.String()
}

// List returns recent reports, newest first, optionally filtered by
// kind. limit defaults to 30, capped at 200.
func (s *Service) List(ctx context.Context, kind string, limit int) ([]model.ReviewReport, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("review: db not configured")
	}
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	q := s.db.WithContext(ctx).Model(&model.ReviewReport{}).Order("id DESC").Limit(limit)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var out []model.ReviewReport
	if err := q.Find(&out).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "review: list", err)
	}
	return out, nil
}

// Get returns a single report by id.
func (s *Service) Get(ctx context.Context, id uint64) (*model.ReviewReport, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("review: db not configured")
	}
	var r model.ReviewReport
	if err := s.db.WithContext(ctx).First(&r, id).Error; err != nil {
		return nil, apperr.NotFound("review report not found")
	}
	return &r, nil
}

// GenerateDaily builds a review for the trading day that ended today
// (window = [today, tomorrow)). Convenience for the scheduler + manual
// trigger.
func (s *Service) GenerateDaily(ctx context.Context, now time.Time) (*model.ReviewReport, error) {
	day := now.UTC().Truncate(24 * time.Hour)
	return s.Generate(ctx, model.ReviewKindDaily, day, day.AddDate(0, 0, 1))
}

// GenerateWeekly builds a review for the trailing 7 days ending today.
func (s *Service) GenerateWeekly(ctx context.Context, now time.Time) (*model.ReviewReport, error) {
	end := now.UTC().Truncate(24 * time.Hour).AddDate(0, 0, 1)
	return s.Generate(ctx, model.ReviewKindWeekly, end.AddDate(0, 0, -7), end)
}
