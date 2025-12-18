package freqtrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"brale/internal/config"
	"brale/internal/decision"
	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/gateway/notifier"
	"brale/internal/logger"
	"brale/internal/pkg/convert"
	"brale/internal/store"
	"brale/internal/strategy"
	"brale/internal/strategy/exit"
	exithandlers "brale/internal/strategy/exit/handlers"
	"brale/internal/trader"
)

// NOTE: exchange types are used now.

// Logger abstracts decision log writes (live_decision_logs & live_orders).
type Logger interface {
	Insert(ctx context.Context, rec database.DecisionLogRecord) (int64, error)
}

// TextNotifier describes a minimal text notification interface.
type TextNotifier interface {
	SendText(text string) error
}

// Manager provides freqtrade execution, logging, and position synchronization.
type Manager struct {
	client         *Client
	cfg            config.FreqtradeConfig
	logger         Logger
	store          store.Store
	posRepo        *PositionRepo
	posStore       database.LivePositionStore
	executor       exchange.Exchange
	balance        exchange.Balance
	planUpdateHook exchange.PlanUpdateHook
	// Trader Actor (Shadow Mode or Active)
	trader *trader.Trader

	openPlanMu    sync.Mutex
	openPlanCache map[string]cachedOpenPlan

	pendingMu sync.Mutex
	pending   map[int]*pendingState
	notifier  TextNotifier
}

type strategyChangeLogWriter interface {
	InsertStrategyChangeLog(ctx context.Context, rec database.StrategyChangeLogRecord) error
}

const (
	pendingStageOpening = "opening"
	pendingStageClosing = "closing"
	pendingTimeout      = 11 * time.Minute
	reconcileDelay      = 5 * time.Second
)

// NewManager creates a freqtrade execution manager.
// Returns an error if required dependencies are missing.
func NewManager(client *Client, cfg config.FreqtradeConfig, logStore Logger, posStore database.LivePositionStore, newStore store.Store, notifier TextNotifier, executor exchange.Exchange) (*Manager, error) {
	// Validate required dependencies
	if posStore == nil {
		// Try to extract from logStore if it implements the interface
		if ps, ok := logStore.(database.LivePositionStore); ok {
			posStore = ps
		} else {
			return nil, fmt.Errorf("posStore is required but not provided")
		}
	}

	if executor == nil {
		return nil, fmt.Errorf("executor is required but not provided")
	}

	initLiveOrderPnL(posStore)

	// Initialize Trader Actor with SQLite Event Store
	eventStore := trader.NewSQLiteEventStore(posStore)

	t := trader.NewTrader(executor, eventStore, posStore)
	if err := t.Recover(); err != nil {
		return nil, fmt.Errorf("trader state recovery failed: %w", err)
	}
	t.Start()

	return &Manager{
		client:        client,
		cfg:           cfg,
		logger:        logStore,
		store:         newStore,
		posStore:      posStore,
		posRepo:       NewPositionRepo(newStore, posStore),
		executor:      executor,
		trader:        t,
		notifier:      notifier,
		openPlanCache: make(map[string]cachedOpenPlan),
	}, nil
}

// Execute calls freqtrade actions based on the decision.
func (m *Manager) Execute(ctx context.Context, input decision.DecisionInput) error {
	if m.trader == nil {
		return fmt.Errorf("trader actor not initialized")
	}
	d := input.Decision
	payload, err := json.Marshal(d)
	if err != nil {
		return err
	}

	var evtType trader.EventType
	switch d.Action {
	case "open_long", "open_short":
		evtType = trader.EvtSignalEntry
	case "close_long", "close_short":
		evtType = trader.EvtSignalExit
	case "update_exit_plan":
		return nil
	default:
		return nil
	}

	if evtType == trader.EvtSignalEntry {
		side := "long"
		if d.Action == "open_short" {
			side = "short"
		}
		sp := trader.SignalEntryPayload{
			Order: exchange.OpenRequest{
				Symbol: d.Symbol,
				// Assuming adapter.OrderSideType was used here, now string
				// e.g. "buy"/"sell"
				Side:      side,
				OrderType: "limit",
				Price:     input.MarketPrice,
				Amount:    0,
			},
		}
		if p, err := json.Marshal(sp); err == nil {
			payload = p
		}
	}

	eventID := managerEventID(input.TraceID, "decision")
	m.trader.Send(trader.EventEnvelope{
		ID:        eventID,
		Type:      evtType,
		Payload:   payload,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(d.Symbol)),
	})
	return nil
}

// HandleWebhook handles webhook requests to update position and logs.
func (m *Manager) HandleWebhook(ctx context.Context, msg exchange.WebhookMessage) {
	// OrderRate not in updated WebhookMessage? Check types.go
	// Assuming it's not needed or use another field if available.
	// Removing it for now as it's not in exchange.WebhookMessage
	// logger.Debugf("webhook trade=%d status=%s side=%s rate=%.2f", msg.TradeID, msg.Status, msg.Side, msg.OrderRate)
	logger.Debugf("Freqtrade Webhook received: %s trade=%d", msg.Type, msg.TradeID)

	if m.trader == nil {
		return
	}

	// Payload conversion
	// We want to generate EventEnvelope
	var evtType trader.EventType
	var payload interface{}
	var postSend func()
	delay := time.Duration(0)
	tradeID := int(msg.TradeID)

	msgType := strings.ToLower(strings.TrimSpace(msg.Type))
	switch msgType {
	case "entry":
		evtType = trader.EvtPositionOpening
		createdAt := parseFreqtradeTime(msg.OpenDate)
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		payload = trader.PositionOpeningPayload{
			TradeID:   strconv.FormatInt(int64(msg.TradeID), 10),
			Symbol:    msg.Pair,
			Side:      msg.Direction,
			Stake:     float64(msg.StakeAmount),
			Leverage:  float64(msg.Leverage),
			Amount:    float64(msg.Amount),
			Price:     float64(msg.OpenRate), // Changed from msg.OrderRate
			CreatedAt: createdAt,
		}
		m.startPending(tradeID, pendingStageOpening)
	case "entry_fill", "entry_fill_info":
		evtType = trader.EvtPositionOpened
		openedAt := parseFreqtradeTime(msg.OpenDate)
		if openedAt.IsZero() {
			openedAt = time.Now()
		}
		openedPayload := trader.PositionOpenedPayload{
			TradeID:  strconv.FormatInt(int64(msg.TradeID), 10),
			Symbol:   msg.Pair,
			Side:     msg.Direction,
			Price:    float64(msg.OpenRate),
			Amount:   float64(msg.Amount),
			Stake:    float64(msg.StakeAmount),
			Leverage: float64(msg.Leverage),
			OpenedAt: openedAt,
		}
		payload = openedPayload
		m.clearPending(tradeID, pendingStageOpening)
		delay = reconcileDelay
		postSend = func() {
			m.initExitPlanOnEntryFill(ctx, tradeID, msg.Pair, float64(msg.OpenRate))
			if m.notifier != nil {
				go m.sendEntryFillNotification(ctx, msg, openedPayload)
			}
		}
	case "exit":
		evtType = trader.EvtPositionClosing
		reqAt := parseFreqtradeTime(msg.CloseDate)
		if reqAt.IsZero() {
			reqAt = time.Now()
		}
		payload = trader.PositionClosingPayload{
			TradeID:   strconv.FormatInt(int64(msg.TradeID), 10),
			Symbol:    msg.Pair,
			Side:      msg.Direction,
			CreatedAt: reqAt,
		}
		m.startPending(tradeID, pendingStageClosing)
	case "exit_fill", "exit_fill_info":
		evtType = trader.EvtPositionClosed
		closedAt := parseFreqtradeTime(msg.CloseDate)
		if closedAt.IsZero() {
			closedAt = time.Now()
		}
		profitAbs := convert.ToFloat64(msg.ProfitAbs)
		profitRatio := convert.ToFloat64(msg.ProfitRatio)
		reason := strings.TrimSpace(msg.ExitReason)
		if reason == "" {
			reason = strings.TrimSpace(msg.Reason)
		}
		executed := float64(msg.Amount)
		closedAmount, remaining := m.deriveCloseBreakdown(msg.Pair, executed)
		closedPayload := trader.PositionClosedPayload{
			TradeID:         strconv.FormatInt(int64(msg.TradeID), 10),
			Symbol:          msg.Pair,
			Side:            msg.Direction,
			Amount:          closedAmount,
			RemainingAmount: remaining,
			ClosePrice:      float64(msg.CloseRate),
			Reason:          reason,
			PnL:             profitAbs,
			PnLPct:          profitRatio,
			ClosedAt:        closedAt,
		}
		payload = closedPayload
		m.clearPending(tradeID, pendingStageClosing)
		delay = reconcileDelay
		postSend = func() {
			m.reconcileExitFillWithFreqtrade(ctx, msg, &closedPayload)
			// Finalize ALL strategies only if fully closed (to prevent killing active tiered strategies)
			if closedPayload.RemainingAmount <= 0.000001 {
				if err := m.posStore.FinalizeStrategies(ctx, int(msg.TradeID)); err != nil {
					logger.Warnf("Failed to finalize strategies for trade %d: %v", msg.TradeID, err)
				} else {
					logger.Infof("Finalized strategies for trade %d (Full Exit)", msg.TradeID)
				}
			} else {
				// Partial exit_fill: only confirm a single pending tier to avoid killing other pending tiers.
				if err := m.posStore.FinalizePendingStrategies(ctx, int(msg.TradeID)); err != nil {
					logger.Warnf("Failed to finalize pending strategies for trade %d: %v", msg.TradeID, err)
				}
				logger.Infof("Finalized pending strategies for trade %d (Partial Exit, Remaining: %.4f)", msg.TradeID, closedPayload.RemainingAmount)
			}
			// 通知 PlanScheduler：exit_fill 会推进 pending->done / 全量 done，
			// 若不刷新内存 watcher，会导致 pending 卡死或继续评估已全平的 trade。
			if m.planUpdateHook != nil {
				m.planUpdateHook.NotifyPlanUpdated(context.Background(), int(msg.TradeID))
			}

			if closedPayload.Amount > 0 && m.notifier != nil {
				go m.sendExitFillNotification(ctx, msg, closedPayload)
			}
		}
	default:
		// Ignore other types for now
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warnf("Failed to marshal webhook payload: %v", err)
		return
	}

	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "webhook"),
		Type:      evtType,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   tradeID,
		Symbol:    strings.ToUpper(strings.TrimSpace(msg.Pair)),
	})
	if delay > 0 {
		m.reconcileTradeAsyncWithDelay(tradeID, delay)
	} else if tradeID > 0 {
		m.reconcileTradeAsync(tradeID)
	}
	if postSend != nil {
		postSend()
	}
}

func (m *Manager) sendEntryFillNotification(ctx context.Context, msg exchange.WebhookMessage, payload trader.PositionOpenedPayload) {
	if m == nil || m.notifier == nil {
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(freqtradePairToSymbol(payload.Symbol)))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(freqtradePairToSymbol(msg.Pair)))
	}
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(msg.Pair))
	}
	tradeID, _ := strconv.Atoi(strings.TrimSpace(payload.TradeID))
	side := strings.ToLower(strings.TrimSpace(payload.Side))
	if side == "" {
		side = strings.ToLower(strings.TrimSpace(msg.Direction))
	}

	lines := []string{
		fmt.Sprintf("方向 %s · 杠杆 x%.0f", strings.ToUpper(side), payload.Leverage),
		fmt.Sprintf("成交价 %.4f", payload.Price),
	}
	if payload.Amount > 0 {
		lines = append(lines, fmt.Sprintf("数量 %.4f", payload.Amount))
	}
	if payload.Stake > 0 {
		lines = append(lines, fmt.Sprintf("仓位(USD) %.2f", payload.Stake))
	}

	if m.posRepo != nil && tradeID > 0 {
		recs, err := m.posRepo.ListStrategyInstances(ctx, tradeID)
		if err == nil && len(recs) > 0 {
			derived := deriveExitPricesFromStrategyInstances(recs, side, payload.Price)
			if derived.StopLoss > 0 {
				lines = append(lines, fmt.Sprintf("SL %.4f", derived.StopLoss))
			}
			if derived.TakeProfit > 0 {
				lines = append(lines, fmt.Sprintf("TP %.4f", derived.TakeProfit))
			}
		}
	}
	if tradeID > 0 {
		lines = append(lines, fmt.Sprintf("TradeID %d", tradeID))
	}

	msgBody := notifier.StructuredMessage{
		Icon:      "🚀",
		Title:     fmt.Sprintf("开仓完成：%s", symbol),
		Sections:  []notifier.MessageSection{{Title: "执行明细", Lines: lines}},
		Timestamp: time.Now().UTC(),
	}
	if err := m.notifier.SendText(msgBody.RenderMarkdown()); err != nil {
		logger.Warnf("Telegram 推送失败(entry_fill): %v", err)
	}
}

// ListFreqtradePositions 实现 FreqtradeWebhookHandler 接口
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
		// For UI reads (desk), prefer the in-memory trader snapshot to avoid DB stalls.
		// Falls back to DB when snapshot is unavailable or empty.
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

		// Active positions count is usually small; fetch all then slice for stable paging.
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

func (m *Manager) logPlanInit(ctx context.Context, tradeID int, planID, traceID, source string) {
	if m == nil || m.posStore == nil || tradeID <= 0 {
		return
	}
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return
	}
	writer, ok := m.posStore.(strategyChangeLogWriter)
	if !ok {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rec := database.StrategyChangeLogRecord{
		TradeID:         tradeID,
		InstanceID:      0,
		PlanID:          planID,
		PlanComponent:   "",
		ChangedField:    "plan_init",
		OldValue:        "",
		NewValue:        "",
		TriggerSource:   strings.TrimSpace(source),
		Reason:          fmt.Sprintf("初始化 Exit Plan: %s", planID),
		DecisionTraceID: strings.TrimSpace(traceID),
		CreatedAt:       time.Now(),
	}
	if err := writer.InsertStrategyChangeLog(ctx, rec); err != nil {
		logger.Warnf("freqtrade: 写 strategy_change_log(init) 失败 trade=%d plan=%s err=%v", tradeID, planID, err)
	}
}

// APIPositionByID returns a single position mapped for API usage.
// It prefers the local DB snapshot to avoid scanning paged lists and to support older closed trades.
func (m *Manager) APIPositionByID(ctx context.Context, tradeID int) (*exchange.APIPosition, error) {
	if tradeID <= 0 {
		return nil, fmt.Errorf("invalid trade_id")
	}
	if m == nil || m.posRepo == nil {
		return nil, fmt.Errorf("posRepo not initialized")
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
// Intended for admin UI "refresh PnL" button: refreshes a single position without a full list reload.
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
	// reconcileTrade persists the latest current_price + pnl fields into DB.
	if err := m.reconcileTrade(ctx, tradeID); err != nil && !errors.Is(err, errTradeNotFound) {
		return nil, err
	}
	return m.APIPositionByID(ctx, tradeID)
}

// CloseFreqtradePosition 实现接口
// CloseFreqtradePosition 实现接口
func (m *Manager) CloseFreqtradePosition(ctx context.Context, tradeID int, symbol, side string, closeRatio float64) error {
	if m.trader == nil {
		return fmt.Errorf("trader not initialized")
	}

	// Validate TradeID if provided to prevent cross-contamination
	if tradeID > 0 {
		if err := m.validateTradeForClose(ctx, tradeID, symbol); err != nil {
			return err
		}
	}

	payload := trader.SignalExitPayload{
		TradeID:        tradeID, // Pass explicit trade ID
		Symbol:         symbol,
		Side:           side,
		CloseRatio:     closeRatio,
		IsInitialRatio: true, // Defaulting to initial ratio as commonly used in tiered exits
	}

	data, _ := json.Marshal(payload)
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "signal-exit"),
		Type:      trader.EvtSignalExit,
		Payload:   data,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
	})
	return nil
}

// validateTradeForClose 验证 tradeID 是否可以平仓
func (m *Manager) validateTradeForClose(ctx context.Context, tradeID int, symbol string) error {
	if norm := freqtradePairToSymbol(symbol); norm != "" {
		symbol = norm
	}
	activeID, exists := m.TradeIDBySymbol(symbol)
	if exists {
		if activeID != tradeID {
			return fmt.Errorf("CloseFreqtradePosition: trade mismatch query=%d active=%d for %s", tradeID, activeID, symbol)
		}
		return nil // 内存中验证通过
	}

	// 内存中不存在，尝试 DB 查询确认（处理重启后状态恢复不完整的情况）
	if m.posRepo != nil {
		rec, ok, err := m.posRepo.GetPosition(ctx, tradeID)
		if err != nil {
			logger.Warnf("CloseFreqtradePosition: DB query failed for trade %d: %v", tradeID, err)
		}
		if ok && rec.Status != database.LiveOrderStatusClosed && rec.Status != database.LiveOrderStatusClosingFull {
			// DB 确认仓位存在且未关闭，允许继续
			logger.Warnf("CloseFreqtradePosition: trade %d not in memory but found in DB (status=%d), proceeding", tradeID, rec.Status)
			return nil
		}
	}

	return fmt.Errorf("CloseFreqtradePosition: trade %d not active for %s", tradeID, symbol)
}

// ListFreqtradeEvents 实现接口
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
	// Duplicated function in snapshot.go, can use that one if exported or duplicate here.
	// Since snapshot.go is in the same package, we can use convertToExchangeTradeEvents from there
	// IF it is available to manager.go (same package).
	// Checking snapshot.go, I defined convertToExchangeTradeEvents there.
	// So I can use it here directly.
	return convertToExchangeTradeEvents(recs), nil
}

// convertToExchangeTradeEvents removed from here as it overlaps with snapshot.go updates or need rename to avoid conflict if both persist.
// But wait, snapshot.go is in `freqtrade` package too.
// So I should NOT define `convertToExchangeTradeEvents` in `manager.go` if I defined it in `snapshot.go`.
// I will remove the local definition from manager.go in this edit.

// ManualOpenPosition 实现接口
func (m *Manager) ManualOpenPosition(ctx context.Context, req exchange.ManualOpenRequest) error {
	if m == nil || m.executor == nil {
		return fmt.Errorf("executor not initialized")
	}
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		return fmt.Errorf("symbol 必填")
	}
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side != "long" && side != "short" {
		return fmt.Errorf("side 需为 long 或 short")
	}
	if req.PositionSizeUSD <= 0 {
		return fmt.Errorf("position_size_usd 需 >0")
	}
	if req.Leverage <= 0 {
		return fmt.Errorf("leverage 需 >0")
	}
	entryPrice := req.EntryPrice
	if entryPrice <= 0 {
		return fmt.Errorf("entry_price 需 >0")
	}
	comboKey := strings.ToLower(strings.TrimSpace(req.ExitCombo))
	if comboKey == "" {
		return fmt.Errorf("exit_combo 必填")
	}

	tagParts := []string{"manual"}
	if strings.TrimSpace(req.ExitCombo) != "" {
		tagParts = append(tagParts, strings.TrimSpace(req.ExitCombo))
	}
	if strings.TrimSpace(req.Reason) != "" {
		tagParts = append(tagParts, strings.TrimSpace(req.Reason))
	}
	entryTag := strings.Join(tagParts, " | ")
	if len(entryTag) > 120 {
		entryTag = entryTag[:120]
	}

	openReq := exchange.OpenRequest{
		Symbol:    symbol,
		Side:      side,
		OrderType: "limit",
		Price:     entryPrice,
		// NOTE: freqtrade adapter 当前使用 Amount 作为 stakeamount（USDT），保持兼容。
		Amount: req.PositionSizeUSD,
		Tag:    entryTag,
	}
	if req.Leverage > 0 {
		openReq.Leverage = float64(req.Leverage)
	}

	result, err := m.executor.OpenPosition(ctx, openReq)
	if err != nil {
		return err
	}
	tradeID, _ := strconv.Atoi(strings.TrimSpace(result.PositionID))
	if tradeID <= 0 {
		return fmt.Errorf("freqtrade 未返回 trade_id")
	}

	planSpec, err := buildManualComboPlanSpec(req, entryPrice, side, comboKey)
	if err != nil {
		return err
	}

	reg := exit.NewHandlerRegistry()
	exithandlers.RegisterCoreHandlers(reg)
	comboHandler, _ := reg.Handler("combo_group")
	if comboHandler == nil {
		return fmt.Errorf("exit plan handler 未注册")
	}
	planID := "plan_combo_main"
	decisionTrace := fmt.Sprintf("manual-open:%d", tradeID)
	instances, err := comboHandler.Instantiate(ctx, exit.InstantiateArgs{
		TradeID:       tradeID,
		PlanID:        planID,
		PlanVersion:   1,
		PlanSpec:      planSpec,
		EntryPrice:    entryPrice,
		Side:          side,
		Symbol:        symbol,
		DecisionTrace: decisionTrace,
	})
	if err != nil {
		return err
	}
	if m.posStore == nil {
		return fmt.Errorf("posStore not initialized")
	}
	recs := make([]database.StrategyInstanceRecord, 0, len(instances))
	for _, inst := range instances {
		recs = append(recs, inst.Record)
	}
	if err := m.posStore.InsertStrategyInstances(ctx, recs); err != nil {
		return err
	}
	m.logPlanInit(ctx, tradeID, planID, decisionTrace, "manual_open")
	// Sync to trader snapshot for UI/调试（不影响 PlanScheduler 的 DB 轮询）。
	_ = m.SyncStrategyPlans(ctx, tradeID, buildPlanSnapshots(recs))
	// 通知 PlanScheduler 新 trade/plan 已创建（避免依赖定时 rebuild）。
	if m.planUpdateHook != nil {
		m.planUpdateHook.NotifyPlanUpdated(context.Background(), tradeID)
	}
	return nil
}

func buildManualComboPlanSpec(req exchange.ManualOpenRequest, entryPrice float64, side string, comboKey string) (map[string]any, error) {
	type tier struct {
		Target float64
		Ratio  float64
	}
	normalize := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	parts := strings.Split(normalize(comboKey), "__")
	if len(parts) < 2 {
		return nil, fmt.Errorf("exit_combo 格式错误: %s", comboKey)
	}
	hasTP := false
	hasSL := false
	seen := map[string]bool{}
	children := make([]any, 0, len(parts))

	buildTierParams := func(tiers []tier) (map[string]any, error) {
		if len(tiers) == 0 {
			return nil, fmt.Errorf("tiers 不能为空")
		}
		if len(tiers) > 3 {
			tiers = tiers[:3]
		}
		sum := 0.0
		raw := make([]any, 0, len(tiers))
		for _, t := range tiers {
			if t.Target <= 0 {
				return nil, fmt.Errorf("tier target_price 必须 >0")
			}
			if t.Ratio <= 0 || t.Ratio > 1 {
				return nil, fmt.Errorf("tier ratio 需位于 (0,1]")
			}
			sum += t.Ratio
			raw = append(raw, map[string]any{
				"target_price": t.Target,
				"ratio":        t.Ratio,
			})
		}
		if len(tiers) == 1 {
			// 单段兜底：ratio 强制为 1，避免用户误填。
			raw[0].(map[string]any)["ratio"] = 1.0
			sum = 1.0
		}
		if math.Abs(sum-1) > 1e-6 {
			return nil, fmt.Errorf("tiers 比例合计需为 1，当前=%.4f", sum)
		}
		return map[string]any{"tiers": raw}, nil
	}

	tpTiers := []tier{}
	if req.Tier1Target > 0 {
		tpTiers = append(tpTiers, tier{Target: req.Tier1Target, Ratio: req.Tier1Ratio})
	}
	if req.Tier2Target > 0 {
		tpTiers = append(tpTiers, tier{Target: req.Tier2Target, Ratio: req.Tier2Ratio})
	}
	if req.Tier3Target > 0 {
		tpTiers = append(tpTiers, tier{Target: req.Tier3Target, Ratio: req.Tier3Ratio})
	}

	slTiers := []tier{}
	if req.SLTier1Target > 0 {
		slTiers = append(slTiers, tier{Target: req.SLTier1Target, Ratio: req.SLTier1Ratio})
	}
	if req.SLTier2Target > 0 {
		slTiers = append(slTiers, tier{Target: req.SLTier2Target, Ratio: req.SLTier2Ratio})
	}
	if req.SLTier3Target > 0 {
		slTiers = append(slTiers, tier{Target: req.SLTier3Target, Ratio: req.SLTier3Ratio})
	}

	for _, raw := range parts {
		comp := normalize(raw)
		if comp == "" {
			continue
		}
		if seen[comp] {
			return nil, fmt.Errorf("exit_combo component 重复: %s", comp)
		}
		seen[comp] = true
		switch comp {
		case "tp_single":
			hasTP = true
			if req.TakeProfit <= 0 {
				return nil, fmt.Errorf("tp_single 需提供 take_profit")
			}
			params, err := buildTierParams([]tier{{Target: req.TakeProfit, Ratio: 1}})
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "tp_single",
				"handler":   "tier_take_profit",
				"params":    params,
			})
		case "tp_tiers":
			hasTP = true
			if len(tpTiers) == 0 {
				return nil, fmt.Errorf("tp_tiers 需提供 tier1/2/3 目标价")
			}
			params, err := buildTierParams(tpTiers)
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "tp_tiers",
				"handler":   "tier_take_profit",
				"params":    params,
			})
		case "sl_single":
			hasSL = true
			if req.StopLoss <= 0 {
				return nil, fmt.Errorf("sl_single 需提供 stop_loss")
			}
			params, err := buildTierParams([]tier{{Target: req.StopLoss, Ratio: 1}})
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "sl_single",
				"handler":   "tier_stop_loss",
				"params":    params,
			})
		case "sl_tiers":
			hasSL = true
			if len(slTiers) == 0 {
				return nil, fmt.Errorf("sl_tiers 需提供 sl_tier1/2/3 目标价（不再自动生成）")
			}
			params, err := buildTierParams(slTiers)
			if err != nil {
				return nil, err
			}
			children = append(children, map[string]any{
				"component": "sl_tiers",
				"handler":   "tier_stop_loss",
				"params":    params,
			})
		case "tp_atr", "sl_atr":
			return nil, fmt.Errorf("手动开仓暂不支持 ATR 组件: %s", comp)
		default:
			return nil, fmt.Errorf("未知 exit_combo component: %s", comp)
		}
	}
	if !hasTP || !hasSL {
		return nil, fmt.Errorf("exit_combo 必须同时包含止盈(tp_*)与止损(sl_*)组件")
	}
	return map[string]any{"children": children}, nil
}

// GetLatestPriceQuote 实现接口
func (m *Manager) GetLatestPriceQuote(ctx context.Context, symbol string) (exchange.PriceQuote, error) {
	return exchange.PriceQuote{}, nil
}

// RefreshBalance fetches balance from client
func (m *Manager) RefreshBalance(ctx context.Context) (exchange.Balance, error) {
	if m.client == nil {
		return exchange.Balance{}, fmt.Errorf("client not initialized")
	}
	bal, err := m.client.GetBalance(ctx)
	if err != nil {
		return exchange.Balance{}, err
	}
	m.balance = bal
	return bal, nil
}

// PublishPrice forwards price updates to the Trader actor
func (m *Manager) PublishPrice(symbol string, quote exchange.PriceQuote) {
	if m.trader == nil {
		return
	}
	payload, _ := json.Marshal(trader.PriceUpdatePayload{
		Symbol: symbol,
		Quote: strategy.MarketQuote{
			Last: quote.Last,
			High: quote.High,
			Low:  quote.Low,
		},
	})
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "price"),
		Type:      trader.EvtPriceUpdate,
		Payload:   payload,
		CreatedAt: time.Now(),
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
	})
}

// PublishPlanStateUpdate emits a plan state update event to the trader actor.
func (m *Manager) PublishPlanStateUpdate(ctx context.Context, payload exchange.PlanStateUpdatePayload) error {
	if m.trader == nil {
		return fmt.Errorf("trader not initialized")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "plan-state"),
		Type:      trader.EvtPlanStateUpdate,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   payload.TradeID,
	})
	return nil
}

func (m *Manager) reconcileTradeAsync(tradeID int) {
	m.reconcileTradeAsyncWithDelay(tradeID, 0)
}

func (m *Manager) reconcileTradeAsyncWithDelay(tradeID int, delay time.Duration) {
	if m == nil || m.client == nil || m.posRepo == nil || tradeID <= 0 {
		return
	}
	go func(id int, wait time.Duration) {
		if wait > 0 {
			time.Sleep(wait)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.reconcileTrade(ctx, id); err != nil && !errors.Is(err, errTradeNotFound) {
			logger.Warnf("freqtrade: reconcile failed trade=%d err=%v", id, err)
		}
	}(tradeID, delay)
}

func (m *Manager) reconcileTrade(ctx context.Context, tradeID int) error {
	if m == nil || m.client == nil || m.posRepo == nil {
		return fmt.Errorf("reconcile dependencies unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Prefer /status for open trades: some /trades payloads omit is_open and would be misclassified.
	trade, err := m.client.GetOpenTrade(ctx, tradeID)
	if err != nil {
		if !errors.Is(err, errTradeNotFound) {
			return err
		}
		trade, err = m.client.GetTrade(ctx, tradeID)
		if err != nil {
			return err
		}
	}
	if trade == nil {
		return fmt.Errorf("trade %d not found", tradeID)
	}
	rec := tradeToLiveRecord(trade)
	if rec.FreqtradeID == 0 {
		rec.FreqtradeID = tradeID
	}
	if strings.TrimSpace(rec.Symbol) == "" {
		rec.Symbol = strings.ToUpper(strings.TrimSpace(trade.Pair))
	}
	return m.posRepo.SavePosition(ctx, rec)
}

// Positions returns active positions for decision engine
func (m *Manager) Positions() []decision.PositionSnapshot {
	if m.trader != nil {
		snap := m.trader.Snapshot()
		return snapshotDecisionPositions(snap)
	}
	if m.posRepo == nil {
		return nil
	}
	recs, err := m.posRepo.ListActivePositions(context.Background(), 100)
	if err != nil {
		logger.Warnf("failed to list active positions: %v", err)
		return nil
	}
	var out []decision.PositionSnapshot
	now := time.Now().UnixMilli()
	for _, r := range recs {
		holdingMs := int64(0)
		if r.StartTime != nil {
			holdingMs = now - r.StartTime.UnixMilli()
		}
		out = append(out, decision.PositionSnapshot{
			Symbol:          r.Symbol,
			Side:            r.Side,
			Quantity:        valOrZero(r.Amount),
			EntryPrice:      valOrZero(r.Price),
			CurrentPrice:    valOrZero(r.CurrentPrice),
			UnrealizedPn:    valOrZero(r.UnrealizedPnLUSD),
			UnrealizedPnPct: valOrZero(r.UnrealizedPnLRatio) * 100,
			HoldingMs:       holdingMs,
			Leverage:        valOrZero(r.Leverage),
			Stake:           valOrZero(r.StakeAmount),
		})
	}
	return out
}

type PlanAdjustRequest struct {
	TradeID       int
	PlanID        string
	PlanComponent string
	Status        database.StrategyStatus
	ParamsJSON    string
	StateJSON     string
}

func (m *Manager) AdjustPlan(ctx context.Context, req PlanAdjustRequest) error {
	return nil
}

// AccountBalance returns cached balance
func (m *Manager) AccountBalance() exchange.Balance {
	return m.balance
}

// CacheDecision implements ExecutionManager interface.
// 目前用于在开仓时缓存 exit_plan，供 entry_fill webhook 生成 strategy_instances。
func (m *Manager) CacheDecision(key string, d decision.Decision) string {
	m.cacheOpenExitPlan(key, d)
	return key
}

// PositionsForAPI stub
func (m *Manager) PositionsForAPI(ctx context.Context, opts exchange.PositionListOptions) (exchange.PositionListResult, error) {
	return m.ListFreqtradePositions(ctx, opts)
}

// ListOpenPositions returns all open positions mapped to exchange.Position
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
		// Map database record to exchange.Position
		pos := exchange.Position{
			ID:                 strconv.Itoa(r.FreqtradeID),
			Symbol:             r.Symbol,
			Side:               r.Side,
			Amount:             valOrZero(r.Amount),
			InitialAmount:      valOrZero(r.InitialAmount),
			EntryPrice:         valOrZero(r.Price),
			Leverage:           valOrZero(r.Leverage),
			StakeAmount:        valOrZero(r.StakeAmount),
			IsOpen:             r.Status == 1, // assuming 1 is open
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

func (m *Manager) reconcileExitFillWithFreqtrade(ctx context.Context, msg exchange.WebhookMessage, payload *trader.PositionClosedPayload) {
	if m == nil || m.client == nil || payload == nil {
		return
	}
	tradeID, err := strconv.Atoi(strings.TrimSpace(payload.TradeID))
	if err != nil || tradeID <= 0 {
		return
	}
	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	statusCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	openTrade, err := m.client.GetOpenTrade(statusCtx, tradeID)
	cancel()
	switch {
	case err == nil && openTrade != nil:
		remoteRemaining := clampAmount(openTrade.Amount)
		if math.Abs(remoteRemaining-payload.RemainingAmount) > 1e-6 {
			logger.Warnf("freqtrade: trade=%d remaining mismatch，本地=%.4f 远端=%.4f，已使用远端数据", tradeID, payload.RemainingAmount, remoteRemaining)
			payload.RemainingAmount = remoteRemaining
		}
		return
	case err != nil && !errors.Is(err, errTradeNotFound):
		logger.Warnf("freqtrade: 查询 /status 失败 trade=%d err=%v", tradeID, err)
		return
	}
	historyCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
	trade, err := m.client.GetTrade(historyCtx, tradeID)
	cancel()
	if err != nil {
		if !errors.Is(err, errTradeNotFound) {
			logger.Warnf("freqtrade: 查询历史 trades 失败 trade=%d err=%v", tradeID, err)
		}
		return
	}
	if trade == nil {
		return
	}
	payload.RemainingAmount = 0
	if trade.CloseRate > 0 {
		payload.ClosePrice = trade.CloseRate
	}
	if trade.CloseProfitAbs != 0 {
		payload.PnL = trade.CloseProfitAbs
	} else if trade.ProfitAbs != 0 {
		payload.PnL = trade.ProfitAbs
	}
	if trade.CloseProfit != 0 {
		payload.PnLPct = trade.CloseProfit
	} else if trade.ProfitRatio != 0 {
		payload.PnLPct = trade.ProfitRatio
	}
}

// SyncStrategyPlans sends plan updates to Trader
func (m *Manager) SyncStrategyPlans(ctx context.Context, tradeID int, plans any) error {
	if m.trader == nil {
		return nil
	}
	// Validate type if needed, but for now just pass to payload
	// The payload expects []exit.StrategyPlanSnapshot.
	// We need to cast it or change payload to any?
	// trader.SyncPlansPayload has Plans []exit.StrategyPlanSnapshot.
	// So we must cast.
	planSnapshots, ok := plans.([]exit.StrategyPlanSnapshot)
	if !ok {
		return fmt.Errorf("invalid plans type, expected []exit.StrategyPlanSnapshot")
	}

	payload := trader.SyncPlansPayload{
		TradeID: tradeID,
		Version: time.Now().UnixNano(),
		Plans:   planSnapshots,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.trader.Send(trader.EventEnvelope{
		ID:        managerEventID("", "sync-plans"),
		Type:      trader.EvtSyncPlans,
		Payload:   data,
		CreatedAt: time.Now(),
		TradeID:   tradeID,
	})
	return nil
}

// TraderActor 返回当前 manager 使用的 Trader actor。
// 主要用于测试/集成环境，便于直接读取快照或控制 actor 生命周期。
func (m *Manager) TraderActor() interface{} {
	if m == nil {
		return nil
	}
	return m.trader
}

// PositionStore returns the underlying LivePositionStore used by the manager.
func (m *Manager) PositionStore() database.LivePositionStore {
	if m == nil {
		return nil
	}
	return m.posStore
}

func managerEventID(seed, prefix string) string {
	seed = strings.TrimSpace(seed)
	if seed != "" {
		return seed
	}
	if prefix == "" {
		prefix = "evt"
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func initLiveOrderPnL(store database.LivePositionStore) {
	if store == nil {
		return
	}
	if err := store.AddOrderPnLColumns(); err != nil {
		logger.Warnf("freqtrade manager: pnl storage init failed: %v", err)
	}
}

// SetPlanUpdateHook sets the hook for plan updates
func (m *Manager) SetPlanUpdateHook(hook exchange.PlanUpdateHook) {
	m.planUpdateHook = hook
}

// NotifyPlanUpdated is called by PlanScheduler when a plan changes.
func (m *Manager) NotifyPlanUpdated(ctx context.Context, tradeID int) {
	// We need to fetch the plan? Or just trigger sync?
	// SyncStrategyPlans requires plan snapshots.
	recs, err := m.posStore.ListStrategyInstances(ctx, tradeID)
	if err != nil {
		logger.Warnf("NotifyPlanUpdated: failed to list plans for %d: %v", tradeID, err)
		return
	}
	snapshots := buildPlanSnapshots(recs)
	m.SyncStrategyPlans(ctx, tradeID, snapshots)
}

// buildPlanSnapshots converts database records to adapter plan snapshots
func buildPlanSnapshots(recs []database.StrategyInstanceRecord) []exit.StrategyPlanSnapshot {
	if len(recs) == 0 {
		return nil
	}
	out := make([]exit.StrategyPlanSnapshot, 0, len(recs))
	for _, rec := range recs {
		out = append(out, exit.StrategyPlanSnapshot{
			PlanID:          rec.PlanID,
			PlanComponent:   rec.PlanComponent,
			PlanVersion:     rec.PlanVersion,
			StatusLabel:     exit.StatusLabel(rec.Status),
			StatusCode:      int(rec.Status),
			ParamsJSON:      rec.ParamsJSON,
			StateJSON:       rec.StateJSON,
			DecisionTraceID: rec.DecisionTraceID,
			CreatedAt:       rec.CreatedAt.Unix(),
			UpdatedAt:       rec.UpdatedAt.Unix(),
		})
	}
	return out
}

func (m *Manager) startPending(tradeID int, stage string) {
	if tradeID <= 0 {
		return
	}
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if m.pending == nil {
		m.pending = make(map[int]*pendingState)
	}
	if prev, ok := m.pending[tradeID]; ok {
		if prev.timer != nil {
			prev.timer.Stop()
		}
	}
	timer := time.AfterFunc(pendingTimeout, func() {
		m.handlePendingTimeout(tradeID, stage)
	})
	m.pending[tradeID] = &pendingState{stage: stage, timer: timer}
}

func (m *Manager) clearPending(tradeID int, stage string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	if m.pending == nil {
		return
	}
	if ps, ok := m.pending[tradeID]; ok {
		if ps.stage == stage && ps.timer != nil {
			ps.timer.Stop()
		}
		delete(m.pending, tradeID)
	}
}

func (m *Manager) handlePendingTimeout(tradeID int, stage string) {
	switch stage {
	case pendingStageOpening:
		logger.Warnf("freqtrade: 开仓超时 trade=%d，回退状态为失败", tradeID)
		m.updateOrderStatus(tradeID, database.LiveOrderStatusRetrying)
	case pendingStageClosing:
		logger.Warnf("freqtrade: 平仓超时 trade=%d，回退状态为 open", tradeID)
		m.updateOrderStatus(tradeID, database.LiveOrderStatusOpen)
	default:
	}
	m.pendingMu.Lock()
	if m.pending != nil {
		delete(m.pending, tradeID)
	}
	m.pendingMu.Unlock()
}

func (m *Manager) updateOrderStatus(tradeID int, status database.LiveOrderStatus) {
	if m == nil || m.posStore == nil || tradeID <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.posStore.UpdateOrderStatus(ctx, tradeID, status); err != nil {
		logger.Warnf("update order status failed trade=%d status=%d err=%v", tradeID, status, err)
	}
}

func (m *Manager) currentPositionAmount(symbol string) float64 {
	if m == nil || m.trader == nil {
		return 0
	}
	snap := m.trader.Snapshot()
	if snap == nil || snap.Positions == nil {
		return 0
	}
	key := freqtradePairToSymbol(symbol)
	if key == "" {
		key = strings.ToUpper(strings.TrimSpace(symbol))
	}
	pos, ok := snap.Positions[key]
	if !ok || pos == nil {
		return 0
	}
	return pos.Amount
}

func (m *Manager) deriveCloseBreakdown(symbol string, executed float64) (float64, float64) {
	current := m.currentPositionAmount(symbol)
	if current <= 0 {
		return executed, 0
	}
	closed := executed
	if closed <= 0 || closed > current {
		closed = current
	}
	remaining := current - closed
	if remaining < 0 {
		remaining = 0
	}
	return closed, remaining
}

func (m *Manager) sendExitFillNotification(ctx context.Context, msg exchange.WebhookMessage, payload trader.PositionClosedPayload) {
	if m == nil || m.notifier == nil {
		return
	}
	symbol := strings.ToUpper(strings.TrimSpace(payload.Symbol))
	if symbol == "" {
		symbol = strings.ToUpper(strings.TrimSpace(msg.Pair))
	}
	tradeID, _ := strconv.Atoi(strings.TrimSpace(payload.TradeID))
	stageTitle, stageDetail := decodePlanReason(payload.Reason)
	// fallback removed as planEvents map is gone
	title := fmt.Sprintf("平仓完成：%s", symbol)
	if stageTitle != "" {
		title = fmt.Sprintf("%s %s", title, stageTitle)
	}
	lines := []string{
		fmt.Sprintf("成交价 %.4f", payload.ClosePrice),
	}
	if payload.Amount > 0 {
		lines = append(lines, fmt.Sprintf("本次成交 %.4f", payload.Amount))
	}
	if payload.RemainingAmount > 0 {
		lines = append(lines, fmt.Sprintf("剩余仓位 %.4f", payload.RemainingAmount))
	} else if payload.Amount > 0 {
		lines = append(lines, "剩余仓位 0 · 计划持仓已清空")
	}
	if stageDetail != "" {
		lines = append(lines, "策略阶段 "+stageDetail)
	} else if strings.TrimSpace(payload.Reason) != "" {
		lines = append(lines, "策略阶段 "+strings.TrimSpace(payload.Reason))
	}

	pnlAbs := payload.PnL
	pnlPct := payload.PnLPct
	pctAlreadyPercent := false
	if math.Abs(pnlAbs) < 1e-9 && math.Abs(pnlPct) < 1e-9 {
		entry := m.lookupEntryPrice(ctx, tradeID, symbol)
		side := m.lookupPositionSide(symbol, strings.ToLower(strings.TrimSpace(payload.Side)))
		if entry > 0 && payload.Amount > 0 {
			direction := 1.0
			if side == "short" {
				direction = -1
			}
			pnlAbs = (payload.ClosePrice - entry) * payload.Amount * direction
			pnlPct = (payload.ClosePrice - entry) / entry * 100 * direction
			pctAlreadyPercent = true
		}
	}
	if math.Abs(pnlAbs) >= 1e-9 || math.Abs(pnlPct) >= 1e-9 {
		displayPct := pnlPct
		if !pctAlreadyPercent {
			if math.Abs(displayPct) <= 2 {
				displayPct = displayPct * 100
			}
		}
		lines = append(lines, fmt.Sprintf("盈亏 %s · %s", formatSignedValue(pnlAbs), formatSignedPercent(displayPct)))
	}
	if tradeID > 0 {
		lines = append(lines, fmt.Sprintf("TradeID %d", tradeID))
	}

	msgBody := notifier.StructuredMessage{
		Icon:      "🏁",
		Title:     title,
		Sections:  []notifier.MessageSection{{Title: "执行明细", Lines: lines}},
		Timestamp: time.Now().UTC(),
	}
	if err := m.notifier.SendText(msgBody.RenderMarkdown()); err != nil {
		logger.Warnf("Telegram 推送失败(exit_fill): %v", err)
	}
}

func (m *Manager) lookupEntryPrice(ctx context.Context, tradeID int, symbol string) float64 {
	if ctx == nil {
		ctx = context.Background()
	}
	if tradeID > 0 && m.posRepo != nil {
		if rec, ok, err := m.posRepo.GetPosition(ctx, tradeID); err == nil && ok {
			if rec.Price != nil && *rec.Price > 0 {
				return *rec.Price
			}
		}
	}
	if m.trader != nil {
		snap := m.trader.Snapshot()
		if snap != nil {
			key := freqtradePairToSymbol(symbol)
			if key == "" {
				key = strings.ToUpper(strings.TrimSpace(symbol))
			}
			if pos, ok := snap.Positions[key]; ok && pos != nil && pos.EntryPrice > 0 {
				return pos.EntryPrice
			}
		}
	}
	return 0
}

func (m *Manager) lookupPositionSide(symbol string, fallback string) string {
	if m != nil && m.trader != nil {
		snap := m.trader.Snapshot()
		if snap != nil {
			key := freqtradePairToSymbol(symbol)
			if key == "" {
				key = strings.ToUpper(strings.TrimSpace(symbol))
			}
			if pos, ok := snap.Positions[key]; ok && pos != nil && pos.Side != "" {
				switch strings.ToLower(pos.Side) {
				case "short", "sell":
					return "short"
				default:
					return "long"
				}
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(fallback)) {
	case "short", "sell":
		return "short"
	default:
		return "long"
	}
}
