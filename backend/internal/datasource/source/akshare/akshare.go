// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package akshare implements the datasource.Source contract against
// an akshare sidecar service. akshare is a Python library; the
// production deployment exposes its functionality over a small HTTP
// API. This implementation is intentionally permissive about the
// exact response format: it speaks the minimal JSON contract that
// the sidecar is expected to expose:
//
//	GET /quote?code=600519      -> {"code":"...","name":"...","price":...}
//	GET /kline?code=...&period=...&start=...&end=...   -> {"klines":[{...}]}
//	GET /fundamental?code=...   -> {"pe":...,"pb":...}
//	GET /news?code=...&limit=... -> {"news":[{...}]}
//
// The exact field names are open to revision; the parser is lenient
// (missing fields default to zero) and the constructor accepts a
// configurable base URL so tests can point it at httptest.NewServer.
package akshare

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
	// DefaultBaseURL is the assumed sidecar origin. v1 deployments
	// run a small Python wrapper around akshare on the same host.
	DefaultBaseURL = "http://127.0.0.1:18800"
	// DefaultTimeout matches the project's 5s rule of thumb.
	DefaultTimeout = 5 * time.Second
)

// Options configures the akshare Source.
type Options struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Source is the akshare implementation of datasource.Source.
type Source struct {
	base    string
	http    *http.Client
	healthy bool
}

// New builds an akshare Source.
func New(opts Options) *Source {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}
	return &Source{
		base:    strings.TrimRight(opts.BaseURL, "/"),
		http:    opts.HTTPClient,
		healthy: true,
	}
}

// Name implements datasource.Source.
func (s *Source) Name() string { return "akshare" }

// IsHealthy implements datasource.Source.
func (s *Source) IsHealthy() bool { return s.healthy }

// MarkUnhealthy flips the in-process liveness flag.
func (s *Source) MarkUnhealthy(v bool) { s.healthy = !v }

// GetQuote implements datasource.Source.
func (s *Source) GetQuote(ctx context.Context, stockCode string) (*datasource.Quote, error) {
	q := url.Values{}
	q.Set("code", stockCode)
	endpoint := s.base + "/quote?" + q.Encode()

	var resp struct {
		Code        string  `json:"code"`
		Name        string  `json:"name"`
		Price       float64 `json:"price"`
		Open        float64 `json:"open"`
		High        float64 `json:"high"`
		Low         float64 `json:"low"`
		PrevClose   float64 `json:"prev_close"`
		Volume      int64   `json:"volume"`
		Amount      float64 `json:"amount"`
		Bid1, Bid2, Bid3, Bid4, Bid5 float64 `json:"-"`
		BidV1, BidV2, BidV3, BidV4, BidV5 int64 `json:"-"`
		Ask1, Ask2, Ask3, Ask4, Ask5 float64 `json:"-"`
		AskV1, AskV2, AskV3, AskV4, AskV5 int64 `json:"-"`
	}
	if err := s.getJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("akshare: GetQuote: %w", err)
	}
	if resp.Price == 0 && resp.Name == "" {
		return nil, datasource.ErrNotImplemented
	}
	q1 := &datasource.Quote{
		StockCode: stockCode,
		StockName: resp.Name,
		Price:     resp.Price,
		Open:      resp.Open,
		High:      resp.High,
		Low:       resp.Low,
		PrevClose: resp.PrevClose,
		Volume:    resp.Volume,
		Amount:    resp.Amount,
		Timestamp: time.Now().UTC(),
		Source:    s.Name(),
	}
	return q1, nil
}

// GetKLine implements datasource.Source.
func (s *Source) GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]datasource.KLine, error) {
	q := url.Values{}
	q.Set("code", stockCode)
	q.Set("period", period)
	q.Set("start", start.Format("2006-01-02"))
	q.Set("end", end.Format("2006-01-02"))
	endpoint := s.base + "/kline?" + q.Encode()

	var resp struct {
		KLines []struct {
			Day    string  `json:"day"`
			Open   float64 `json:"open"`
			High   float64 `json:"high"`
			Low    float64 `json:"low"`
			Close  float64 `json:"close"`
			Volume int64   `json:"volume"`
			Amount float64 `json:"amount"`
		} `json:"klines"`
	}
	if err := s.getJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("akshare: GetKLine: %w", err)
	}
	if len(resp.KLines) == 0 {
		return nil, datasource.ErrNotImplemented
	}
	out := make([]datasource.KLine, 0, len(resp.KLines))
	for _, e := range resp.KLines {
		ts, err := time.Parse("2006-01-02", e.Day)
		if err != nil {
			continue
		}
		out = append(out, datasource.KLine{
			StockCode: stockCode,
			Period:    period,
			Timestamp: ts,
			Open:      e.Open,
			High:      e.High,
			Low:       e.Low,
			Close:     e.Close,
			Volume:    e.Volume,
			Amount:    e.Amount,
			Source:    s.Name(),
		})
	}
	return out, nil
}

// GetFundamental implements datasource.Source.
func (s *Source) GetFundamental(ctx context.Context, stockCode string) (*datasource.Fundamental, error) {
	q := url.Values{}
	q.Set("code", stockCode)
	endpoint := s.base + "/fundamental?" + q.Encode()

	var resp struct {
		Code          string  `json:"code"`
		Name          string  `json:"name"`
		PE            float64 `json:"pe"`
		PB            float64 `json:"pb"`
		ROE           float64 `json:"roe"`
		DividendYield float64 `json:"dividend_yield"`
		Revenue       float64 `json:"revenue"`
		NetProfit     float64 `json:"net_profit"`
	}
	if err := s.getJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("akshare: GetFundamental: %w", err)
	}
	if resp.Name == "" {
		return nil, datasource.ErrNotImplemented
	}
	return &datasource.Fundamental{
		StockCode:     stockCode,
		StockName:     resp.Name,
		PE:            resp.PE,
		PB:            resp.PB,
		ROE:           resp.ROE,
		DividendYield: resp.DividendYield,
		Revenue:       resp.Revenue,
		NetProfit:     resp.NetProfit,
		UpdateTime:    time.Now().UTC(),
		Source:        s.Name(),
	}, nil
}

// GetNews implements datasource.Source.
func (s *Source) GetNews(ctx context.Context, stockCode string, limit int) ([]datasource.News, error) {
	q := url.Values{}
	q.Set("code", stockCode)
	q.Set("limit", strconv.Itoa(limit))
	endpoint := s.base + "/news?" + q.Encode()

	var resp struct {
		News []struct {
			Title       string    `json:"title"`
			Content     string    `json:"content"`
			URL         string    `json:"url"`
			PublishedAt time.Time `json:"published_at"`
		} `json:"news"`
	}
	if err := s.getJSON(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("akshare: GetNews: %w", err)
	}
	if len(resp.News) == 0 {
		return nil, datasource.ErrNotImplemented
	}
	out := make([]datasource.News, 0, len(resp.News))
	for _, n := range resp.News {
		out = append(out, datasource.News{
			StockCode:   stockCode,
			Title:       n.Title,
			Content:     n.Content,
			URL:         n.URL,
			PublishedAt: n.PublishedAt,
			Source:      s.Name(),
		})
	}
	return out, nil
}

// getJSON issues a GET and decodes JSON.
func (s *Source) getJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
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
