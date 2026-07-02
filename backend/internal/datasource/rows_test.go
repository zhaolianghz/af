// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package datasource

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuoteRow_FieldsAndCloseMirror(t *testing.T) {
	q := Quote{
		StockCode: "600519.SH",
		StockName: "贵州茅台",
		Price:     1820.5,
		Open:      1800,
		High:      1850,
		Low:       1795,
		PrevClose: 1810,
		Volume:    12345,
		Amount:    9.9e8,
	}
	r := q.Row()
	// price mirrored to close so indicator-less screens can filter on close.
	require.Equal(t, 1820.5, r["price"])
	require.Equal(t, 1820.5, r["close"], "price must mirror to close")
	require.Equal(t, "600519.SH", r["stock_code"])
	require.Equal(t, "贵州茅台", r["stock_name"])
	require.Equal(t, 1800.0, r["open"])
	require.Equal(t, 1850.0, r["high"])
	require.Equal(t, 1795.0, r["low"])
	require.Equal(t, 1810.0, r["prev_close"])
	require.Equal(t, int64(12345), r["volume"])
	require.Equal(t, 9.9e8, r["amount"])
}

func TestFundamentalRow_Fields(t *testing.T) {
	f := Fundamental{
		StockCode:     "000001.SZ",
		StockName:     "平安银行",
		PE:            5.2,
		PB:            0.6,
		ROE:           12.3,
		DividendYield: 3.1,
		Revenue:       1.2e11,
		NetProfit:     4.5e10,
	}
	r := f.Row()
	require.Equal(t, "000001.SZ", r["stock_code"])
	require.Equal(t, "平安银行", r["stock_name"])
	require.Equal(t, 5.2, r["pe"])
	require.Equal(t, 0.6, r["pb"])
	require.Equal(t, 12.3, r["roe"])
	require.Equal(t, 3.1, r["dividend_yield"])
	require.Equal(t, 1.2e11, r["revenue"])
	require.Equal(t, 4.5e10, r["net_profit"])
}

func TestNewsRow_Fields(t *testing.T) {
	ts := time.Date(2026, 6, 25, 9, 30, 0, 0, time.UTC)
	n := News{
		StockCode:   "600519.SH",
		Title:       "茅台发布年报",
		Content:     "……",
		URL:         "https://example.com/a",
		PublishedAt: ts,
		Source:      "eastmoney",
	}
	r := n.Row()
	require.Equal(t, "600519.SH", r["stock_code"])
	require.Equal(t, "茅台发布年报", r["title"])
	require.Equal(t, "……", r["content"])
	require.Equal(t, "https://example.com/a", r["url"])
	require.Equal(t, ts, r["published_at"])
	require.Equal(t, "eastmoney", r["source"])
}

func TestToRows_ProjectsTypedSliceToAny(t *testing.T) {
	// The whole point: a typed slice becomes []any of map[string]any so
	// the executor's findFirstSlice (which only matches []any) can read
	// it. This is the bug-class fix for fundamentals AND news.
	funds := []Fundamental{
		{StockCode: "A", PE: 1},
		{StockCode: "B", PE: 2},
	}
	rows := ToRows(funds)
	require.Len(t, rows, 2)
	// Each element must be a map[string]any (NOT a typed struct), or
	// findFirstSlice downstream matches zero.
	m0, ok := rows[0].(map[string]any)
	require.True(t, ok, "row must be map[string]any for findFirstSlice")
	require.Equal(t, "A", m0["stock_code"])
	require.Equal(t, 1.0, m0["pe"])

	news := []News{{StockCode: "X", Title: "t"}}
	nrows := ToRows(news)
	require.Len(t, nrows, 1)
	nm, ok := nrows[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "X", nm["stock_code"])
	require.Equal(t, "t", nm["title"])
}

func TestToRows_Empty(t *testing.T) {
	require.Empty(t, ToRows([]Quote{}))
	require.Empty(t, ToRows[Quote](nil))
}
