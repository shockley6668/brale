package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/logger"
	"brale/internal/pkg/trading"
	"brale/internal/strategy/exit"
	"brale/internal/strategy/exit/handlers"
)

const ()

// Recover rebuilds the Trader's state by replaying events from the store.
// It should be called before Start().
// Recover rebuilds the Trader's state by hydrating from DB and then replaying events.
func (t *Trader) Recover() error {
	ctx := context.Background()
	t.state = NewState()
	t.refreshSnapshot(true)

	if err := t.syncExecutorPositions(ctx); err != nil {
		logger.Warnf("Trader: executor position sync failed: %v", err)
	}

	if t.posStore != nil {
		if err := t.hydrateFromDB(ctx); err != nil {
			return err
		}
	}

	t.refreshSnapshot(true)
	logger.Infof("Trader: Recovery complete using live position store")
	return nil
}

// Trader is the core Actor managing state and adapter.
type Trader struct {
	// Dependencies
	executor      exchange.Exchange
	store         EventStore                 // Persist events
	posStore      database.LivePositionStore // Persist state
	registry      *exit.HandlerRegistry      // Exit strategies
	eventRegistry *HandlerRegistry           // Event handlers

	// Channels
	msgCh  chan EventEnvelope
	stopCh chan struct{}
	wg     sync.WaitGroup

	// State (Thread-confined)
	state *State

	stateSnapshot    atomic.Value
	snapshotThrottle time.Duration
	lastSnapshot     time.Time
}

// NewTrader creates a new Trader actor.
// store can be nil for in-memory only (e.g. tests or shadow mode without persistence).
func NewTrader(exec exchange.Exchange, store EventStore, posStore database.LivePositionStore) *Trader {
	reg := exit.NewHandlerRegistry()
	handlers.RegisterCoreHandlers(reg)

	// Create event handler registry
	eventReg := NewHandlerRegistry()
	eventReg.RegisterDefaultHandlers()

	tr := &Trader{
		executor:         exec,
		store:            store,
		posStore:         posStore,
		registry:         reg,
		eventRegistry:    eventReg,
		msgCh:            make(chan EventEnvelope, 100), // Buffered to handle bursts
		stopCh:           make(chan struct{}),
		state:            NewState(),
		snapshotThrottle: 50 * time.Millisecond,
	}
	tr.refreshSnapshot(true)
	return tr
}

// Start launches the actor loop in a background goroutine.
func (t *Trader) Start() {
	t.wg.Add(1)
	go t.runLoop()
}

// Stop signals the actor to stop and waits for it to finish.
func (t *Trader) Stop() {
	close(t.stopCh)
	t.wg.Wait()
	if t.store != nil {
		t.store.Close()
	}
}

// Send sends an event to the actor. Non-blocking unless buffer is full.
func (t *Trader) Send(evt EventEnvelope) error {
	select {
	case t.msgCh <- evt:
		return nil
	case <-t.stopCh:
		return fmt.Errorf("trader is stopped")
	default:
		return fmt.Errorf("trader mailbox is full")
	}
}

// SendSync sends an event and waits for the result (use with caution).
func (t *Trader) SendSync(ctx context.Context, evt EventEnvelope) error {
	if evt.ReplyCh == nil {
		evt.ReplyCh = make(chan error, 1)
	}

	if err := t.Send(evt); err != nil {
		return err
	}

	select {
	case err := <-evt.ReplyCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-t.stopCh:
		return fmt.Errorf("trader stopped during sync call")
	}
}

// Snapshot returns a thread-safe copy of the current state.
// This is wait-free for readers (Manager).
func (t *Trader) Snapshot() *State {
	val := t.stateSnapshot.Load()
	if val == nil {
		return NewState()
	}
	return val.(*State)
}

// refreshSnapshot atomically updates the read-only snapshot.
// Must be called from runLoop (writer).
func (t *Trader) refreshSnapshot(force bool) {
	if !force && t.snapshotThrottle > 0 && !t.lastSnapshot.IsZero() {
		if time.Since(t.lastSnapshot) < t.snapshotThrottle {
			return
		}
	}
	// Deep copy
	newState := NewState()
	for k, v := range t.state.Positions {
		cp := *v
		newState.Positions[k] = &cp
	}
	for id, sym := range t.state.ByTradeID {
		newState.ByTradeID[id] = sym
	}
	for id, plans := range t.state.Plans {
		if len(plans) == 0 {
			continue
		}
		cp := make([]exit.StrategyPlanSnapshot, len(plans))
		copy(cp, plans)
		newState.Plans[id] = cp
	}
	for id, ver := range t.state.PlanVersions {
		newState.PlanVersions[id] = ver
	}
	for sym, tradeID := range t.state.SymbolIndex {
		newState.SymbolIndex[sym] = tradeID
	}
	for tradeID, events := range t.state.TradeEvents {
		if len(events) == 0 {
			continue
		}
		cp := make([]database.TradeOperationRecord, len(events))
		copy(cp, events)
		newState.TradeEvents[tradeID] = cp
	}
	t.stateSnapshot.Store(newState)
	t.lastSnapshot = time.Now()
}

// runLoop is the single-threaded heart of the actor.
func (t *Trader) runLoop() {

	defer t.wg.Done()
	logger.Infof("Trader Actor started")

	for {
		select {
		case evt := <-t.msgCh:
			t.handleEvent(evt)
		case <-t.stopCh:
			logger.Infof("Trader Actor stopping")
			return
		}
	}
}

func (t *Trader) handleEvent(evt EventEnvelope) {
	var err error
	Start := time.Now()

	defer func() {
		// Recovery
		if r := recover(); r != nil {
			logger.Errorf("Trader panic handling event %s: %v", evt.Type, r)
			debug.PrintStack()
			err = fmt.Errorf("panic: %v", r)
		}

		// Reply if requested
		if evt.ReplyCh != nil {
			evt.ReplyCh <- err
			close(evt.ReplyCh)
		}

		// Log slow events
		if dur := time.Since(Start); dur > 100*time.Millisecond {
			logger.Warnf("Slow event %s took %v", evt.Type, dur)
		}
	}()

	// 1. Persist Event (WAL) - skip non-fact events
	if t.store != nil && shouldPersistEvent(evt.Type) {
		if err := t.store.Append(evt); err != nil {
			logger.Errorf("Failed to persist event %s: %v", evt.Type, err)
		}
	}

	// 2. Dispatch to registered handler
	handler, ok := t.eventRegistry.Get(evt.Type)
	if !ok {
		logger.Warnf("No handler registered for event type: %s", evt.Type)
		return
	}

	ctx := NewHandlerContext(t)
	err = handler.Handle(ctx, evt.Payload, evt.ID)

	if err != nil {
		logger.Errorf("Trader failed to handle %s: %v", evt.Type, err)
	}
}

func (t *Trader) hydrateFromDB(ctx context.Context) error {
	logger.Infof("Trader: Hydrating state from DB...")
	active, err := t.posStore.ListActivePositions(ctx, 1000)
	if err != nil {
		return fmt.Errorf("failed to list active positions from DB: %w", err)
	}
	// Always hydrate to ensure we have the correct TradeID mappings (internal DB ID vs Exchange ID)
	// even if we already have positions from the executor sync.
	for _, rec := range active {
		symbol := normalizeSymbol(rec.Symbol)
		if symbol == "" {
			continue
		}

		// Ensure we record the internal TradeID mapping
		if rec.FreqtradeID > 0 {
			tradeID := strconv.Itoa(rec.FreqtradeID)
			t.state.ByTradeID[tradeID] = symbol
			// CRITICAL: Overwrite SymbolIndex with internal ID so we can look up Plans (which are keyed by internal ID)
			t.state.SymbolIndex[symbol] = tradeID
		}

		// If position is not in memory (e.g. not returned by executor sync), we might want to add it?
		// Or trust executor? Standard practice: Trust executor for existence, DB for metadata.
		// If executor missed it (e.g. network issue), maybe add it marked as 'assume open'?
		// For now, let's only backfill if missing, but primarily we care about the ID mapping above.
		if _, exists := t.state.Positions[symbol]; !exists {
			amt := 0.0
			if rec.Amount != nil {
				amt = *rec.Amount
			}
			price := 0.0
			if rec.Price != nil {
				price = *rec.Price
			}
			initAmt := amt
			if rec.InitialAmount != nil && *rec.InitialAmount > 0 {
				initAmt = *rec.InitialAmount
			}
			stake := 0.0
			if rec.StakeAmount != nil {
				stake = *rec.StakeAmount
			}
			lev := 0.0
			if rec.Leverage != nil {
				lev = *rec.Leverage
			}
			openedAt := rec.CreatedAt
			if rec.StartTime != nil && !rec.StartTime.IsZero() {
				openedAt = *rec.StartTime
			}
			pos := &exchange.Position{
				ID:            strconv.Itoa(rec.FreqtradeID),
				Symbol:        symbol,
				Side:          strings.ToLower(strings.TrimSpace(rec.Side)),
				Amount:        amt,
				InitialAmount: initAmt,
				StakeAmount:   stake,
				Leverage:      lev,
				EntryPrice:    price,
				OpenedAt:      openedAt,
				UpdatedAt:     rec.UpdatedAt,
				IsOpen:        true,
				// We don't have ID from DB usually (unless we stored it), relying on FreqtradeID
			}
			t.state.Positions[symbol] = pos
		}
	}

	if err := t.hydratePlansFromDB(ctx); err != nil {
		return err
	}
	logger.Infof("Trader: Hydrated metadata for %d active positions from DB.", len(active))
	return nil
}

func (t *Trader) hydratePlansFromDB(ctx context.Context) error {
	if t.posStore == nil {
		return nil
	}
	for idStr := range t.state.ByTradeID {
		tradeID, err := strconv.Atoi(idStr)
		if err != nil || tradeID <= 0 {
			continue
		}
		recs, err := t.posStore.ListStrategyInstances(ctx, tradeID)
		if err != nil {
			logger.Warnf("Trader: failed to load plans for trade %s: %v", idStr, err)
			continue
		}
		if len(recs) == 0 {
			continue
		}
		snaps := make([]exit.StrategyPlanSnapshot, 0, len(recs))
		for _, r := range recs {
			snaps = append(snaps, exit.StrategyPlanSnapshot{
				PlanID:        r.PlanID,
				PlanComponent: r.PlanComponent,
				PlanVersion:   r.PlanVersion,
				StatusCode:    int(r.Status),
				ParamsJSON:    r.ParamsJSON,
				StateJSON:     r.StateJSON,
				CreatedAt:     r.CreatedAt.Unix(),
				UpdatedAt:     r.UpdatedAt.Unix(),
			})
		}
		t.state.Plans[idStr] = snaps
		t.state.PlanVersions[idStr] = time.Now().UnixNano()
	}
	return nil
}

func (t *Trader) syncExecutorPositions(ctx context.Context) error {
	if t.executor == nil {
		return nil
	}
	positions, err := t.executor.ListOpenPositions(ctx)
	if err != nil {
		return err
	}

	logger.Debugf("Trader: sync found %d open positions from executor", len(positions))
	t.state.Positions = make(map[string]*exchange.Position)
	t.state.SymbolIndex = make(map[string]string)

	for i := range positions {
		pos := &positions[i]
		symbol := normalizeSymbol(pos.Symbol)
		t.state.Positions[symbol] = pos
		if pos.ID != "" {
			t.state.SymbolIndex[symbol] = pos.ID
		}
	}
	return nil
}

func (t *Trader) applyExecutorPosition(ctx context.Context, pos exchange.Position) {
	symbol := normalizeSymbol(pos.Symbol)
	if symbol == "" {
		return
	}
	if t.posStore != nil {
		rec := executorPositionToRecord(pos)
		if err := t.posStore.SavePosition(ctx, rec); err != nil {
			logger.Warnf("Trader: failed to persist executor position %s: %v", symbol, err)
		}
	}
	copyPos := pos
	copyPos.Symbol = symbol
	t.state.Positions[symbol] = &copyPos
	if pos.ID != "" {
		t.state.ByTradeID[pos.ID] = symbol
		t.state.SymbolIndex[symbol] = pos.ID
	}
}

// handleSyncPlans processes plan synchronization.
func (t *Trader) handleSyncPlans(payload []byte) error {
	var p SyncPlansPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal sync plans: %w", err)
	}
	logger.Infof("Trader: Sync Plans for TradeID=%d", p.TradeID)
	// Update State with Plans
	strID := strconv.Itoa(p.TradeID)
	if p.Version > 0 {
		if prev, ok := t.state.PlanVersions[strID]; ok && p.Version <= prev {
			return nil
		}
		t.state.PlanVersions[strID] = p.Version
	}
	cloned := make([]exit.StrategyPlanSnapshot, len(p.Plans))
	copy(cloned, p.Plans)
	t.state.Plans[strID] = cloned
	t.refreshSnapshot(false)
	return nil
}

func (t *Trader) handlePlanStateUpdate(payload []byte) error {
	var p PlanStateUpdatePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal plan state update: %w", err)
	}
	if p.TradeID <= 0 || strings.TrimSpace(p.PlanID) == "" {
		return fmt.Errorf("plan state update missing identifiers")
	}
	tradeKey := strconv.Itoa(p.TradeID)
	snap := exit.StrategyPlanSnapshot{
		PlanID:          p.PlanID,
		PlanComponent:   p.PlanComponent,
		PlanVersion:     p.PlanVersion,
		StatusCode:      p.StatusCode,
		ParamsJSON:      p.ParamsJSON,
		StateJSON:       p.StateJSON,
		DecisionTraceID: p.DecisionTraceID,
		UpdatedAt:       p.UpdatedAt,
	}

	if p.UpdatedAt > 0 {
		snap.UpdatedAt = p.UpdatedAt
	} else {
		snap.UpdatedAt = time.Now().Unix()
	}
	updated := false
	plans := t.state.Plans[tradeKey]
	for i := range plans {
		if plans[i].PlanID == p.PlanID && plans[i].PlanComponent == p.PlanComponent {
			plans[i] = snap
			updated = true
			break
		}
	}
	if !updated {
		plans = append(plans, snap)
	}
	t.state.Plans[tradeKey] = plans
	if p.Version > 0 {
		if prev, ok := t.state.PlanVersions[tradeKey]; !ok || p.Version > prev {
			t.state.PlanVersions[tradeKey] = p.Version
		}
	}
	if t.posStore != nil {
		status := database.StrategyStatus(p.StatusCode)
		if err := t.posStore.UpdateStrategyInstanceState(context.Background(), p.TradeID, p.PlanID, p.PlanComponent, p.StateJSON, status); err != nil {
			return fmt.Errorf("failed to persist plan state trade=%d plan=%s: %w", p.TradeID, p.PlanID, err)
		}
	}
	t.refreshSnapshot(false)
	return nil
}

func planSnapshotsToRecords(tradeID int, snaps []exit.StrategyPlanSnapshot) []database.StrategyInstanceRecord {
	recs := make([]database.StrategyInstanceRecord, 0, len(snaps))
	for _, snap := range snaps {
		rec := database.StrategyInstanceRecord{
			TradeID:         tradeID,
			PlanID:          snap.PlanID,
			PlanComponent:   snap.PlanComponent,
			PlanVersion:     snap.PlanVersion,
			ParamsJSON:      snap.ParamsJSON,
			StateJSON:       snap.StateJSON,
			Status:          database.StrategyStatus(snap.StatusCode),
			DecisionTraceID: snap.DecisionTraceID,
		}
		if snap.CreatedAt > 0 {
			rec.CreatedAt = time.Unix(snap.CreatedAt, 0)
		}
		if snap.UpdatedAt > 0 {
			rec.UpdatedAt = time.Unix(snap.UpdatedAt, 0)
		}
		recs = append(recs, rec)
	}
	return recs
}

func executorPositionToRecord(pos exchange.Position) database.LiveOrderRecord {
	now := time.Now()
	createdAt := pos.OpenedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := pos.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	rec := database.LiveOrderRecord{
		FreqtradeID: parseTradeID(pos.ID),
		Symbol:      normalizeSymbol(pos.Symbol),
		Side:        strings.ToLower(strings.TrimSpace(pos.Side)),
		Status:      database.LiveOrderStatusOpen,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	start := createdAt
	rec.StartTime = &start
	if pos.Amount != 0 {
		amt := pos.Amount
		rec.Amount = &amt
		rec.InitialAmount = &amt
	}
	if pos.StakeAmount != 0 {
		stake := pos.StakeAmount
		rec.StakeAmount = &stake
	}
	if pos.Leverage != 0 {
		lev := pos.Leverage
		rec.Leverage = &lev
	}
	if pos.EntryPrice != 0 {
		price := pos.EntryPrice
		rec.Price = &price
	}
	if pos.CurrentPrice != 0 {
		cp := pos.CurrentPrice
		rec.CurrentPrice = &cp
	}
	if pos.UnrealizedPnL != 0 {
		pnl := pos.UnrealizedPnL
		rec.UnrealizedPnLUSD = &pnl
	}
	return rec
}

// handleOrderResult processes async execution results.
func (t *Trader) handleOrderResult(payload []byte) error {
	var res OrderResultPayload
	if err := json.Unmarshal(payload, &res); err != nil {
		return fmt.Errorf("failed to unmarshal order result: %w", err)
	}

	if res.Error != "" {
		logger.Errorf("Async Execution Failed for %s: %s", res.Symbol, res.Error)
		// TODO: Retry logic or alert?
		return nil
	}

	logger.Infof("Async Execution Success for %s, TradeID: %s", res.Symbol, res.OrderID)

	switch res.Action {
	case OrderActionOpen:
		return t.processOpenSuccess(res)
	case OrderActionClose:
		return t.processCloseSuccess(res)
	default:
		logger.Warnf("OrderResult missing action for %s, inferring by state", res.Symbol)
		if _, exists := t.state.Positions[res.Symbol]; !exists {
			return t.processOpenSuccess(res)
		}
		return t.processCloseSuccess(res)
	}
}

func (t *Trader) processOpenSuccess(res OrderResultPayload) error {
	logger.Infof("Executor reported open success for %s (trade=%s)，等待 freqtrade webhook 对帐", res.Symbol, res.TradeID)
	symbol := normalizeSymbol(res.Symbol)
	tradeID := strings.TrimSpace(res.TradeID)
	if symbol != "" && tradeID != "" {
		t.state.SymbolIndex[symbol] = tradeID
		t.state.ByTradeID[tradeID] = symbol
		t.refreshSnapshot(false)
	}
	return nil
}

func (t *Trader) processCloseSuccess(res OrderResultPayload) error {
	logger.Infof("Executor reported close success for %s，等待 freqtrade webhook 对帐", res.Symbol)
	return nil
}

func shouldPersistEvent(t EventType) bool {
	switch t {
	case EvtPriceUpdate, EvtSyncPlans:
		return false
	default:
		return true
	}
}

// handlePriceUpdate processes market price updates.
func (t *Trader) handlePriceUpdate(payload []byte) error {
	var p PriceUpdatePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("handlePriceUpdate: failed to unmarshal: %v", err)
	}

	// 1. Check if we have an active position for this symbol
	if _, ok := t.state.Positions[p.Symbol]; !ok {
		return nil // No active position, ignore
	}

	tradeIDStr := t.state.TradeIDBySymbol(p.Symbol)
	if tradeIDStr == "" {
		return nil
	}

	// 3. Get Plans
	plans, ok := t.state.Plans[tradeIDStr]
	if !ok || len(plans) == 0 {
		return nil
	}

	// tradeIDInt, _ := strconv.Atoi(tradeIDStr) // Unused now

	// 4. Evaluate Plans - DISABLED to prevent dual execution
	// The PlanScheduler (Agent) is now the sole authority for evaluating plans.
	// The Trader actor retains the plan state for visibility but does not act on it.
	/*
		for _, snap := range plans {
			// ... internal evaluation logic removed ...
		}
	*/

	// We keep the function signature and checks for consistency, but return early.
	return nil
}

// handlePlanEvent processes plan triggers (StopLoss, TakeProfit, etc).
func (t *Trader) handlePlanEvent(payload []byte) error {
	var p PlanEventPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal plan event: %w", err)
	}

	// Assuming plan event implies we need to close the position or adjust it.
	// For "stop_loss", "take_profit", "final_stop_loss", "final_take_profit" -> Close Position.
	// For "adjust" -> maybe ignore or AdjustOrder (not implemented fully).

	symbol := ""

	// 1. Try Lookup by TradeID from Index
	strID := strconv.Itoa(p.TradeID)
	if s, ok := t.state.ByTradeID[strID]; ok {
		symbol = s
	}

	// 2. Fallback to Context if Index missed (e.g. legacy or un-indexed)
	if symbol == "" {
		if val, ok := p.Context["symbol"].(string); ok && val != "" {
			symbol = val
		}
	}

	if symbol == "" {
		return fmt.Errorf("handlePlanEvent: symbol not found for TradeID %d", p.TradeID)
	}

	logger.Infof("Trader: Plan Event %s type=%s", symbol, p.EventType)

	// Execute Close
	// Use ClosePosition which usually closes the entire position.
	// We assume Side is opposite of current position, or Executor handles "Close" without side if smart.
	// If Executor needs Side, we must look it up.
	// Let's attempt blind close or look up state.

	// Look up state for Side
	pos, ok := t.state.Positions[symbol]
	side := ""
	if ok {
		// Reverse side? Or just pass "close" intent.
		// Executor ClosePosition usually takes side of the position to close?
		// Or "sell" if long, "buy" if short.
		if pos.Side == "long" || pos.Side == "buy" {
			side = "sell"
		} else {
			side = "buy"
		}
	} else {
		// If state is empty (restart), we might fail.
		// We should try to use Executor.GetPosition to confirm.
		// For now, log and return.
		logger.Warnf("Trader: Plan Event for %s but no local state.", symbol)
		// Try minimal effort close if API allows
		// return t.executor.ClosePosition(...) with empty side?
	}

	if side == "" {
		return fmt.Errorf("handlePlanEvent: unable to determine side for %s", symbol)
	}

	reason := fmt.Sprintf("plan:%s", p.EventType)
	if comp := extractString(p.Context, "component"); comp != "" {
		reason = fmt.Sprintf("plan:%s:%s", comp, p.EventType)
	}
	tradeIDStr := strconv.Itoa(p.TradeID)
	closeAmount := t.planEventAmount(p, pos)
	t.dispatchClose(symbol, side, closeAmount, newEventID("plan-close"), reason, tradeIDStr, p)
	return nil
}

// handleSignalExit processes an exit signal asynchronously.
func (t *Trader) handleSignalExit(payload []byte, traceID string) error {
	var p SignalExitPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("failed to unmarshal signal exit: %w", err)
	}

	if p.Symbol == "" || p.Side == "" {
		return fmt.Errorf("signal exit missing symbol/side")
	}

	logger.Infof("Trader: Signal Exit %s side=%s ratio=%.2f (async)", p.Symbol, p.Side, p.CloseRatio)

	amount := 0.0
	if p.CloseRatio > 0 {
		pos := t.state.Positions[strings.ToUpper(p.Symbol)]
		amount = t.calcCloseAmount(pos, p.CloseRatio, p.IsInitialRatio)
	}

	tradeID := t.tradeIDForSymbol(p.Symbol)
	t.dispatchClose(p.Symbol, p.Side, amount, traceID, "signal_exit", tradeID, p)
	return nil
}

func (t *Trader) calcCloseAmount(pos *exchange.Position, ratio float64, isInitial bool) float64 {
	if pos == nil {
		return 0
	}
	return trading.CalcCloseAmount(pos.Amount, pos.InitialAmount, ratio, isInitial)
}

func (t *Trader) dispatchClose(symbol, side string, amount float64, traceID, reason, tradeID string, original interface{}) {
	if symbol == "" || side == "" {
		logger.Warnf("dispatchClose skipped: missing symbol/side (symbol=%s side=%s)", symbol, side)
		return
	}
	if traceID == "" {
		traceID = newEventID("close")
	}
	if tradeID == "" {
		tradeID = t.tradeIDForSymbol(symbol)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := t.executor.ClosePosition(ctx, exchange.CloseRequest{
			Symbol: symbol,
			Side:   side,
			Amount: amount,
		})

		res := OrderResultPayload{
			RequestID: traceID,
			Action:    OrderActionClose,
			Reason:    reason,
			TradeID:   tradeID,
			Symbol:    symbol,
			Side:      side,
			FillSize:  amount,
			Original:  original,
			Timestamp: time.Now(),
		}
		if err != nil {
			res.Error = err.Error()
		}

		payloadBytes, _ := json.Marshal(res)
		tradeIDInt := 0
		if id, err := strconv.Atoi(strings.TrimSpace(tradeID)); err == nil {
			tradeIDInt = id
		}
		t.Send(EventEnvelope{
			ID:        newEventID("order-result"),
			Type:      EvtOrderResult,
			Payload:   payloadBytes,
			CreatedAt: time.Now(),
			TradeID:   tradeIDInt,
			Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
		})
	}()
}

func (t *Trader) tradeIDForSymbol(symbol string) string {
	if t.state == nil {
		return ""
	}
	if id := t.state.TradeIDBySymbol(symbol); id != "" {
		return id
	}
	for tradeID, sym := range t.state.ByTradeID {
		if sym == symbol {
			return tradeID
		}
	}
	return ""
}

func newEventID(prefix string) string {
	if prefix == "" {
		prefix = "evt"
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func (t *Trader) planEventAmount(p PlanEventPayload, pos *exchange.Position) float64 {
	if pos == nil || pos.Amount <= 0 {
		return 0
	}
	switch p.EventType {
	case exit.PlanEventTypeTierHit:
		if ratio, ok := extractFloat(p.Context, "ratio"); ok && ratio > 0 {
			if ratio > 1 {
				ratio = 1
			}
			return trading.CalcCloseAmount(pos.Amount, pos.InitialAmount, ratio, true)
		}
		return pos.Amount
	default:
		return pos.Amount
	}
}

func extractFloat(ctx map[string]interface{}, key string) (float64, bool) {
	if ctx == nil {
		return 0, false
	}
	raw, ok := ctx[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func extractString(ctx map[string]interface{}, key string) string {
	if ctx == nil {
		return ""
	}
	if val, ok := ctx[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func parseTradeID(id string) int {
	if id == "" {
		return 0
	}
	val, _ := strconv.Atoi(id)
	return val
}
