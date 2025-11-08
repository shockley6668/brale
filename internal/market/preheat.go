package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"brale/internal/logger"
)

// 中文说明：
// 预热器：进程启动时，使用 REST 拉取最近 N 根 K 线，避免 WS 冷启动期间上下文为空。

type Preheater struct {
	Store   KlineStore
	Max     int
	Client  *http.Client
	BaseURL string // https://fapi.binance.com
}

func NewPreheater(s KlineStore, max int) *Preheater {
	return &Preheater{Store: s, Max: max, Client: &http.Client{Timeout: 15 * time.Second}, BaseURL: "https://fapi.binance.com"}
}

// Preheat 执行预热：对每个 symbol+interval 拉取最近 max 根并写入存储
func (p *Preheater) Preheat(ctx context.Context, symbols, intervals []string, limit int) {
	if p.Store == nil {
		return
	}
	if limit <= 0 {
		limit = p.Max
	}
	if limit <= 0 {
		limit = 100
	}
	for _, sym := range symbols {
		for _, iv := range intervals {
			batch, err := p.fetchKlines(ctx, sym, iv, limit)
			if err != nil {
				logger.Warnf("[预热] 获取 %s %s 失败: %v", sym, iv, err)
				continue
			}
			if err := p.Store.Put(ctx, sym, iv, batch, p.Max); err != nil {
				logger.Warnf("[预热] 写入 %s %s 失败: %v", sym, iv, err)
				continue
			}
			if len(batch) > 0 {
				first := batch[0]
				last := batch[len(batch)-1]
				fT := time.UnixMilli(first.CloseTime).UTC().Format(time.RFC3339)
				lT := time.UnixMilli(last.CloseTime).UTC().Format(time.RFC3339)
				logger.Debugf("[预热] %s %s 条数=%d 首收=%.4f@%d(%s) 尾收=%.4f@%d(%s)", sym, iv, len(batch), first.Close, first.CloseTime, fT, last.Close, last.CloseTime, lT)
			} else {
				logger.Debugf("[预热] %s %s 条数=0", sym, iv)
			}
		}
	}
}

// Warmup 根据每个周期的需求条数拉取历史，确保初次指标计算可用。
func (p *Preheater) Warmup(ctx context.Context, symbols []string, lookbacks map[string]int) {
	if p.Store == nil || len(lookbacks) == 0 {
		return
	}
	const maxLimit = 1500
	for _, sym := range symbols {
		for tf, need := range lookbacks {
			needBars := need
			if needBars <= 0 {
				needBars = 200
			}
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				cur, err := p.Store.Get(ctx, sym, tf)
				if err != nil {
					logger.Warnf("[warmup] 获取缓存 %s %s 失败: %v", sym, tf, err)
					break
				}
				if len(cur) >= needBars {
					logger.Infof("[warmup] %s %s ready (%d/%d)", sym, tf, len(cur), needBars)
					break
				}
				limit := needBars - len(cur)
				if limit < 50 {
					limit = 50
				}
				if limit > maxLimit {
					limit = maxLimit
				}
				batch, err := p.fetchKlines(ctx, sym, tf, limit)
				if err != nil {
					logger.Warnf("[warmup] 拉取 %s %s 失败: %v", sym, tf, err)
					break
				}
				if len(batch) == 0 {
					logger.Warnf("[warmup] 拉取 %s %s 得到空数据", sym, tf)
					break
				}
				keep := p.Max
				if keep < needBars+50 {
					keep = needBars + 50
				}
				if err := p.Store.Put(ctx, sym, tf, batch, keep); err != nil {
					logger.Warnf("[warmup] 写入 %s %s 失败: %v", sym, tf, err)
					break
				}
				logger.Debugf("[warmup] %s %s 拉取 %d 条，目前=%d/%d", sym, tf, len(batch), len(cur)+len(batch), needBars)
			}
		}
	}
}

// fetchKlines 使用 Binance REST /fapi/v1/klines
func (p *Preheater) fetchKlines(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	url := fmt.Sprintf("%s/fapi/v1/klines?symbol=%s&interval=%s&limit=%d", p.BaseURL, symbol, interval, limit)
	logger.Debugf("[预热] REST %s", url)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// klines 为 [[openTime,open,high,low,close,volume,closeTime,...], ...]
	var arr [][]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, err
	}
	out := make([]Candle, 0, len(arr))
	for _, k := range arr {
		// 简化解析：按索引取需要的字段
		// openTime(0), open(1), high(2), low(3), close(4), volume(5), closeTime(6)
		kl := Candle{}
		if v, ok := k[0].(float64); ok {
			kl.OpenTime = int64(v)
		}
		if v, ok := k[6].(float64); ok {
			kl.CloseTime = int64(v)
		}
		parseF := func(x any) float64 {
			switch t := x.(type) {
			case string:
				var f float64
				fmt.Sscanf(t, "%f", &f)
				return f
			case float64:
				return t
			default:
				return 0
			}
		}
		kl.Open = parseF(k[1])
		kl.High = parseF(k[2])
		kl.Low = parseF(k[3])
		kl.Close = parseF(k[4])
		kl.Volume = parseF(k[5])
		out = append(out, kl)
	}
	return out, nil
}