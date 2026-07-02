// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
package datasource

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
)

// UniverseLister resolves a named stock universe (e.g. "hs300", "all",
// a board) into a list of canonical stock codes. Implemented by the
// concrete manager; the data_source node type-asserts for it so the
// core Manager interface (and its test mocks) stay unchanged.
type UniverseLister interface {
	ListUniverse(ctx context.Context, key string, limit int) ([]string, error)
}

// BatchQuoter fetches quotes for many codes in ONE call (the sidecar
// /spot endpoint serves the whole-market snapshot). Used by the
// data_source quote path to avoid N per-stock requests on a universe.
type BatchQuoter interface {
	BatchQuote(ctx context.Context, codes []string) ([]Quote, error)
}

// BatchQuote hits the akshare sidecar /spot endpoint (one cached call
// for the whole market) and returns quotes for the given codes.
func (m *manager) BatchQuote(ctx context.Context, codes []string) ([]Quote, error) {
	base := strings.TrimRight(m.cfg.UniverseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("datasource: batch quote not configured")
	}
	q := url.Values{}
	q.Set("codes", strings.Join(codes, ","))
	endpoint := base + "/spot?" + q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("datasource: batch quote: build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("datasource: batch quote: http do: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("datasource: batch quote: status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Quotes []struct {
			StockCode string  `json:"stock_code"`
			Name      string  `json:"name"`
			Price     float64 `json:"price"`
			Open      float64 `json:"open"`
			High      float64 `json:"high"`
			Low       float64 `json:"low"`
			PrevClose float64 `json:"prev_close"`
			Volume    int64   `json:"volume"`
			Amount    float64 `json:"amount"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("datasource: batch quote: decode: %w", err)
	}
	quotes := make([]Quote, 0, len(out.Quotes))
	for _, q := range out.Quotes {
		quotes = append(quotes, Quote{
			StockCode: q.StockCode, StockName: q.Name, Price: q.Price,
			Open: q.Open, High: q.High, Low: q.Low, PrevClose: q.PrevClose,
			Volume: q.Volume, Amount: q.Amount, Timestamp: time.Now().UTC(), Source: "akshare-spot",
		})
	}
	return quotes, nil
}

// ListUniverse hits the akshare sidecar's /universe endpoint. The
// sidecar maps a key (hs300 / sse50 / zz500 / all / …) to a list of
// codes; limit caps the result (0 = sidecar default). Returns an error
// when no UniverseURL is configured.
func (m *manager) ListUniverse(ctx context.Context, key string, limit int) ([]string, error) {
	base := strings.TrimRight(m.cfg.UniverseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("datasource: universe resolution not configured")
	}
	q := url.Values{}
	q.Set("key", key)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	endpoint := base + "/universe?" + q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("datasource: universe: build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("datasource: universe: http do: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("datasource: universe: status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("datasource: universe: decode: %w", err)
	}
	return out.Codes, nil
}
