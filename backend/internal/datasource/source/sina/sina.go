// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Package sina implements the datasource.Source contract against
// the Sina public HTTP API. Sina's hq.sinajs.cn endpoint is small and
// stable, and the fields it returns are widely used as a fallback
// source.
//
// The format of the response is a single line of comma-separated
// values per stock: var hq_str_sh600519="贵州茅台,1820.00,...";
//
// Sina does not cover fundamentals or news in the same form as
// eastmoney, so those methods return ErrNotImplemented; the manager
// will fall through to the next source.
package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/skyzhao/af/internal/datasource"
)

const (
	// DefaultBaseURL is the public Sina quote endpoint.
	DefaultBaseURL = "https://hq.sinajs.cn"
	// DefaultTimeout matches the project's 5s rule of thumb.
	DefaultTimeout = 5 * time.Second
)

// Options configures the Sina Source.
type Options struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	UserAgent  string
}

// Source is the sina implementation of datasource.Source.
type Source struct {
	base    string
	http    *http.Client
	ua      string
	healthy bool
}

// New builds a sina Source.
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
	if opts.UserAgent == "" {
		opts.UserAgent = "af-backend/0.1 (+sina-adapter)"
	}
	return &Source{
		base:    strings.TrimRight(opts.BaseURL, "/"),
		http:    opts.HTTPClient,
		ua:      opts.UserAgent,
		healthy: true,
	}
}

// Name implements datasource.Source.
func (s *Source) Name() string { return "sina" }

// IsHealthy implements datasource.Source.
func (s *Source) IsHealthy() bool { return s.healthy }

// MarkUnhealthy flips the in-process liveness flag.
func (s *Source) MarkUnhealthy(v bool) { s.healthy = !v }

// GetQuote implements datasource.Source.
func (s *Source) GetQuote(ctx context.Context, stockCode string) (*datasource.Quote, error) {
	symbol, err := sinaSymbol(stockCode)
	if err != nil {
		return nil, fmt.Errorf("sina: bad stock code %q: %w", stockCode, err)
	}
	endpoint := s.base + "/list=" + symbol

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("sina: build request: %w", err)
	}
	req.Header.Set("User-Agent", s.ua)
	req.Header.Set("Referer", "https://finance.sina.com.cn/")

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sina: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("sina: status %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sina: read body: %w", err)
	}
	q, err := parseSinaLine(string(body), stockCode)
	if err != nil {
		return nil, err
	}
	q.Source = s.Name()
	return q, nil
}

// GetKLine implements datasource.Source using Sina's day-level K-line
// endpoint. Intraday periods are not directly supported and return
// ErrNotImplemented.
func (s *Source) GetKLine(ctx context.Context, stockCode, period string, start, end time.Time) ([]datasource.KLine, error) {
	if period != "1d" && period != "1w" && period != "1M" {
		return nil, datasource.ErrNotImplemented
	}
	scale := 240
	switch period {
	case "1w":
		scale = 1680
	case "1M":
		scale = 7200
	}
	symbol, err := sinaSymbol(stockCode)
	if err != nil {
		return nil, fmt.Errorf("sina: bad stock code %q: %w", stockCode, err)
	}
	endpoint := fmt.Sprintf(
		"%s/%s?symbol=%s&scale=%d&ma=no&datalen=1023",
		strings.TrimRight(s.base, "/"), "api/json_v2.php/CN_MarketData.getKLineData",
		symbol, scale,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("sina: build request: %w", err)
	}
	req.Header.Set("User-Agent", s.ua)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sina: http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("sina: status %d", resp.StatusCode)
	}
	// The response is JSON: an object with a "data" array of
	// {day, open, high, low, close, volume} entries. We keep the
	// parsing minimal: a regex on the JSON is overkill, but
	// importing encoding/json to walk the array of typed objects
	// would be heavier. The below format is stable in practice.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sina: read body: %w", err)
	}
	return parseKLineJSON(string(raw), stockCode, period, start, end)
}

// GetFundamental returns ErrNotImplemented: Sina's free endpoints do
// not expose fundamentals.
func (s *Source) GetFundamental(ctx context.Context, stockCode string) (*datasource.Fundamental, error) {
	return nil, datasource.ErrNotImplemented
}

// GetNews returns ErrNotImplemented: Sina's free endpoints do not
// expose news.
func (s *Source) GetNews(ctx context.Context, stockCode string, limit int) ([]datasource.News, error) {
	return nil, datasource.ErrNotImplemented
}

// =============================================================================
// Parsing helpers
// =============================================================================

// sinaSymbol returns the sina list symbol ("sh600519" / "sz000001"
// / "bj430047") for an A-share code. It accepts both the suffixed
// canonical form ("600519.SH") and the bare 6-digit form via
// datasource.SplitCode, so the numeric part is always clean (no
// ".SH" leaking into the symbol).
func sinaSymbol(stockCode string) (string, error) {
	digits, market, err := datasource.SplitCode(stockCode)
	if err != nil {
		return "", err
	}
	switch market {
	case datasource.MarketSH:
		return "sh" + digits, nil
	case datasource.MarketSZ:
		return "sz" + digits, nil
	case datasource.MarketBJ:
		return "bj" + digits, nil
	}
	return "", fmt.Errorf("unrecognized market %q in %q", market, stockCode)
}

// varLineRE matches the variable name and the quoted value:
// var hq_str_sh600519="...";
var varLineRE = regexp.MustCompile(`var\s+([A-Za-z0-9_]+)="([^"]*)";?`)

// parseSinaLine parses the single-line response from Sina. The body
// may include multiple stocks if the caller requested them all
// (we only consume the first one in this Source).
func parseSinaLine(body, fallbackCode string) (*datasource.Quote, error) {
	match := varLineRE.FindStringSubmatch(body)
	if len(match) < 3 {
		return nil, datasource.ErrNotImplemented
	}
	parts := strings.Split(match[2], ",")
	if len(parts) < 32 {
		return nil, datasource.ErrNotImplemented
	}
	// Sina's field order is well-known. We index it directly.
	parseF := func(i int) float64 { return parseFloat(parts[i]) }
	parseI := func(i int) int64 { return int64(parseFloat(parts[i])) }

	q := &datasource.Quote{
		StockName: parts[0],
		Open:      parseF(1),
		PrevClose: parseF(2),
		Price:     parseF(3),
		High:      parseF(4),
		Low:       parseF(5),
		Timestamp: time.Now().UTC(),
	}
	q.StockCode = fallbackCode
	// Volume / amount.
	if len(parts) > 8 {
		q.Volume = parseI(8)
	}
	if len(parts) > 9 {
		q.Amount = parseF(9)
	}
	// Bid5 / Ask5: Sina returns them at indices 11..30 as
	// alternating price, volume pairs. We always emit a 5-element
	// ladder (zero-filled if the source has fewer).
	for i := 0; i < 5; i++ {
		pxIdx := 11 + i*2
		volIdx := 12 + i*2
		px := 0.0
		vol := int64(0)
		if pxIdx < len(parts) {
			px = parseF(pxIdx)
		}
		if volIdx < len(parts) {
			vol = parseI(volIdx)
		}
		q.Bid5 = append(q.Bid5, datasource.BidAsk{Price: px, Volume: vol})
	}
	for i := 0; i < 5; i++ {
		pxIdx := 21 + i*2
		volIdx := 22 + i*2
		px := 0.0
		vol := int64(0)
		if pxIdx < len(parts) {
			px = parseF(pxIdx)
		}
		if volIdx < len(parts) {
			vol = parseI(volIdx)
		}
		q.Ask5 = append(q.Ask5, datasource.BidAsk{Price: px, Volume: vol})
	}
	if q.StockName == "" || q.Price == 0 {
		return nil, datasource.ErrNotImplemented
	}
	return q, nil
}

// klineEntry mirrors Sina's K-line JSON entry. The fields appear in
// the order day, open, high, low, close, volume.
type klineEntry struct {
	Day    string  `json:"day"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

// klineResponse is the envelope for the K-line response.
type klineResponse struct {
	Data []klineEntry `json:"data"`
}

// parseKLineJSON parses the JSON envelope and filters to [start, end).
func parseKLineJSON(body, stockCode, period string, start, end time.Time) ([]datasource.KLine, error) {
	if body == "" {
		return nil, datasource.ErrNotImplemented
	}
	var resp klineResponse
	if err := jsonUnmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("sina: kline json: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, datasource.ErrNotImplemented
	}
	out := make([]datasource.KLine, 0, len(resp.Data))
	for _, e := range resp.Data {
		ts, err := time.Parse("2006-01-02", e.Day)
		if err != nil {
			continue
		}
		if ts.Before(start) || !ts.Before(end) {
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
			Source:    "sina",
		})
	}
	if len(out) == 0 {
		return nil, datasource.ErrNotImplemented
	}
	return out, nil
}

// jsonUnmarshal is a thin wrapper so the call site stays compact.
func jsonUnmarshal(body string, v interface{}) error {
	return json.Unmarshal([]byte(body), v)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
