package freqtrade

import (
	"context"
	"strings"

	"brale/internal/gateway/database"
	"brale/internal/store"
)

// PositionRepo 封装对 live_orders / live_tiers 的查询与更新。
type PositionRepo struct {
	store    store.Store
	posStore database.LivePositionStore
}

type strategyInstanceBatchLister interface {
	ListStrategyInstancesByTradeIDs(ctx context.Context, tradeIDs []int) (map[int][]database.StrategyInstanceRecord, error)
}

// NewPositionRepo 创建 repo。
func NewPositionRepo(kv store.Store, ps database.LivePositionStore) *PositionRepo {
	return &PositionRepo{
		store:    kv,
		posStore: ps,
	}
}

// ListActivePositions 查询当前未平仓或部分平仓的订单。
func (r *PositionRepo) ListActivePositions(ctx context.Context, limit int) ([]database.LiveOrderRecord, error) {
	if r.store != nil {
		uow, err := r.store.Begin(ctx)
		if err == nil {
			defer uow.Rollback()
			orders, err := uow.Orders().ListActive(ctx)
			if err == nil && len(orders) > 0 {
				var res []database.LiveOrderRecord
				for _, o := range orders {
					res = append(res, fromOrderModel(&o))
				}
				return res, nil
			}
		}
	}

	if r.posStore == nil {
		return nil, nil
	}
	return r.posStore.ListActivePositions(ctx, limit)
}

// ListRecentPositionsPaged 查询最近仓位（包含已平仓），按时间倒序分页。
func (r *PositionRepo) ListRecentPositionsPaged(ctx context.Context, symbol string, limit int, offset int) ([]database.LiveOrderRecord, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if r.posStore != nil {
		return r.posStore.ListRecentPositionsPaged(ctx, symbol, limit, offset)
	}

	// Fallback: store.OrderRepository 仅支持按 limit 查询，不支持分页/过滤。
	if r.store == nil {
		return nil, nil
	}
	fetch := limit + offset
	if fetch <= 0 {
		fetch = 100
	}
	uow, err := r.store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer uow.Rollback()
	orders, err := uow.Orders().ListRecent(ctx, fetch)
	if err != nil {
		return nil, err
	}
	out := make([]database.LiveOrderRecord, 0, len(orders))
	for _, o := range orders {
		rec := fromOrderModel(&o)
		if symbol != "" && !strings.EqualFold(rec.Symbol, symbol) {
			continue
		}
		out = append(out, rec)
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

// GetPosition 查询特定 tradeID 的仓位。
func (r *PositionRepo) GetPosition(ctx context.Context, tradeID int) (database.LiveOrderRecord, bool, error) {
	if r.store != nil {
		uow, err := r.store.Begin(ctx)
		if err == nil {
			defer uow.Rollback()
			order, err := uow.Orders().FindByID(ctx, tradeID)
			if err == nil && order != nil {
				return fromOrderModel(order), true, nil
			}
		}
	}

	if r.posStore == nil {
		return database.LiveOrderRecord{}, false, nil
	}
	return r.posStore.GetLivePosition(ctx, tradeID)
}

// SavePosition 保存仓位
func (r *PositionRepo) SavePosition(ctx context.Context, rec database.LiveOrderRecord) error {
	if r.store != nil {
		uow, err := r.store.Begin(ctx)
		if err == nil {
			model := toOrderModel(rec)
			if err := uow.Orders().Save(ctx, model); err == nil {
				uow.Commit()
			}
		}
	}

	if r.posStore == nil {
		return nil
	}
	return r.posStore.UpsertLiveOrder(ctx, rec)
}

// ListStrategyInstances list strategies
func (r *PositionRepo) ListStrategyInstances(ctx context.Context, tradeID int) ([]database.StrategyInstanceRecord, error) {
	if r.store != nil {
		uow, err := r.store.Begin(ctx)
		if err == nil {
			defer uow.Rollback()
			recs, err := uow.Strategies().FindByTradeID(ctx, tradeID)
			if err == nil && len(recs) > 0 {
				var out []database.StrategyInstanceRecord
				for _, m := range recs {
					out = append(out, fromStrategyModel(&m))
				}
				return out, nil
			}
		}
	}

	if r.posStore == nil {
		return nil, nil
	}
	return r.posStore.ListStrategyInstances(ctx, tradeID)
}

// ListStrategyInstancesByTradeIDs batches strategy instance lookup when the underlying store supports it.
// Falls back to individual queries otherwise.
func (r *PositionRepo) ListStrategyInstancesByTradeIDs(ctx context.Context, tradeIDs []int) (map[int][]database.StrategyInstanceRecord, error) {
	out := make(map[int][]database.StrategyInstanceRecord, len(tradeIDs))
	ids := make([]int, 0, len(tradeIDs))
	seen := make(map[int]struct{}, len(tradeIDs))
	for _, id := range tradeIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}

	if r.posStore != nil {
		if batcher, ok := r.posStore.(strategyInstanceBatchLister); ok {
			return batcher.ListStrategyInstancesByTradeIDs(ctx, ids)
		}
	}

	for _, id := range ids {
		recs, err := r.ListStrategyInstances(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(recs) > 0 {
			out[id] = recs
		}
	}
	return out, nil
}

// TradeEvents wraps ListTradeOperations
func (r *PositionRepo) TradeEvents(ctx context.Context, tradeID int, limit int) ([]database.TradeOperationRecord, error) {
	if r.store != nil {
		uow, err := r.store.Begin(ctx)
		if err == nil {
			defer uow.Rollback()
			logs, err := uow.Logs().ListTradeOperations(ctx, tradeID, limit)
			if err == nil && len(logs) > 0 {
				var out []database.TradeOperationRecord
				for _, l := range logs {
					out = append(out, fromOperationModel(&l))
				}
				return out, nil
			}
		}
	}

	if r.posStore == nil {
		return nil, nil
	}
	return r.posStore.ListTradeOperations(ctx, tradeID, limit)
}
