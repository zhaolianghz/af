// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package eastmoney — eastmoney_test.go
package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/datasource"
)

func newTestSource(base, kline string) (*Source, *httptest.Server) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	src := New(Options{
		BaseURL:  srv.URL + base,
		KLineURL: srv.URL + kline,
		Timeout:  2 * time.Second,
	})
	return src, srv
}

func TestEastmoneyGetQuoteHappyPath(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/get", func(w http.ResponseWriter, r *http.Request) {
		// The endpoint is the same for quote and fundamental; we
		// distinguish by looking at the requested `fields` param.
		fields := r.URL.Query().Get("fields")
		if !strings.Contains(fields, "f43") {
			// fundamental request
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"f57": "贵州茅台",
					"f58": "600519",
					"f162": 25.5,
					"f167": 7.8,
					"f173": 0.25,
					"f189": 0.015,
					"f183": 1245000000000.0,
					"f184": 62716000000.0,
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"f57": "贵州茅台",
				"f58": "600519",
				"f43": 1820.0,
				"f44": 1830.0,
				"f45": 1810.0,
				"f46": 1815.0,
				"f60": 1818.0,
				"f47": 12345678,
				"f48": 22500000000.0,
				"f51": map[string]interface{}{
					"p1": 1819.0, "v1": 100,
					"p2": 1818.0, "v2": 200,
					"p3": 1817.0, "v3": 300,
					"p4": 1816.0, "v4": 400,
					"p5": 1815.0, "v5": 500,
				},
				"f52": map[string]interface{}{
					"p1": 1820.0, "v1": 150,
					"p2": 1821.0, "v2": 250,
					"p3": 1822.0, "v3": 350,
					"p4": 1823.0, "v4": 450,
					"p5": 1824.0, "v5": 550,
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	q, err := src.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	require.NotNil(t, q)
	assert.Equal(t, "600519", q.StockCode)
	assert.Equal(t, "贵州茅台", q.StockName)
	assert.Equal(t, 1820.0, q.Price)
	assert.Equal(t, 1830.0, q.High)
	assert.Equal(t, 1810.0, q.Low)
	assert.Equal(t, 1815.0, q.Open)
	assert.Equal(t, 1818.0, q.PrevClose)
	assert.Equal(t, int64(12345678), q.Volume)
	assert.Equal(t, "eastmoney", q.Source)
	require.Len(t, q.Bid5, 5)
	assert.Equal(t, 1819.0, q.Bid5[0].Price)
	require.Len(t, q.Ask5, 5)
	assert.Equal(t, 1820.0, q.Ask5[0].Price)
}

func TestEastmoneyGetQuoteBadCode(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", KLineURL: "http://127.0.0.1:1", Timeout: 50 * time.Millisecond})
	_, err := src.GetQuote(context.Background(), "abc")
	assert.Error(t, err)
}

func TestEastmoneyGetQuoteEmptyBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestEastmoneyGetQuoteNetworkError(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", KLineURL: "http://127.0.0.1:1", Timeout: 50 * time.Millisecond})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}

func TestEastmoneyGetQuoteServerError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/get", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}

func TestEastmoneyGetKLine(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/kline/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"code": "600519",
				"name": "贵州茅台",
				"klines": "2025-06-09,1810,1820,1825,1809,12345,22500\n2025-06-10,1820,1830,1835,1819,13579,24242",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	start := time.Date(2025, 6, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 11, 0, 0, 0, 0, time.UTC)
	kl, err := src.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	require.Len(t, kl, 2)
	assert.Equal(t, 1810.0, kl[0].Open)
	assert.Equal(t, 1820.0, kl[0].Close)
	assert.Equal(t, 1825.0, kl[0].High)
	assert.Equal(t, 1809.0, kl[0].Low)
	assert.Equal(t, int64(12345), kl[0].Volume)
	assert.Equal(t, 22500.0, kl[0].Amount)
	assert.Equal(t, "eastmoney", kl[0].Source)
	assert.Equal(t, time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC).Unix(), kl[1].Timestamp.Unix())
}

func TestEastmoneyGetKLineEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/kline/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"klines": ""}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetKLine(context.Background(), "600519", "1d", time.Now(), time.Now().Add(time.Hour))
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestEastmoneyGetKLineBadPeriod(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", KLineURL: "http://127.0.0.1:1", Timeout: time.Second})
	_, err := src.GetKLine(context.Background(), "600519", "bogus", time.Now(), time.Now().Add(time.Hour))
	assert.Error(t, err)
}

func TestEastmoneyGetKLineBadCode(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", KLineURL: "http://127.0.0.1:1", Timeout: time.Second})
	_, err := src.GetKLine(context.Background(), "abc", "1d", time.Now(), time.Now().Add(time.Hour))
	assert.Error(t, err)
}

func TestEastmoneyGetFundamental(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"f57": "贵州茅台",
				"f58": "600519",
				"f162": 25.5,
				"f167": 7.8,
				"f173": 0.25,
				"f189": 0.015,
				"f183": 1245000000000.0,
				"f184": 62716000000.0,
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	f, err := src.GetFundamental(context.Background(), "600519")
	require.NoError(t, err)
	assert.Equal(t, "贵州茅台", f.StockName)
	assert.Equal(t, 25.5, f.PE)
	assert.Equal(t, 7.8, f.PB)
	assert.Equal(t, 0.25, f.ROE)
	assert.Equal(t, 0.015, f.DividendYield)
	assert.Equal(t, 1245000000000.0, f.Revenue)
	assert.Equal(t, 62716000000.0, f.NetProfit)
}

func TestEastmoneyGetFundamentalEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/qt/stock/get", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetFundamental(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestEastmoneyGetNews(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	_, err := src.GetNews(context.Background(), "600519", 5)
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestEastmoneyName(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	assert.Equal(t, "eastmoney", src.Name())
	assert.True(t, src.IsHealthy())
	src.MarkUnhealthy(true)
	assert.False(t, src.IsHealthy())
	src.MarkUnhealthy(false)
	assert.True(t, src.IsHealthy())
}

func TestEastmoneyToSecID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"600519": "1.600519",
		"000001": "0.000001",
		"300750": "0.300750",
		"430001": "0.430001",
		"830001": "0.830001",
		"900901": "1.900901",
	}
	for code, want := range cases {
		got, err := toSecID(code)
		require.NoError(t, err, code)
		assert.Equal(t, want, got, code)
	}
	_, err := toSecID("12345")
	assert.Error(t, err)
	_, err = toSecID("7xxxxx")
	assert.Error(t, err)
}

func TestEastmoneyPeriodToKLT(t *testing.T) {
	t.Parallel()
	// Pin eastmoney's ACTUAL klt codes. The old mapping was off by
	// a whole scheme (1m→101, 1d→107); 101 is really daily, so daily
	// requests silently fetched nothing. These values are the real
	// push2his klt contract.
	want := map[string]int{
		"1m":  1,
		"5m":  5,
		"15m": 15,
		"30m": 30,
		"60m": 60,
		"1h":  60,
		"1d":  101,
		"1w":  102,
		"1M":  103,
		"1mo": 103,
	}
	for p, exp := range want {
		got, err := periodToKLT(p)
		assert.NoError(t, err, p)
		assert.Equal(t, exp, got, p)
	}
	// Periods eastmoney's kline endpoint has no klt for must error,
	// not silently map to a bogus code.
	for _, p := range []string{"120m", "2h", "1Q", "1Y", "bogus", ""} {
		_, err := periodToKLT(p)
		assert.Error(t, err, p)
	}
}

func TestEastmoneyNewDefaults(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	assert.Equal(t, DefaultBaseURL, src.base)
	assert.Equal(t, DefaultKLineBaseURL, src.kline)
	assert.NotNil(t, src.http)
}

func TestEastmoneyUserAgentOverride(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	uaCh := make(chan string, 1)
	mux.HandleFunc("/api/qt/stock/get", func(w http.ResponseWriter, r *http.Request) {
		uaCh <- r.Header.Get("User-Agent")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{"f57": "x", "f58": "600519"}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, KLineURL: srv.URL, UserAgent: "test-agent", Timeout: time.Second})
	_, _ = src.GetQuote(context.Background(), "600519")
	select {
	case ua := <-uaCh:
		assert.Equal(t, "test-agent", ua)
	case <-time.After(time.Second):
		t.Fatal("did not receive request")
	}
}

// fmtSprint is unused at the moment but keeps the import live.
var _ = fmt.Sprintf
