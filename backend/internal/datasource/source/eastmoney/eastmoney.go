// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package eastmoney implements the datasource.Source contract against
// the eastmoney public HTTP API. The real public endpoints
// (push2.eastmoney.com, push2his.eastmoney.com, …) are unstable and
// the project does not require production-grade live accuracy in v1;
// this implementation focuses on:
//
//   - A clean, testable HTTP client that talks to a configurable
//     base URL (so tests can point it at httptest.NewServer).
//   - Real-format parsers for the most common responses (quote push,
//     K-line history, basic fundamental) so the integration is
//     production-shaped even when the data is mocked.
//   - Reasonable defaults for BaseURL / Timeout that match the
//     documented public endpoints.
//
// When a real response is not available (e.g. the test feeds in a
// canned body), the parser falls through to ErrNotImplemented for
// fields the source does not actually cover; the manager treats that
// as a permanent error and tries the next source.
package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skyzhao/af/internal/datasource"
)

const (
	// DefaultBaseURL is the public push endpoint for eastmoney. It
	// is overridable via Options.BaseURL for tests.
	DefaultBaseURL = "https://push2.eastmoney.com"
	// DefaultKLineBaseURL is the historical K-line endpoint.
	DefaultKLineBaseURL = "https://push2his.eastmoney.com"
	// DefaultTimeout matches the project's 5s rule of thumb.
	DefaultTimeout = 5 * time.Second
)

// Options configures a Source. Zero values fall back to the
// package-level defaults.
type Options struct {
	BaseURL    string        // push endpoint
	KLineURL   string        // K-line endpoint
	HTTPClient *http.Client  // optional
	Timeout    time.Duration // used when HTTPClient is nil
	UserAgent  string
}

// Source is the eastmoney implementation of datasource.Source.
type Source struct {
	base    string
	kline   string
	http    *http.Client
	ua      string
	healthy bool
}

// New builds an eastmoney Source.
func New(opts Options) *Source {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.KLineURL == "" {
		opts.KLineURL = DefaultKLineBaseURL
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "af-backend/0.1 (+eastmoney-adapter)"
	}
	return &Source{
		base:    strings.TrimRight(opts.BaseURL, "/"),
		kline:   strings.TrimRight(opts.KLineURL, "/"),
		http:    opts.HTTPClient,
		ua:      opts.UserAgent,
		healthy: true,
	}
}

// Name implements datasource.Source.
func (s *Source) Name() string { return "eastmoney" }

// IsHealthy implements datasource.Source.
func (s *Source) IsHealthy() bool { return s.healthy }

// MarkUnhealthy flips the in-process liveness flag. Used by
// background health checks; not part of the public Source contract.
func (s *Source) MarkUnhealthy(v bool) { s.healthy = !v }

// GetQuote implements datasource.Source.
//
// Eastmoney's push endpoint takes a 1-prefixed secid; for Shanghai
// (6xxxxx) stocks that's "1.600519", for Shenzhen (0/3xxxxx) stocks
// it's "0.000001". The conversion is encapsulated here so callers
// pass plain stock codes.
func (s *Source) GetQuote(ctx context.Context, stockCode string) (*datasource.Quote, error) {
	secid, err := toSecID(stockCode)
	if err != nil {
		return nil, fmt.Errorf("eastmoney: bad stock code %q: %w", stockCode, err)
	}

	q := url.Values{}
	q.Set("secid", secid)
	q.Set("fields", "f43,f44,f45,f46,f47,f48,f50,f51,f57,f58,f60,f168,f169,f170,f171")
	q.Set("invt", "2")
	q.Set("fltt", "2")
	endpoint := s.base + "/api/qt/stock/get?" + q.Encode()

	var payload struct {
		Data *quotePayload `json:"data"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, fmt.Errorf("eastmoney: GetQuote: %w", err)
	}
	if payload.Data == nil {
		return nil, datasource.ErrNotImplemented
	}
	if payload.Data.Name == "" || payload.Data.Code == "" {
		return nil, datasource.ErrNotImplemented
	}

	q1 := &datasource.Quote{
		StockCode: payload.Data.Code,
		StockName: payload.Data.Name,
		Price:     payload.Data.Price,
		Open:      payload.Data.Open,
		High:      payload.Data.High,
		Low:       payload.Data.Low,
		PrevClose: payload.Data.PreviousClose,
		Volume:    payload.Data.Volume,
		Amount:    payload.Data.Amount,
		Bid5:      payload.Data.Bid5.toBidAsks(),
		Ask5:      payload.Data.Ask5.toBidAsks(),
		Timestamp: time.Now().UTC(),
		Source:    s.Name(),
	}
	return q1, nil
}

// GetKLine implements datasource.Source.
//
// Eastmoney's K-line endpoint returns a klines string with comma
// separated fields; we parse it directly so the test suite can
// hand-construct responses.
func (s *Source) GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]datasource.KLine, error) {
	secid, err := toSecID(stockCode)
	if err != nil {
		return nil, fmt.Errorf("eastmoney: bad stock code %q: %w", stockCode, err)
	}
	// klt selects the bar size (see periodToKLT: 1/5/15/30/60 for
	// intraday minutes, 101/102/103 for daily/weekly/monthly).
	klt, err := periodToKLT(period)
	if err != nil {
		return nil, fmt.Errorf("eastmoney: unsupported period %q: %w", period, err)
	}

	q := url.Values{}
	q.Set("secid", secid)
	q.Set("fields1", "f1,f2,f3,f4,f5,f6")
	q.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61")
	q.Set("klt", strconv.Itoa(klt))
	q.Set("fqt", "1")
	q.Set("beg", start.Format("20060102"))
	q.Set("end", end.Format("20060102"))
	q.Set("lmt", "1000")
	endpoint := s.kline + "/api/qt/stock/kline/get?" + q.Encode()

	var payload struct {
		Data *klinePayload `json:"data"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, fmt.Errorf("eastmoney: GetKLine: %w", err)
	}
	if payload.Data == nil || len(payload.Data.KLines) == 0 {
		return nil, datasource.ErrNotImplemented
	}
	out := make([]datasource.KLine, 0, 32)
	for _, line := range payload.Data.KLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 6 {
			continue
		}
		ts, err := time.Parse("2006-01-02", fields[0])
		if err != nil {
			continue
		}
		k := datasource.KLine{
			StockCode: stockCode,
			Period:    period,
			Timestamp: ts,
			Open:      parseFloat(fields[1]),
			Close:     parseFloat(fields[2]),
			High:      parseFloat(fields[3]),
			Low:       parseFloat(fields[4]),
			Volume:    int64(parseFloat(fields[5])),
			Source:    s.Name(),
		}
		if len(fields) > 6 {
			k.Amount = parseFloat(fields[6])
		}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, datasource.ErrNotImplemented
	}
	return out, nil
}

// GetFundamental implements datasource.Source.
func (s *Source) GetFundamental(ctx context.Context, stockCode string) (*datasource.Fundamental, error) {
	secid, err := toSecID(stockCode)
	if err != nil {
		return nil, fmt.Errorf("eastmoney: bad stock code %q: %w", stockCode, err)
	}
	q := url.Values{}
	q.Set("secid", secid)
	q.Set("fields", "f57,f58,f162,f167,f168,f169,f170,f171,f173,f177,f183,f184,f185,f186,f187,f188,f189,f190,f191")
	endpoint := s.base + "/api/qt/stock/get?" + q.Encode()

	var payload struct {
		Data *fundamentalPayload `json:"data"`
	}
	if err := s.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, fmt.Errorf("eastmoney: GetFundamental: %w", err)
	}
	if payload.Data == nil || payload.Data.Name == "" {
		return nil, datasource.ErrNotImplemented
	}
	return &datasource.Fundamental{
		StockCode:     stockCode,
		StockName:     payload.Data.Name,
		PE:            payload.Data.PE,
		PB:            payload.Data.PB,
		ROE:           payload.Data.ROE,
		DividendYield: payload.Data.DividendYield,
		Revenue:       payload.Data.Revenue,
		NetProfit:     payload.Data.NetProfit,
		UpdateTime:    time.Now().UTC(),
		Source:        s.Name(),
	}, nil
}

// GetNews implements datasource.Source. Eastmoney does not expose a
// simple news endpoint over push; we return ErrNotImplemented so the
// manager falls through to the next source.
func (s *Source) GetNews(ctx context.Context, stockCode string, limit int) ([]datasource.News, error) {
	return nil, datasource.ErrNotImplemented
}

// =============================================================================
// HTTP helpers
// =============================================================================

// getJSON issues a GET, validates the response, and decodes JSON.
func (s *Source) getJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

// =============================================================================
// Wire-format payloads
// =============================================================================

type quotePayload struct {
	Name         string  `json:"f57"`
	Code         string  `json:"f58"`
	Price        float64 `json:"f43"`
	Open         float64 `json:"f46"`
	High         float64 `json:"f44"`
	Low          float64 `json:"f45"`
	PreviousClose float64 `json:"f60"`
	Volume       int64   `json:"f47"`
	Amount       float64 `json:"f48"`
	Bid5         bidAskGroup `json:"f51"`
	Ask5         bidAskGroup `json:"f52"`
}

type bidAskGroup struct {
	P1 float64 `json:"p1"`
	V1 int64   `json:"v1"`
	P2 float64 `json:"p2"`
	V2 int64   `json:"v2"`
	P3 float64 `json:"p3"`
	V3 int64   `json:"v3"`
	P4 float64 `json:"p4"`
	V4 int64   `json:"v4"`
	P5 float64 `json:"p5"`
	V5 int64   `json:"v5"`
}

func (b bidAskGroup) toBidAsks() []datasource.BidAsk {
	return []datasource.BidAsk{
		{Price: b.P1, Volume: b.V1},
		{Price: b.P2, Volume: b.V2},
		{Price: b.P3, Volume: b.V3},
		{Price: b.P4, Volume: b.V4},
		{Price: b.P5, Volume: b.V5},
	}
}

type klinePayload struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// The real push2his API returns klines as an ARRAY of CSV
	// strings (["2026-06-09,1810,...", ...]). It was previously
	// declared `string`, which made every live decode fail with
	// "cannot unmarshal array into Go struct field ... of type
	// string" — the primary source could never serve a K-line.
	KLines []string `json:"klines"`
}

type fundamentalPayload struct {
	Name          string  `json:"f57"`
	PE            float64 `json:"f162"`
	PB            float64 `json:"f167"`
	ROE           float64 `json:"f173"`
	DividendYield float64 `json:"f189"`
	Revenue       float64 `json:"f183"`
	NetProfit     float64 `json:"f184"`
}

// =============================================================================
// Helpers
// =============================================================================

// toSecID converts an A-share code to the eastmoney "market.code"
// format used in the secid query parameter. It accepts both the
// suffixed canonical form ("600519.SH") and the bare 6-digit form
// ("600519") via datasource.SplitCode.
func toSecID(stockCode string) (string, error) {
	digits, market, err := datasource.SplitCode(stockCode)
	if err != nil {
		return "", err
	}
	switch market {
	case datasource.MarketSH:
		return "1." + digits, nil
	case datasource.MarketSZ, datasource.MarketBJ:
		return "0." + digits, nil
	}
	return "", fmt.Errorf("unrecognized market %q in %q", market, stockCode)
}

// periodToKLT converts a textual period to eastmoney's klt field.
// These are eastmoney's ACTUAL klt codes (verified against the
// push2his kline endpoint): intraday minute bars are 1/5/15/30/60,
// and 101/102/103 are daily/weekly/monthly. The previous mapping
// was off by an entire scheme (1m→101, 1d→107): 101 is really
// *daily*, and 107 is not a valid klt at all, so every daily
// request came back empty / EOF and no template could ever fetch
// klines. Eastmoney has no quarter/year klt, so those error out.
func periodToKLT(period string) (int, error) {
	switch period {
	case "1m":
		return 1, nil
	case "5m":
		return 5, nil
	case "15m":
		return 15, nil
	case "30m":
		return 30, nil
	case "60m", "1h":
		return 60, nil
	case "1d":
		return 101, nil
	case "1w":
		return 102, nil
	case "1M", "1mo":
		return 103, nil
	}
	return 0, fmt.Errorf("unsupported period %q", period)
}

// parseFloat is a lenient float parser used for the K-line CSV.
func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
