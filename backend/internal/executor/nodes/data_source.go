// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/skyzhao/af/internal/datasource"
	"github.com/skyzhao/af/internal/orchestrator"
)

// DataSourceNode pulls market data via the injected
// datasource.Manager. Its Subtype selects the call type:
//
//	"quote"       (default) — GetQuote
//	"kline"                 — GetKLine
//	"fundamental"           — GetFundamental
//	"news"                  — GetNews
//
// All sources fail-soft per code: a single stock error is
// logged and the code is skipped, but the node only fails if
// every code failed.
type DataSourceNode struct{}

// NewDataSourceNode returns a fresh instance. The Subtype is
// read from in["_subtype"] at Run time, so the same struct is
// reused for all four variants via the Registry's
// GetBySubtype lookup.
func NewDataSourceNode() *DataSourceNode { return &DataSourceNode{} }

func (n *DataSourceNode) Type() string    { return orchestrator.NodeTypeDataSource }
func (n *DataSourceNode) Subtype() string { return "" }
func (n *DataSourceNode) Schema() orchestrator.NodeSchema {
	return orchestrator.NodeSchema{
		Description: "Pull market data via the configured datasource chain.",
		Params: map[string]orchestrator.FieldSchema{
			"subtype":        {Type: "enum", Enum: []string{"quote", "kline", "fundamental", "news"}, Required: true, Default: "quote"},
			"stock_codes":    {Type: "array", Description: "List of stock codes, e.g. 600000.SH, 000001.SZ. Optional if `universe` is set."},
			"universe":       {Type: "string", Description: "Named stock pool: hs300 / sse50 / zz500 / all / etc. Resolved to codes; merged with stock_codes."},
			"universe_limit": {Type: "number", Description: "Cap on codes contributed by `universe` (each is a separate fetch — keep modest, e.g. 50)"},
			"period":         {Type: "string", Description: "K-line period (1d/5m/30m/1h) — required for subtype=kline", Default: "1d"},
			"days":           {Type: "number", Description: "K-line lookback window in days — default 30", Default: 30},
			"news_limit":     {Type: "number", Description: "News cap per code — default 10", Default: 10},
		},
		Outputs: map[string]orchestrator.FieldSchema{
			// quote / fundamental / news all emit the unified row
			// envelope {items:[]any, count}. Only kline emits a typed
			// series under `klines` (the indicator node groups it).
			"items":  {Type: "array", Description: "Per-stock rows for quote/fundamental/news subtypes. Consumed by filter/rank/dedupe/persist/notify."},
			"klines": {Type: "array", Description: "K-line series (subtype=kline only) — fed to an indicator node."},
			"count":  {Type: "number"},
		},
	}
}

type dataSourceParams struct {
	Subtype    string   `json:"subtype"`
	StockCodes []string `json:"stock_codes"`
	// Universe resolves a named stock pool (hs300 / sse50 / zz500 /
	// all / a board) to codes via the datasource UniverseLister. When
	// set, it is merged with any explicit StockCodes. UniverseLimit
	// caps how many codes the universe contributes (0 = sidecar
	// default) — important because each code is a separate kline fetch.
	Universe      string `json:"universe"`
	UniverseLimit int    `json:"universe_limit"`
	Period        string `json:"period"`
	Days          int    `json:"days"`
	NewsLimit     int    `json:"news_limit"`
}

func (n *DataSourceNode) Run(ctx context.Context, rc *orchestrator.RunContext, in map[string]any) (map[string]any, error) {
	var p dataSourceParams
	if raw, ok := in["_params"].([]byte); ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, orchestrator.NewParamError("data_source", "params", "invalid json: "+err.Error())
		}
	}
	if p.Subtype == "" {
		p.Subtype = "quote"
	}
	if p.Days <= 0 {
		p.Days = 30
	}
	if p.NewsLimit <= 0 {
		p.NewsLimit = 10
	}
	if p.Period == "" {
		p.Period = "1d"
	}

	// Resolve a named universe into codes (if requested) and merge
	// with explicit codes (deduped, order-stable).
	codes := p.StockCodes
	if p.Universe != "" {
		if rc == nil || rc.DataSource == nil {
			// Trial-run can't resolve a universe; report it as a no-op.
			return map[string]any{
				"dry_run": true, "subtype": p.Subtype, "universe": p.Universe, "count": 0,
			}, nil
		}
		lister, ok := rc.DataSource.(datasource.UniverseLister)
		if !ok {
			return nil, orchestrator.NewParamError("data_source", "universe", "universe resolution not available")
		}
		resolved, err := lister.ListUniverse(ctx, p.Universe, p.UniverseLimit)
		if err != nil {
			return nil, orchestrator.NewParamError("data_source", "universe", err.Error())
		}
		codes = mergeCodes(p.StockCodes, resolved)
	}
	if len(codes) == 0 {
		return nil, orchestrator.NewParamError("data_source", "stock_codes", "provide stock_codes or a universe")
	}

	if rc == nil || rc.DataSource == nil {
		// Trial-run or DB-less mode.
		return map[string]any{
			"dry_run":     true,
			"subtype":     p.Subtype,
			"stock_codes": codes,
			"count":       0,
		}, nil
	}

	switch p.Subtype {
	case "quote":
		return n.fetchQuotes(ctx, rc, codes), nil
	case "kline":
		return n.fetchKLines(ctx, rc, codes, p.Period, p.Days), nil
	case "fundamental":
		return n.fetchFundamentals(ctx, rc, codes), nil
	case "news":
		return n.fetchNews(ctx, rc, codes, p.NewsLimit), nil
	default:
		return nil, orchestrator.NewParamError("data_source", "subtype", "unknown: "+p.Subtype)
	}
}

// mergeCodes concatenates explicit + resolved codes, deduped and
// order-stable (explicit first).
func mergeCodes(explicit, resolved []string) []string {
	seen := make(map[string]struct{}, len(explicit)+len(resolved))
	out := make([]string, 0, len(explicit)+len(resolved))
	for _, c := range append(append([]string{}, explicit...), resolved...) {
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func (n *DataSourceNode) fetchQuotes(ctx context.Context, rc *orchestrator.RunContext, codes []string) map[string]any {
	// Prefer a single batched call (whole-market /spot snapshot) when
	// the datasource supports it — one request for any number of codes,
	// vs N per-stock GetQuote calls that hit sina rate limits on a
	// universe. Falls back to per-stock on batch failure.
	if bq, ok := rc.DataSource.(datasource.BatchQuoter); ok {
		if quotes, err := bq.BatchQuote(ctx, codes); err == nil {
			items := datasource.ToRows(quotes)
			return map[string]any{"items": items, "count": len(items)}
		} else {
			rc.Logger.Warn("data_source: batch quote failed; falling back to per-stock", zap.Error(err))
		}
	}
	// Per-stock rows via Quote.Row() — same {items:[]any} contract as
	// fundamental/news, so a filter/rank directly downstream of a quote
	// data_source can read scalar fields via findFirstSlice.
	quotes := make([]datasource.Quote, 0, len(codes))
	for _, c := range codes {
		q, err := rc.DataSource.GetQuote(ctx, c)
		if err != nil {
			rc.Logger.Warn("data_source: GetQuote failed", zap.String("code", c), zap.Error(err))
			continue
		}
		if q == nil {
			continue
		}
		quotes = append(quotes, *q)
	}
	items := datasource.ToRows(quotes)
	return map[string]any{"items": items, "count": len(items)}
}

func (n *DataSourceNode) fetchKLines(ctx context.Context, rc *orchestrator.RunContext, codes []string, period string, days int) map[string]any {
	end := rc.Now().UTC()
	start := end.AddDate(0, 0, -days)
	out := make([]datasource.KLine, 0, len(codes)*days)
	for _, c := range codes {
		kl, err := rc.DataSource.GetKLine(ctx, c, period, start, end)
		if err != nil {
			rc.Logger.Warn("data_source: GetKLine failed", zap.String("code", c), zap.Error(err))
			continue
		}
		out = append(out, kl...)
	}
	// KLine stays a typed SERIES under "klines": the indicator node
	// groups these candles by stock and emits per-stock scalar rows.
	// This is intentionally NOT the {items} row shape.
	return map[string]any{"klines": out, "count": len(out), "period": period, "start": start, "end": end}
}

func (n *DataSourceNode) fetchFundamentals(ctx context.Context, rc *orchestrator.RunContext, codes []string) map[string]any {
	// Per-stock rows via Fundamental.Row() so filter/rank (which scan
	// for the first []any via findFirstSlice and read scalar fields)
	// consume them directly. A typed []datasource.Fundamental would NOT
	// match findFirstSlice ([]any only), so every fundamental filter
	// would match 0 — the row projection is what makes it work.
	funds := make([]datasource.Fundamental, 0, len(codes))
	for _, c := range codes {
		f, err := rc.DataSource.GetFundamental(ctx, c)
		if err != nil {
			rc.Logger.Warn("data_source: GetFundamental failed", zap.String("code", c), zap.Error(err))
			continue
		}
		if f == nil {
			continue
		}
		funds = append(funds, *f)
	}
	items := datasource.ToRows(funds)
	return map[string]any{"items": items, "count": len(items)}
}

func (n *DataSourceNode) fetchNews(ctx context.Context, rc *orchestrator.RunContext, codes []string, limit int) map[string]any {
	// News is emitted as {items:[]any} rows (via News.Row()), NOT a
	// typed []datasource.News. findFirstSlice only matches []any, so a
	// typed slice would silently match zero downstream — same class of
	// bug fundamentals had. Row projection keeps news filterable.
	out := make([]datasource.News, 0, len(codes))
	for _, c := range codes {
		ns, err := rc.DataSource.GetNews(ctx, c, limit)
		if err != nil {
			rc.Logger.Warn("data_source: GetNews failed", zap.String("code", c), zap.Error(err))
			continue
		}
		out = append(out, ns...)
	}
	items := datasource.ToRows(out)
	return map[string]any{"items": items, "count": len(items)}
}

// Compile-time interface check.
var _ orchestrator.Node = (*DataSourceNode)(nil)

// Stub references to keep imports stable for the linker.
var _ = fmt.Sprintf
var _ = time.Now
