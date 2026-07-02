// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for DataSourceNode — verifies the dry-run path, the four
// subtypes against a fake datasource.Manager, and parameter
// validation. The fake manager lets us assert call args without
// standing up a real data source.
package nodes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/orchestrator"
)

// fakeDS is a stand-in for datasource.Manager. It records every
// call so we can assert on the args.
type fakeDS struct {
	quoteCalls []string
	klineCalls []klineCall
	fundCalls  []string
	newsCalls  []newsCall

	quoteOut map[string]*datasource.Quote
	quoteErr map[string]error
	klineOut map[string][]datasource.KLine
	klineErr map[string]error
	fundOut  map[string]*datasource.Fundamental
	fundErr  map[string]error
	newsOut  map[string][]datasource.News
	newsErr  map[string]error
}

type klineCall struct {
	Code   string
	Period string
	Start  time.Time
	End    time.Time
}
type newsCall struct {
	Code  string
	Limit int
}

func (f *fakeDS) GetQuote(ctx context.Context, code string) (*datasource.Quote, error) {
	f.quoteCalls = append(f.quoteCalls, code)
	if e, ok := f.quoteErr[code]; ok {
		return nil, e
	}
	if f.quoteOut == nil {
		return nil, nil
	}
	return f.quoteOut[code], nil
}

func (f *fakeDS) GetKLine(ctx context.Context, code, period string, start, end time.Time) ([]datasource.KLine, error) {
	f.klineCalls = append(f.klineCalls, klineCall{Code: code, Period: period, Start: start, End: end})
	if e, ok := f.klineErr[code]; ok {
		return nil, e
	}
	if f.klineOut == nil {
		return nil, nil
	}
	return f.klineOut[code], nil
}

func (f *fakeDS) GetFundamental(ctx context.Context, code string) (*datasource.Fundamental, error) {
	f.fundCalls = append(f.fundCalls, code)
	if e, ok := f.fundErr[code]; ok {
		return nil, e
	}
	if f.fundOut == nil {
		return nil, nil
	}
	return f.fundOut[code], nil
}

func (f *fakeDS) GetNews(ctx context.Context, code string, limit int) ([]datasource.News, error) {
	f.newsCalls = append(f.newsCalls, newsCall{Code: code, Limit: limit})
	if e, ok := f.newsErr[code]; ok {
		return nil, e
	}
	if f.newsOut == nil {
		return nil, nil
	}
	return f.newsOut[code], nil
}

func (f *fakeDS) ListSources() []string { return []string{"fake"} }

func (f *fakeDS) BreakerSnapshots() []datasource.BreakerSnapshot { return nil }

func (f *fakeDS) HealthCheck(ctx context.Context) error { return nil }

func newDataSourceRC(ds datasource.Manager) *orchestrator.RunContext {
	return orchestrator.NewRunContext(orchestrator.RunContextOptions{
		DataSource: ds,
		Logger:     zap.NewNop(),
		Clock:      func() time.Time { return time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC) },
	})
}

// =============================================================================
// Validation
// =============================================================================

func TestDataSource_InvalidParamsJSON(t *testing.T) {
	n := NewDataSourceNode()
	in := map[string]any{orchestrator.InputKeyParams: []byte(`{not json`)}
	_, err := n.Run(context.Background(), newDataSourceRC(&fakeDS{}), in)
	require.Error(t, err)
}

func TestDataSource_MissingStockCodes(t *testing.T) {
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "quote",
			StockCodes: nil,
		}),
	}
	_, err := n.Run(context.Background(), newDataSourceRC(&fakeDS{}), in)
	require.Error(t, err)
}

func TestDataSource_UnknownSubtype(t *testing.T) {
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "bogus",
			StockCodes: []string{"600000.SH"},
		}),
	}
	_, err := n.Run(context.Background(), newDataSourceRC(&fakeDS{}), in)
	require.Error(t, err)
}

// =============================================================================
// Dry-run (no datasource.Manager)
// =============================================================================

func TestDataSource_DryRun_NoManager(t *testing.T) {
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "quote",
			StockCodes: []string{"600000.SH"},
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(nil), in)
	require.NoError(t, err)
	require.Equal(t, true, out["dry_run"])
	require.Equal(t, []string{"600000.SH"}, out["stock_codes"])
}

func TestDataSource_DryRun_NilRC(t *testing.T) {
	// Defensive: rc itself is nil.
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "quote",
			StockCodes: []string{"600000.SH"},
		}),
	}
	out, err := n.Run(context.Background(), nil, in)
	require.NoError(t, err)
	require.Equal(t, true, out["dry_run"])
}

// =============================================================================
// Quote subtype
// =============================================================================

func TestDataSource_Quote(t *testing.T) {
	ds := &fakeDS{
		quoteOut: map[string]*datasource.Quote{
			"600000.SH": {StockCode: "600000.SH", Price: 10.0},
		},
	}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "quote",
			StockCodes: []string{"600000.SH"},
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"])
	require.Equal(t, []string{"600000.SH"}, ds.quoteCalls)
}

func TestDataSource_Quote_DefaultSubtype(t *testing.T) {
	// Empty subtype defaults to "quote".
	ds := &fakeDS{
		quoteOut: map[string]*datasource.Quote{"600000.SH": {}},
	}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "",
			StockCodes: []string{"600000.SH"},
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"])
}

func TestDataSource_Quote_FailSoft(t *testing.T) {
	// A single failure should not stop the whole run.
	ds := &fakeDS{
		quoteOut: map[string]*datasource.Quote{
			"000001.SZ": {StockCode: "000001.SZ"},
		},
		quoteErr: map[string]error{
			"600000.SH": errors.New("timeout"),
		},
	}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "quote",
			StockCodes: []string{"600000.SH", "000001.SZ"},
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"]) // one good, one failed
}

// =============================================================================
// Kline subtype
// =============================================================================

func TestDataSource_KLine(t *testing.T) {
	ds := &fakeDS{
		klineOut: map[string][]datasource.KLine{
			"600000.SH": {{StockCode: "600000.SH", Close: 10.0}},
		},
	}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "kline",
			StockCodes: []string{"600000.SH"},
			Period:     "1d",
			Days:       10,
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"])
	require.Equal(t, "1d", out["period"])
	require.Len(t, ds.klineCalls, 1)
	require.Equal(t, "600000.SH", ds.klineCalls[0].Code)
	require.Equal(t, "1d", ds.klineCalls[0].Period)
}

func TestDataSource_KLine_DefaultPeriodAndDays(t *testing.T) {
	ds := &fakeDS{}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "kline",
			StockCodes: []string{"600000.SH"},
		}),
	}
	_, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, "1d", ds.klineCalls[0].Period)
	// 30 days default → start is 30 days before clock.
	start := ds.klineCalls[0].Start
	end := ds.klineCalls[0].End
	require.Equal(t, 30*24*time.Hour, end.Sub(start))
}

// =============================================================================
// Fundamental subtype
// =============================================================================

func TestDataSource_Fundamental(t *testing.T) {
	ds := &fakeDS{
		fundOut: map[string]*datasource.Fundamental{
			"600000.SH": {StockCode: "600000.SH", PE: 12.5, PB: 1.1, ROE: 11.0, DividendYield: 4.2},
		},
	}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "fundamental",
			StockCodes: []string{"600000.SH"},
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"])
	// Must emit per-stock ROWS as []any of maps (the filter contract),
	// NOT a typed []datasource.Fundamental which findFirstSlice can't read.
	items, ok := out["items"].([]any)
	require.True(t, ok, "fundamental output must expose items as []any")
	require.Len(t, items, 1)
	row := items[0].(map[string]any)
	require.Equal(t, "600000.SH", row["stock_code"])
	require.Equal(t, 12.5, row["pe"])
	require.Equal(t, 1.1, row["pb"])
	require.Equal(t, 11.0, row["roe"])
	require.Equal(t, 4.2, row["dividend_yield"])
}

// =============================================================================
// News subtype
// =============================================================================

func TestDataSource_News(t *testing.T) {
	ds := &fakeDS{
		newsOut: map[string][]datasource.News{
			"600000.SH": {{StockCode: "600000.SH", Title: "A"}},
		},
	}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "news",
			StockCodes: []string{"600000.SH"},
			NewsLimit:  5,
		}),
	}
	out, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 1, out["count"])
	require.Equal(t, 5, ds.newsCalls[0].Limit)
	// News must emit the {items:[]any of map} envelope (NOT a typed
	// []News), so a filter/notify node directly downstream — which uses
	// findFirstSlice ([]any only) — actually sees the rows. This is the
	// news bug fix: a typed slice silently matched zero.
	items, ok := out["items"].([]any)
	require.True(t, ok, "news must emit []any items, not a typed slice")
	require.Len(t, items, 1)
	row, ok := items[0].(map[string]any)
	require.True(t, ok, "each news item must be map[string]any")
	require.Equal(t, "600000.SH", row["stock_code"])
	require.Equal(t, "A", row["title"])
	require.Nil(t, out["news"], "the old typed `news` key must be gone")
}

func TestDataSource_News_DefaultLimit(t *testing.T) {
	ds := &fakeDS{}
	n := NewDataSourceNode()
	in := map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "news",
			StockCodes: []string{"600000.SH"},
		}),
	}
	_, err := n.Run(context.Background(), newDataSourceRC(ds), in)
	require.NoError(t, err)
	require.Equal(t, 10, ds.newsCalls[0].Limit)
}

// TestDataSource_News_FilterableDownstream is the regression test for
// the news bug: a filter node placed directly after a news data_source
// must actually see the rows. Before the row-projection fix, news
// emitted a typed []datasource.News under the `news` key, which
// findFirstSlice ([]any only) could not see — so every downstream
// filter matched zero. This wires news → filter and asserts a non-empty
// match.
func TestDataSource_News_FilterableDownstream(t *testing.T) {
	ds := &fakeDS{
		newsOut: map[string][]datasource.News{
			"600000.SH": {
				{StockCode: "600000.SH", Title: "重组", Source: "eastmoney"},
				{StockCode: "600000.SH", Title: "分红", Source: "sina"},
			},
		},
	}
	// 1. Run the news data_source node.
	dsNode := NewDataSourceNode()
	dsOut, err := dsNode.Run(context.Background(), newDataSourceRC(ds), map[string]any{
		orchestrator.InputKeyParams: params(t, dataSourceParams{
			Subtype:    "news",
			StockCodes: []string{"600000.SH"},
		}),
	})
	require.NoError(t, err)

	// 2. Feed its output as a predecessor payload into a filter that
	//    keeps only source == "eastmoney".
	fn := NewFilterNode()
	fout, err := fn.Run(context.Background(), newDataSourceRC(ds), map[string]any{
		orchestrator.InputKeyParams: params(t, filterParams{
			Field: "source", Op: "==", Value: "eastmoney",
		}),
		"news_node": dsOut, // predecessor payload, keyed by upstream node id
	})
	require.NoError(t, err)

	// 3. The filter must have SEEN the rows (matched 1, dropped 1) —
	//    proving findFirstSlice reached them. The pre-fix behavior was
	//    matched=0, dropped=0 (the typed slice was invisible).
	require.Equal(t, 1, fout["matched"], "filter must see news rows (bug-fix regression)")
	require.Equal(t, 1, fout["dropped"])
}
