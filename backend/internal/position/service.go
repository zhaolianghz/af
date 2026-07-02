// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package position implements the user-tracked holdings ledger (§10).
// Positions are hand-maintained (cost/quantity); current price and
// P&L are computed on read from the live datasource and never stored.
package position

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/skyzhao/af/internal/apperr"
	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/model"
)

// QuoteSource is the slice of the datasource manager this package
// needs — just the latest price. Narrowed for testability.
type QuoteSource interface {
	GetQuote(ctx context.Context, stockCode string) (*datasource.Quote, error)
}

// Service is the positions use-case layer.
type Service struct {
	db    *gorm.DB
	quote QuoteSource
}

// NewService wires the service. quote may be nil (P&L fields then
// stay zero / current_price null).
func NewService(db *gorm.DB, quote QuoteSource) *Service {
	return &Service{db: db, quote: quote}
}

// View is a Position enriched with live price + P&L. The P&L fields
// are pointers so "price unavailable" (nil) is distinct from "0".
type View struct {
	model.Position
	CurrentPrice *float64 `json:"current_price"`
	MarketValue  *float64 `json:"market_value"`
	CostValue    float64  `json:"cost_value"`
	PnL          *float64 `json:"pnl"`
	PnLPct       *float64 `json:"pnl_pct"`
}

// CreateInput is the body for marking/adding a position.
type CreateInput struct {
	StockCode              string
	StockName              string
	CostPrice              float64
	Quantity               int64
	OpenedAt               *time.Time
	SourceRecommendationID uint64
	Note                   string
}

// Create inserts a new position. Validates code/cost/quantity.
func (s *Service) Create(ctx context.Context, in CreateInput) (*model.Position, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("position: db not configured")
	}
	if in.StockCode == "" {
		return nil, apperr.InvalidArg("stock_code is required")
	}
	if in.CostPrice <= 0 {
		return nil, apperr.InvalidArg("cost_price must be > 0")
	}
	if in.Quantity <= 0 {
		return nil, apperr.InvalidArg("quantity must be > 0")
	}
	opened := time.Now().UTC().Truncate(24 * time.Hour)
	if in.OpenedAt != nil {
		opened = in.OpenedAt.UTC().Truncate(24 * time.Hour)
	}
	p := &model.Position{
		StockCode:              in.StockCode,
		StockName:              in.StockName,
		CostPrice:              in.CostPrice,
		Quantity:               in.Quantity,
		OpenedAt:               opened,
		SourceRecommendationID: in.SourceRecommendationID,
		Note:                   in.Note,
	}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "position: create", err)
	}
	return p, nil
}

// UpdateInput carries the hand-maintainable fields. Nil = leave as-is.
type UpdateInput struct {
	CostPrice *float64
	Quantity  *int64
	Note      *string
}

// Update edits cost/quantity/note of an open position.
func (s *Service) Update(ctx context.Context, id uint64, in UpdateInput) (*model.Position, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("position: db not configured")
	}
	var p model.Position
	if err := s.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, apperr.NotFound("position not found")
	}
	if in.CostPrice != nil {
		if *in.CostPrice <= 0 {
			return nil, apperr.InvalidArg("cost_price must be > 0")
		}
		p.CostPrice = *in.CostPrice
	}
	if in.Quantity != nil {
		if *in.Quantity <= 0 {
			return nil, apperr.InvalidArg("quantity must be > 0")
		}
		p.Quantity = *in.Quantity
	}
	if in.Note != nil {
		p.Note = *in.Note
	}
	if err := s.db.WithContext(ctx).Save(&p).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "position: update", err)
	}
	return &p, nil
}

// Close soft-deletes a position (the user sold / stopped tracking it).
func (s *Service) Close(ctx context.Context, id uint64) error {
	if s.db == nil {
		return apperr.Unavailable("position: db not configured")
	}
	res := s.db.WithContext(ctx).Delete(&model.Position{}, id)
	if res.Error != nil {
		return apperr.Wrap(apperr.CodeInternal, "position: close", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.NotFound("position not found")
	}
	return nil
}

// List returns all open positions enriched with live price + P&L.
// A per-stock quote failure leaves that row's price fields nil (the
// position still shows, with cost data) — one bad quote never fails
// the whole list.
func (s *Service) List(ctx context.Context) ([]View, error) {
	if s.db == nil {
		return nil, apperr.Unavailable("position: db not configured")
	}
	var rows []model.Position
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&rows).Error; err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "position: list", err)
	}
	out := make([]View, 0, len(rows))
	for _, p := range rows {
		v := View{Position: p, CostValue: p.CostPrice * float64(p.Quantity)}
		if s.quote != nil {
			if q, err := s.quote.GetQuote(ctx, p.StockCode); err == nil && q != nil && q.Price > 0 {
				price := q.Price
				mv := price * float64(p.Quantity)
				pnl := mv - v.CostValue
				v.CurrentPrice = &price
				v.MarketValue = &mv
				v.PnL = &pnl
				if v.CostValue > 0 {
					pct := pnl / v.CostValue
					v.PnLPct = &pct
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Summary aggregates the open book: total cost, total market value,
// total P&L. Market totals are over positions with a known price.
type Summary struct {
	Count            int      `json:"count"`
	TotalCostValue   float64  `json:"total_cost_value"`
	TotalMarketValue *float64 `json:"total_market_value"`
	TotalPnL         *float64 `json:"total_pnl"`
	PricedCount      int      `json:"priced_count"`
}

// Summarize folds a View slice into a Summary.
func Summarize(views []View) Summary {
	s := Summary{Count: len(views)}
	var mv float64
	priced := false
	for _, v := range views {
		s.TotalCostValue += v.CostValue
		if v.MarketValue != nil {
			mv += *v.MarketValue
			s.PricedCount++
			priced = true
		}
	}
	if priced {
		pnl := mv - costOfPriced(views)
		s.TotalMarketValue = &mv
		s.TotalPnL = &pnl
	}
	return s
}

// costOfPriced sums the cost basis of only the priced positions, so
// total_pnl = total_market_value - cost-of-priced is apples-to-apples.
func costOfPriced(views []View) float64 {
	var c float64
	for _, v := range views {
		if v.MarketValue != nil {
			c += v.CostValue
		}
	}
	return c
}
