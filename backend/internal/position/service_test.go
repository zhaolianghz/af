// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package position

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/model"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "pos.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, model.Migrate(db))
	return db
}

// fakeQuote returns fixed prices per code; codes absent -> error.
type fakeQuote struct{ prices map[string]float64 }

func (f *fakeQuote) GetQuote(_ context.Context, code string) (*datasource.Quote, error) {
	if p, ok := f.prices[code]; ok {
		return &datasource.Quote{StockCode: code, Price: p}, nil
	}
	return nil, errors.New("no quote")
}

func TestPosition_CreateValidation(t *testing.T) {
	s := NewService(newDB(t), nil)
	ctx := context.Background()
	_, err := s.Create(ctx, CreateInput{StockCode: "", CostPrice: 10, Quantity: 100})
	require.Error(t, err)
	_, err = s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 0, Quantity: 100})
	require.Error(t, err)
	_, err = s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 10, Quantity: 0})
	require.Error(t, err)
}

func TestPosition_CreateListPnL(t *testing.T) {
	db := newDB(t)
	q := &fakeQuote{prices: map[string]float64{"600519.SH": 120.0}}
	s := NewService(db, q)
	ctx := context.Background()

	_, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", StockName: "茅台", CostPrice: 100, Quantity: 200, SourceRecommendationID: 7})
	require.NoError(t, err)

	views, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, views, 1)
	v := views[0]
	require.Equal(t, 100.0*200, v.CostValue)
	require.NotNil(t, v.CurrentPrice)
	require.Equal(t, 120.0, *v.CurrentPrice)
	require.NotNil(t, v.MarketValue)
	require.Equal(t, 120.0*200, *v.MarketValue)
	require.NotNil(t, v.PnL)
	require.Equal(t, (120.0-100.0)*200, *v.PnL) // +4000
	require.NotNil(t, v.PnLPct)
	require.InDelta(t, 0.20, *v.PnLPct, 1e-9)

	sum := Summarize(views)
	require.Equal(t, 1, sum.Count)
	require.Equal(t, 1, sum.PricedCount)
	require.Equal(t, 20000.0, sum.TotalCostValue)
	require.NotNil(t, sum.TotalPnL)
	require.Equal(t, 4000.0, *sum.TotalPnL)
}

func TestPosition_QuoteFailureLeavesPriceNil(t *testing.T) {
	db := newDB(t)
	q := &fakeQuote{prices: map[string]float64{}} // every quote fails
	s := NewService(db, q)
	ctx := context.Background()
	_, err := s.Create(ctx, CreateInput{StockCode: "000001.SZ", CostPrice: 10, Quantity: 100})
	require.NoError(t, err)
	views, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Nil(t, views[0].CurrentPrice) // unavailable, not 0
	require.Nil(t, views[0].PnL)
	require.Equal(t, 1000.0, views[0].CostValue) // cost still shown
	sum := Summarize(views)
	require.Equal(t, 0, sum.PricedCount)
	require.Nil(t, sum.TotalPnL)
}

func TestPosition_UpdateAndClose(t *testing.T) {
	db := newDB(t)
	s := NewService(db, nil)
	ctx := context.Background()
	p, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 100, Quantity: 100})
	require.NoError(t, err)

	newCost := 90.0
	newQty := int64(150)
	upd, err := s.Update(ctx, p.ID, UpdateInput{CostPrice: &newCost, Quantity: &newQty})
	require.NoError(t, err)
	require.Equal(t, 90.0, upd.CostPrice)
	require.Equal(t, int64(150), upd.Quantity)

	require.NoError(t, s.Close(ctx, p.ID))
	views, err := s.List(ctx)
	require.NoError(t, err)
	require.Empty(t, views) // soft-deleted, gone from list

	require.Error(t, s.Close(ctx, p.ID)) // already gone
}

func TestPosition_UpdateValidationAndNotFound(t *testing.T) {
	s := NewService(newDB(t), nil)
	ctx := context.Background()
	p, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 100, Quantity: 100})
	require.NoError(t, err)

	bad := 0.0
	_, err = s.Update(ctx, p.ID, UpdateInput{CostPrice: &bad})
	require.Error(t, err, "cost_price <= 0 must be rejected")
	badQty := int64(0)
	_, err = s.Update(ctx, p.ID, UpdateInput{Quantity: &badQty})
	require.Error(t, err, "quantity <= 0 must be rejected")
	_, err = s.Update(ctx, 999999, UpdateInput{Note: strptr("x")})
	require.Error(t, err, "unknown id must be NotFound")
}

func TestPosition_PartialUpdateLeavesOthers(t *testing.T) {
	s := NewService(newDB(t), nil)
	ctx := context.Background()
	p, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 100, Quantity: 100, Note: "orig"})
	require.NoError(t, err)

	// Only update the note; cost + quantity must remain.
	upd, err := s.Update(ctx, p.ID, UpdateInput{Note: strptr("changed")})
	require.NoError(t, err)
	require.Equal(t, "changed", upd.Note)
	require.Equal(t, 100.0, upd.CostPrice)
	require.Equal(t, int64(100), upd.Quantity)
}

func TestPosition_NilDBUnavailable(t *testing.T) {
	s := NewService(nil, nil)
	ctx := context.Background()
	_, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 1, Quantity: 1})
	require.Error(t, err)
	_, err = s.Update(ctx, 1, UpdateInput{})
	require.Error(t, err)
	require.Error(t, s.Close(ctx, 1))
	_, err = s.List(ctx)
	require.Error(t, err)
}

func TestPosition_NilQuoteSourceLeavesPriceNil(t *testing.T) {
	// Distinct from QuoteFailure: here the source itself is nil (not
	// configured), so we never even attempt a quote.
	s := NewService(newDB(t), nil)
	ctx := context.Background()
	_, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 10, Quantity: 100})
	require.NoError(t, err)
	views, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Nil(t, views[0].CurrentPrice)
	require.Nil(t, views[0].MarketValue)
	require.Nil(t, views[0].PnL)
	require.Equal(t, 1000.0, views[0].CostValue)
}

func TestPosition_ListOrderingDesc(t *testing.T) {
	db := newDB(t)
	s := NewService(db, nil)
	ctx := context.Background()
	_, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 10, Quantity: 100})
	require.NoError(t, err)
	_, err = s.Create(ctx, CreateInput{StockCode: "000001.SZ", CostPrice: 10, Quantity: 100})
	require.NoError(t, err)
	views, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, views, 2)
	// id DESC → most recently created first.
	require.Equal(t, "000001.SZ", views[0].StockCode)
	require.Equal(t, "600519.SH", views[1].StockCode)
}

func TestPosition_SummarizeMixedPriced(t *testing.T) {
	db := newDB(t)
	// Only 600519 has a price; 000001 quote fails → unpriced.
	q := &fakeQuote{prices: map[string]float64{"600519.SH": 120.0}}
	s := NewService(db, q)
	ctx := context.Background()
	_, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 100, Quantity: 200}) // cost 20000, mv 24000
	require.NoError(t, err)
	_, err = s.Create(ctx, CreateInput{StockCode: "000001.SZ", CostPrice: 10, Quantity: 100}) // cost 1000, unpriced
	require.NoError(t, err)

	views, err := s.List(ctx)
	require.NoError(t, err)
	sum := Summarize(views)
	require.Equal(t, 2, sum.Count)
	require.Equal(t, 1, sum.PricedCount, "only the priced one counts toward market totals")
	require.Equal(t, 21000.0, sum.TotalCostValue, "total cost spans ALL positions")
	require.NotNil(t, sum.TotalMarketValue)
	require.Equal(t, 24000.0, *sum.TotalMarketValue)
	require.NotNil(t, sum.TotalPnL)
	// PnL is apples-to-apples: market(24000) - cost-of-PRICED(20000) = 4000,
	// NOT 24000 - 21000. The unpriced position's cost is excluded.
	require.Equal(t, 4000.0, *sum.TotalPnL)
}

func TestPosition_OpenedAtTruncatedToDay(t *testing.T) {
	s := NewService(newDB(t), nil)
	ctx := context.Background()
	ts := time.Date(2026, 6, 25, 14, 37, 11, 0, time.UTC)
	p, err := s.Create(ctx, CreateInput{StockCode: "600519.SH", CostPrice: 10, Quantity: 100, OpenedAt: &ts})
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), p.OpenedAt.UTC())
}

func strptr(s string) *string { return &s }
