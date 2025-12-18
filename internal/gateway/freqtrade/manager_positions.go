package freqtrade

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
)

// ListFreqtradePositions implements livehttp.FreqtradeWebhookHandler.
func (m *Manager) ListFreqtradePositions(ctx context.Context, opts exchange.PositionListOptions) (exchange.PositionListResult, error) {
	if m.posRepo == nil {
		return exchange.PositionListResult{}, fmt.Errorf("posRepo not initialized")
	}

	limit := opts.PageSize
	if limit <= 0 {
		limit = 100
	}
	page := opts.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	status := strings.ToLower(strings.TrimSpace(opts.Status))
	if status == "" {
		status = "active"
	}

	now := time.Now().UnixMilli()
	symbolFilter := strings.ToUpper(strings.TrimSpace(opts.Symbol))

	switch status {
	case "active", "open":
		if m.trader != nil {
			snap := m.trader.Snapshot()
			if snap != nil && len(snap.Positions) > 0 {
				var list []exchange.APIPosition
				for _, p := range snap.Positions {
					if p == nil || !p.IsOpen {
						continue
					}
					if symbolFilter != "" && !strings.EqualFold(p.Symbol, symbolFilter) {
						continue
					}
					list = append(list, exchangePositionToAPIPosition(*p, now))
				}
				sort.Slice(list, func(i, j int) bool {
					return list[i].OpenedAt > list[j].OpenedAt
				})
				total := len(list)
				if offset > total {
					offset = total
				}
				end := offset + limit
				if end > total {
					end = total
				}
				pageList := list[offset:end]
				m.hydrateAPIPositionExits(ctx, pageList)
				return exchange.PositionListResult{
					TotalCount: total,
					Page:       page,
					PageSize:   limit,
					Positions:  pageList,
				}, nil
			}
		}

		activeOrders, err := m.posRepo.ListActivePositions(ctx, 500)
		if err != nil {
			return exchange.PositionListResult{}, err
		}
		if symbolFilter != "" {
			filtered := activeOrders[:0]
			for _, rec := range activeOrders {
				if strings.EqualFold(rec.Symbol, symbolFilter) {
					filtered = append(filtered, rec)
				}
			}
			activeOrders = filtered
		}
		total := len(activeOrders)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		activeOrders = activeOrders[offset:end]

		positions := make([]exchange.APIPosition, 0, len(activeOrders))
		for _, o := range activeOrders {
			positions = append(positions, liveOrderToAPIPosition(o, now))
		}
		m.hydrateAPIPositionExits(ctx, positions)
		return exchange.PositionListResult{
			TotalCount: total,
			Page:       page,
			PageSize:   limit,
			Positions:  positions,
		}, nil

	case "closed":
		fetch := offset + limit
		if fetch < 50 {
			fetch = 50
		}
		if fetch > 1000 {
			fetch = 1000
		}
		recs, err := m.posRepo.ListRecentPositionsPaged(ctx, symbolFilter, fetch, 0)
		if err != nil {
			return exchange.PositionListResult{}, err
		}
		closed := make([]database.LiveOrderRecord, 0, len(recs))
		for _, rec := range recs {
			if rec.Status == database.LiveOrderStatusClosed {
				closed = append(closed, rec)
			}
		}
		total := len(closed)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		closed = closed[offset:end]

		positions := make([]exchange.APIPosition, 0, len(closed))
		for _, o := range closed {
			positions = append(positions, liveOrderToAPIPosition(o, now))
		}
		m.hydrateAPIPositionExits(ctx, positions)
		return exchange.PositionListResult{
			TotalCount: total,
			Page:       page,
			PageSize:   limit,
			Positions:  positions,
		}, nil

	case "all":
		fetch := offset + limit
		if fetch < 100 {
			fetch = 100
		}
		if fetch > 2000 {
			fetch = 2000
		}
		recs, err := m.posRepo.ListRecentPositionsPaged(ctx, symbolFilter, fetch, 0)
		if err != nil {
			return exchange.PositionListResult{}, err
		}
		total := len(recs)
		if offset > total {
			offset = total
		}
		end := offset + limit
		if end > total {
			end = total
		}
		recs = recs[offset:end]

		positions := make([]exchange.APIPosition, 0, len(recs))
		for _, o := range recs {
			positions = append(positions, liveOrderToAPIPosition(o, now))
		}
		m.hydrateAPIPositionExits(ctx, positions)
		return exchange.PositionListResult{
			TotalCount: total,
			Page:       page,
			PageSize:   limit,
			Positions:  positions,
		}, nil
	default:
		return exchange.PositionListResult{}, fmt.Errorf("unknown status: %s", status)
	}
}

// APIPositionByID returns a single position snapshot for admin UI.
func (m *Manager) APIPositionByID(ctx context.Context, tradeID int) (*exchange.APIPosition, error) {
	if tradeID <= 0 {
		return nil, fmt.Errorf("invalid trade_id")
	}
	if m == nil || m.posRepo == nil {
		return nil, fmt.Errorf("posRepo not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rec, ok, err := m.posRepo.GetPosition(ctx, tradeID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("position not found")
	}
	now := time.Now().UnixMilli()
	pos := liveOrderToAPIPosition(rec, now)
	m.hydrateAPIPositionExit(ctx, &pos)
	return &pos, nil
}

// RefreshAPIPosition reconciles the given trade against freqtrade and returns the latest API snapshot.
func (m *Manager) RefreshAPIPosition(ctx context.Context, tradeID int) (*exchange.APIPosition, error) {
	if tradeID <= 0 {
		return nil, fmt.Errorf("invalid trade_id")
	}
	if m == nil || m.posRepo == nil {
		return nil, fmt.Errorf("posRepo not initialized")
	}
	if m.client == nil {
		return nil, fmt.Errorf("freqtrade client not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.reconcileTrade(ctx, tradeID); err != nil && !errors.Is(err, errTradeNotFound) {
		return nil, err
	}
	return m.APIPositionByID(ctx, tradeID)
}

// ListOpenPositions returns all open positions mapped to exchange.Position.
func (m *Manager) ListOpenPositions(ctx context.Context) ([]exchange.Position, error) {
	if m.posRepo == nil {
		return nil, nil
	}
	recs, err := m.posRepo.ListActivePositions(ctx, 100)
	if err != nil {
		return nil, err
	}
	var out []exchange.Position
	for _, r := range recs {
		pos := exchange.Position{
			ID:                 strconv.Itoa(r.FreqtradeID),
			Symbol:             r.Symbol,
			Side:               r.Side,
			Amount:             valOrZero(r.Amount),
			InitialAmount:      valOrZero(r.InitialAmount),
			EntryPrice:         valOrZero(r.Price),
			Leverage:           valOrZero(r.Leverage),
			StakeAmount:        valOrZero(r.StakeAmount),
			IsOpen:             r.Status == 1,
			UnrealizedPnL:      valOrZero(r.UnrealizedPnLUSD),
			UnrealizedPnLRatio: valOrZero(r.UnrealizedPnLRatio),
			CurrentPrice:       valOrZero(r.CurrentPrice),
		}
		if r.StartTime != nil {
			pos.OpenedAt = *r.StartTime
		}
		out = append(out, pos)
	}
	return out, nil
}

// PositionsForAPI is a compatibility shim for livehttp handlers.
func (m *Manager) PositionsForAPI(ctx context.Context, opts exchange.PositionListOptions) (exchange.PositionListResult, error) {
	return m.ListFreqtradePositions(ctx, opts)
}

// TradeIDBySymbol returns the TradeID for the current position of the symbol.
func (m *Manager) TradeIDBySymbol(symbol string) (int, bool) {
	if m == nil || m.trader == nil {
		return 0, false
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return 0, false
	}
	snap := m.trader.Snapshot()
	if snap == nil {
		return 0, false
	}
	idStr := strings.TrimSpace(snap.TradeIDBySymbol(sym))
	if idStr == "" {
		return 0, false
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// EntryPriceBySymbol returns cached entry price for a symbol when available.
// It is used by the agent layer for notifications without leaking internal actor types.
func (m *Manager) EntryPriceBySymbol(symbol string) float64 {
	if m == nil {
		return 0
	}
	return m.lookupEntryPrice(context.Background(), 0, symbol)
}

// ListFreqtradeEvents implements livehttp.FreqtradeWebhookHandler.
func (m *Manager) ListFreqtradeEvents(ctx context.Context, tradeID int, limit int) ([]exchange.TradeEvent, error) {
	if m.trader != nil {
		if events := snapshotTradeEvents(m.trader.Snapshot(), tradeID, limit); events != nil {
			return events, nil
		}
	}
	if m.posRepo == nil {
		return nil, fmt.Errorf("posRepo not initialized")
	}
	recs, err := m.posRepo.TradeEvents(ctx, tradeID, limit)
	if err != nil {
		return nil, err
	}
	return convertToExchangeTradeEvents(recs), nil
}
