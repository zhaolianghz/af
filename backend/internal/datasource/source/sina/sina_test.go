// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package sina — sina_test.go
package sina

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skyzhao/af/internal/datasource"
)

// sinaQuoteBody builds a representative Sina response. The format is
// a single line of comma-separated values per the public docs.
func sinaQuoteBody(name string, open, prev, price, high, low float64, volume int64, amount float64) string {
	parts := make([]string, 32)
	parts[0] = name
	parts[1] = ftoa(open)
	parts[2] = ftoa(prev)
	parts[3] = ftoa(price)
	parts[4] = ftoa(high)
	parts[5] = ftoa(low)
	parts[6] = "0" // bid 1 volume
	parts[7] = "0" // ask 1 volume
	parts[8] = ftoa(float64(volume))
	parts[9] = ftoa(amount)
	parts[10] = "0"
	for i := 0; i < 5; i++ {
		parts[11+i*2] = ftoa(price - float64(i+1))
		parts[12+i*2] = "100"
		parts[21+i*2] = ftoa(price + float64(i+1))
		parts[22+i*2] = "100"
	}
	parts[31] = "2025-06-10,15:00:00,00"
	return `var hq_str_sh600519="` + strings.Join(parts, ",") + `";` + "\n"
}

func ftoa(v float64) string {
	// Trivial, no fmt import needed.
	if v == 0 {
		return "0.000"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := v - float64(whole)
	if frac < 0 {
		frac = -frac
	}
	fracStr := ""
	for i := 0; i < 3; i++ {
		frac *= 10
		d := int(frac)
		fracStr += string(rune('0' + d))
		frac -= float64(d)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return sign + itoa(whole) + "." + fracStr
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

func TestSinaGetQuoteHappyPath(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/list=sh600519", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sinaQuoteBody("贵州茅台", 1815, 1818, 1820, 1830, 1810, 12345678, 22500000000)))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	q, err := src.GetQuote(context.Background(), "600519")
	require.NoError(t, err)
	require.NotNil(t, q)
	assert.Equal(t, "贵州茅台", q.StockName)
	assert.Equal(t, 1820.0, q.Price)
	assert.Equal(t, 1810.0, q.Low)
	assert.Equal(t, 1815.0, q.Open)
	assert.Equal(t, 1818.0, q.PrevClose)
	assert.Equal(t, "sina", q.Source)
	assert.Equal(t, int64(12345678), q.Volume)
	assert.Equal(t, 22500000000.0, q.Amount)
}

func TestSinaGetQuoteBadCode(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", Timeout: 50 * time.Millisecond})
	_, err := src.GetQuote(context.Background(), "abc")
	assert.Error(t, err)
}

func TestSinaGetQuoteServerError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/list=sh600519", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}

func TestSinaGetQuoteMalformedBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/list=sh600519", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`var hq_str_sh600519="only,few,parts";`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestSinaGetQuoteNetworkError(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", Timeout: 50 * time.Millisecond})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.Error(t, err)
}

func TestSinaGetKLine(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/json_v2.php/CN_MarketData.getKLineData", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"day":"2025-06-09","open":1810,"high":1825,"low":1809,"close":1820,"volume":12345}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	start := time.Date(2025, 6, 9, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC)
	kl, err := src.GetKLine(context.Background(), "600519", "1d", start, end)
	require.NoError(t, err)
	require.Len(t, kl, 1)
	assert.Equal(t, 1820.0, kl[0].Close)
	assert.Equal(t, 1810.0, kl[0].Open)
}

func TestSinaGetKLineEmpty(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/json_v2.php/CN_MarketData.getKLineData", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetKLine(context.Background(), "600519", "1d", time.Now(), time.Now().Add(time.Hour))
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestSinaGetKLineIntradayUnsupported(t *testing.T) {
	t.Parallel()
	src := New(Options{BaseURL: "http://127.0.0.1:1", Timeout: 50 * time.Millisecond})
	_, err := src.GetKLine(context.Background(), "600519", "5m", time.Now(), time.Now().Add(time.Hour))
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestSinaGetFundamental(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	_, err := src.GetFundamental(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestSinaGetNews(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	_, err := src.GetNews(context.Background(), "600519", 5)
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}

func TestSinaName(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	assert.Equal(t, "sina", src.Name())
	assert.True(t, src.IsHealthy())
	src.MarkUnhealthy(true)
	assert.False(t, src.IsHealthy())
}

func TestSinaSymbol(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		// bare 6-digit (market inferred from prefix)
		"600519": "sh600519",
		"000001": "sz000001",
		"300750": "sz300750",
		"430001": "bj430001",
		"830001": "bj830001",
		"900901": "sh900901",
		// suffixed canonical form (the form bundled templates ship —
		// this is the case that used to fail with "expected 6-digit code")
		"600519.SH": "sh600519",
		"000001.SZ": "sz000001",
		"430047.BJ": "bj430047",
		// suffix is authoritative + case-insensitive
		"600519.sh": "sh600519",
	}
	for code, want := range cases {
		got, err := sinaSymbol(code)
		require.NoError(t, err, code)
		assert.Equal(t, want, got, code)
	}
	_, err := sinaSymbol("12345")
	assert.Error(t, err)
	_, err = sinaSymbol("7xxxxx")
	assert.Error(t, err)
	_, err = sinaSymbol("600519.XX") // bad suffix
	assert.Error(t, err)
}

func TestSinaNewDefaults(t *testing.T) {
	t.Parallel()
	src := New(Options{})
	assert.Equal(t, DefaultBaseURL, src.base)
	assert.NotNil(t, src.http)
}

func TestSinaEmptyBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/list=sh600519", func(w http.ResponseWriter, r *http.Request) {
		// no body
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := New(Options{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, err := src.GetQuote(context.Background(), "600519")
	assert.ErrorIs(t, err, datasource.ErrNotImplemented)
}
