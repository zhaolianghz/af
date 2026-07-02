// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package akshare — akshare_test.go
package akshare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/datasource"
)

func TestAKShareGetQuoteHappyPath(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/quote", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":       "600519",
			"name":       "贵州茅台",
			"price":      1820.0,
			"open":       1815.0,
			"high":       1830.0,
			"low":        1810.0,
			"prev_close": 1818.0,
			"volume":     12345678,
			"amount":     22500000000.0,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	q, err := src.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	require.NotNil(t, q)
	assert.Equal(t, "600519", q.StockCode)
	assert.Equal(t, "贵州茅台", q.StockName)
	assert.Equal(t, 1820.0, q.Price)
	assert.Equal(t, 1815.0, q.Open)
	assert.Equal(t, "akshare", q.Source)
}

func TestAKShareGetQuoteEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/quote", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"","name":""}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestAKShareGetQuoteServerError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/quote", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}

func TestAKShareGetQuoteNetworkError(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", Timeout: 50 * time.Millisecond})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}

func TestAKShareGetKLine(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/kline", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"klines": []map[string]interface{}{
				{"day": "2025-06-09", "open": 1810.0, "high": 1825.0, "low": 1809.0, "close": 1820.0, "volume": 12345, "amount": 22500.0},
				{"day": "2025-06-10", "open": 1820.0, "high": 1830.0, "low": 1819.0, "close": 1825.0, "volume": 13579, "amount": 24242.0},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	start := time.Date(2025, 6, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 11, 0, 0, 0, 0, time.UTC)
	kl, err := src.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	require.Len(t, kl, 2)
	assert.Equal(t, 1820.0, kl[0].Close)
	assert.Equal(t, 1825.0, kl[1].Close)
	assert.Equal(t, "akshare", kl[0].Source)
}

func TestAKShareGetKLineEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/kline", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"klines": []interface{}{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetKLine(context.Background(), "600519", "1d", time.Now(), time.Now().Add(time.Hour))
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestAKShareGetFundamental(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/fundamental", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code":            "600519",
			"name":            "贵州茅台",
			"pe":              25.5,
			"pb":              7.8,
			"roe":             0.25,
			"dividend_yield":  0.015,
			"revenue":         1245000000000.0,
			"net_profit":      62716000000.0,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	f, err := src.GetFundamental(context.Background(), "600519")
	require.NoError(t, err)
	assert.Equal(t, "贵州茅台", f.StockName)
	assert.Equal(t, 25.5, f.PE)
	assert.Equal(t, 7.8, f.PB)
	assert.Equal(t, 0.25, f.ROE)
	assert.Equal(t, 0.015, f.DividendYield)
	assert.Equal(t, 1245000000000.0, f.Revenue)
}

func TestAKShareGetFundamentalEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/fundamental", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":""}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetFundamental(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestAKShareGetNews(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/news", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"news": []map[string]interface{}{
				{
					"title":        "Moutai Q1 earnings beat",
					"content":      "...",
					"url":          "https://example.com/1",
					"published_at": time.Date(2025, 4, 30, 9, 30, 0, 0, time.UTC),
				},
				{
					"title":        "Moutai announces dividend",
					"content":      "...",
					"url":          "https://example.com/2",
					"published_at": time.Date(2025, 5, 15, 9, 30, 0, 0, time.UTC),
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	news, err := src.GetNews(context.Background(), "600519", 10)
	require.NoError(t, err)
	require.Len(t, news, 2)
	assert.Equal(t, "Moutai Q1 earnings beat", news[0].Title)
	assert.Equal(t, "akshare", news[0].Source)
}

func TestAKShareGetNewsEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/news", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"news":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetNews(context.Background(), "600519", 10)
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestAKShareName(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	assert.Equal(t, "akshare", src.Name())
	assert.True(t, src.IsHealthy())
	src.MarkUnhealthy(true)
	assert.False(t, src.IsHealthy())
}

func TestAKShareNewDefaults(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	assert.Equal(t, DefaultBaseURL, src.base)
	assert.NotNil(t, src.http)
}

func TestAKShareKLineServerError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/kline", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetKLine(context.Background(), "600519", "1d", time.Now(), time.Now().Add(time.Hour))
	assert.Error(t, err)
}

func TestAKShareMalformedJSON(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/quote", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}
