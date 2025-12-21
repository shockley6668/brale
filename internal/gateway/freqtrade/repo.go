package freqtrade

import (
	"context"
	"fmt"
	"strings"

	"brale/internal/gateway/database"
	"brale/internal/store"
)

type PositionRepo struct {
	store    store.Store
	posStore database.LivePositionStore
}

type strategyInstanceBatchLister interface {
	ListStrategyInstancesByTradeIDs(ctx context.Context, tradeIDs []int) (map[int][]database.StrategyInstanceRecord, error)
}

func NewPositionRepo(kv store.Store, ps database.LivePositionStore) *PositionRepo {
	return &PositionRepo{
		store:    kv,
		posStore: ps,
	}
}

func (r *PositionRepo) ListActivePositions(ctx context.Context, limit int) ([]database.LiveOrderRecord, error) {
	if r.posStore == nil {
		if r.store == nil {
			return nil, nil
		}
		uow, err := r.store.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = uow.Close() }()
		orders, err := uow.Orders().ListActive(ctx)
		if err != nil {
			return nil, err
		}
		var res []database.LiveOrderRecord
		for _, o := range orders {
			res = append(res, fromOrderModel(&o))
		}
		return res, nil
	}
	return r.posStore.ListActivePositions(ctx, limit)
}

func (r *PositionRepo) ListRecentPositionsPaged(ctx context.Context, symbol string, limit int, offset int) ([]database.LiveOrderRecord, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if r.posStore != nil {
		return r.posStore.ListRecentPositionsPaged(ctx, symbol, limit, offset)
	}

	if r.store == nil {
		return nil, nil
	}
	fetch := limit + offset
	if fetch <= 0 {
		fetch = 100
	}
	uow, err := r.store.Read(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = uow.Close() }()
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

func (r *PositionRepo) GetPosition(ctx context.Context, tradeID int) (database.LiveOrderRecord, bool, error) {
	if r.posStore == nil {
		if r.store == nil {
			return database.LiveOrderRecord{}, false, nil
		}
		uow, err := r.store.Read(ctx)
		if err != nil {
			return database.LiveOrderRecord{}, false, err
		}
		defer func() { _ = uow.Close() }()
		order, err := uow.Orders().FindByID(ctx, tradeID)
		if err != nil || order == nil {
			return database.LiveOrderRecord{}, false, err
		}
		return fromOrderModel(order), true, nil
	}
	return r.posStore.GetLivePosition(ctx, tradeID)
}

func (r *PositionRepo) SavePosition(ctx context.Context, rec database.LiveOrderRecord) error {
	var storeErr error
	if r.store != nil {
		uow, err := r.store.Begin(ctx)
		if err == nil {
			defer func() { _ = uow.Rollback() }()
			model := toOrderModel(rec)
			if err := uow.Orders().Save(ctx, model); err == nil {
				if err := uow.Commit(); err != nil {
					storeErr = err
				}
			}
		}
	}

	if r.posStore == nil {
		return storeErr
	}
	if err := r.posStore.UpsertLiveOrder(ctx, rec); err != nil {
		if storeErr != nil {
			return fmt.Errorf("persist order commit=%v upsert=%w", storeErr, err)
		}
		return err
	}
	return storeErr
}

func (r *PositionRepo) ListStrategyInstances(ctx context.Context, tradeID int) ([]database.StrategyInstanceRecord, error) {
	if r.posStore == nil {
		if r.store == nil {
			return nil, nil
		}
		uow, err := r.store.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = uow.Close() }()
		recs, err := uow.Strategies().FindByTradeID(ctx, tradeID)
		if err != nil {
			return nil, err
		}
		var out []database.StrategyInstanceRecord
		for _, m := range recs {
			out = append(out, fromStrategyModel(&m))
		}
		return out, nil
	}
	return r.posStore.ListStrategyInstances(ctx, tradeID)
}

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

func (r *PositionRepo) TradeEvents(ctx context.Context, tradeID int, limit int) ([]database.TradeOperationRecord, error) {
	if r.posStore == nil {
		if r.store == nil {
			return nil, nil
		}
		uow, err := r.store.Read(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = uow.Close() }()
		logs, err := uow.Logs().ListTradeOperations(ctx, tradeID, limit)
		if err != nil {
			return nil, err
		}
		var out []database.TradeOperationRecord
		for _, l := range logs {
			out = append(out, fromOperationModel(&l))
		}
		return out, nil
	}
	return r.posStore.ListTradeOperations(ctx, tradeID, limit)
}
