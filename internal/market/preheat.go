package market

import (
	"context"
	"time"

	"brale/internal/logger"
)

type Preheater struct {
	Store  KlineStore
	Max    int
	Source Source
}

func NewPreheater(s KlineStore, max int, src Source) *Preheater {
	return &Preheater{Store: s, Max: max, Source: src}
}

func (p *Preheater) Preheat(ctx context.Context, symbols, intervals []string, limit int) {
	if p.Store == nil {
		return
	}
	if p.Source == nil {
		logger.Warnf("[预热] 未注入 Source，跳过 Preheat")
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
			batch, err := p.Source.FetchHistory(ctx, sym, iv, limit)
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

func (p *Preheater) Warmup(ctx context.Context, symbols []string, lookbacks map[string]int) {
	if p.Store == nil || len(lookbacks) == 0 {
		return
	}
	if p.Source == nil {
		logger.Warnf("[warmup] 未注入 Source，跳过 Warmup")
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
				batch, err := p.Source.FetchHistory(ctx, sym, tf, limit)
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
