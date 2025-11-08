package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"brale/internal/market"
)

// BinanceSource 基于 Binance USDT 合约 REST /fapi/v1/klines。
type BinanceSource struct {
	baseURL string
	client  *http.Client
}

func NewBinanceSource(base string) *BinanceSource {
	if base == "" {
		base = "https://fapi.binance.com"
	}
	return &BinanceSource{
		baseURL: base,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *BinanceSource) Name() string { return "binance" }

func (b *BinanceSource) Fetch(ctx context.Context, req FetchRequest) ([]market.Candle, error) {
	if req.Symbol == "" || req.Interval == "" {
		return nil, fmt.Errorf("symbol/interval 不能为空")
	}
	limit := req.Limit
	if limit <= 0 || limit > 1500 {
		limit = 1000
	}
	u, _ := url.Parse(b.baseURL)
	u.Path = "/fapi/v1/klines"
	q := u.Query()
	q.Set("symbol", req.Symbol)
	q.Set("interval", req.Interval)
	q.Set("limit", strconv.Itoa(limit))
	if req.Start > 0 {
		q.Set("startTime", strconv.FormatInt(req.Start, 10))
	}
	if req.End > 0 {
		q.Set("endTime", strconv.FormatInt(req.End, 10))
	}
	u.RawQuery = q.Encode()

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("binance 返回状态码 %d", resp.StatusCode)
	}
	var raw [][]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]market.Candle, 0, len(raw))
	for _, row := range raw {
		if len(row) < 7 {
			continue
		}
		out = append(out, market.Candle{
			OpenTime:  toInt64(row[0]),
			Open:      toFloat(row[1]),
			High:      toFloat(row[2]),
			Low:       toFloat(row[3]),
			Close:     toFloat(row[4]),
			Volume:    toFloat(row[5]),
			CloseTime: toInt64(row[6]),
			Trades:    toInt64(row[8]),
		})
	}
	return out, nil
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case float64:
		return t
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch t := v.(type) {
	case string:
		i, _ := strconv.ParseInt(t, 10, 64)
		return i
	case float64:
		return int64(t)
	default:
		return 0
	}
}