// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// rows.go is the projection layer: it maps the canonical market-data
// structs (Quote / Fundamental / News) into the per-stock "row" shape
// the executor's downstream nodes (filter / rank / dedupe / persist /
// notify) consume.
//
// Why this exists as its own layer:
//   - Downstream nodes scan for the first []any of map[string]any via
//     findFirstSlice and read scalar fields by snake_case key. A typed
//     slice ([]Fundamental, []News) does NOT match that scan, so it
//     would silently filter to zero. Centralizing the struct→row
//     mapping here keeps the contract in ONE place instead of being
//     hand-written (and drifting) inside data_source.go.
//   - The one semantic convention — Quote.Price mirrored to `close` so
//     indicator-less screens can filter on `close` — lives here, once.
//
// KLine deliberately has NO Row(): it is a SERIES (many candles per
// stock) consumed by the indicator node under the `klines` key, which
// groups by stock and computes per-stock scalars. Flattening it to one
// row per candle would break that grouping. The kline path stays typed.
package datasource

// Rower is implemented by canonical types that project to a single
// downstream row. toRows turns a typed slice into the []any the
// executor nodes expect.
type Rower interface {
	Row() map[string]any
}

// toRows projects a typed slice of Rowers into the []any of
// map[string]any envelope payload that findFirstSlice consumes.
func ToRows[T Rower](xs []T) []any {
	out := make([]any, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.Row())
	}
	return out
}

// Row projects a Quote to a downstream row. Price is mirrored to
// `close` so screens that have no indicator (pure quote filters) can
// still filter/rank on `close` like a kline-derived row would.
func (q Quote) Row() map[string]any {
	return map[string]any{
		"stock_code": q.StockCode,
		"stock_name": q.StockName,
		"price":      q.Price,
		"close":      q.Price,
		"open":       q.Open,
		"high":       q.High,
		"low":        q.Low,
		"prev_close": q.PrevClose,
		"volume":     q.Volume,
		"amount":     q.Amount,
	}
}

// Row projects a Fundamental to a downstream row (PE/PB/ROE-driven
// screens read these scalar fields by key).
func (f Fundamental) Row() map[string]any {
	return map[string]any{
		"stock_code":     f.StockCode,
		"stock_name":     f.StockName,
		"pe":             f.PE,
		"pb":             f.PB,
		"roe":            f.ROE,
		"dividend_yield": f.DividendYield,
		"revenue":        f.Revenue,
		"net_profit":     f.NetProfit,
	}
}

// Row projects a News item to a downstream row. Emitting news as rows
// (not a typed []News) lets a filter/notify node sit directly after a
// news data_source — findFirstSlice only matches []any, so a typed
// slice would silently match zero.
func (n News) Row() map[string]any {
	return map[string]any{
		"stock_code":   n.StockCode,
		"title":        n.Title,
		"content":      n.Content,
		"url":          n.URL,
		"published_at": n.PublishedAt,
		"source":       n.Source,
	}
}

// Compile-time checks: these types satisfy Rower.
var (
	_ Rower = Quote{}
	_ Rower = Fundamental{}
	_ Rower = News{}
)
