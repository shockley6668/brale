package freqtrade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	brcfg "brale/internal/config"
	"brale/internal/decision"
	"brale/internal/executor/freqtrade/storage"
	"brale/internal/gateway/database"
	"brale/internal/logger"
	"brale/internal/market"
)

// Logger abstracts decision log writes (live_decision_logs & live_orders).
type Logger interface {
	Insert(ctx context.Context, rec database.DecisionLogRecord) (int64, error)
}

// TextNotifier 描述最小化的文本推送接口（用于 Telegram 等）。
type TextNotifier interface {
	SendText(text string) error
}

// APIPosition 用于 /api/live/freqtrade/positions 返回的数据结构。
type APIPosition struct {
	TradeID    int     `json:"trade_id"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	EntryPrice float64 `json:"entry_price"`
	Amount     float64 `json:"amount"`
	Stake      float64 `json:"stake"`
	Leverage   float64 `json:"leverage"`
	OpenedAt   int64   `json:"opened_at"`
	HoldingMs  int64   `json:"holding_ms"`
}

// Manager 提供 freqtrade 执行、日志与持仓同步能力。
type Manager struct {
	client           *Client
	cfg              brcfg.FreqtradeConfig
	logger           Logger
	orderRec         market.Recorder
	traceByKey       map[string]int
	traceByID        map[int]string
	positions        map[int]Position
	mu               sync.Mutex
	horizonName      string
	stopLossClient   *http.Client
	takeProfitClient *http.Client
	riskStore        *storage.Store
	riskStorePath    string
	notifier         TextNotifier
}

// Position 缓存 freqtrade 持仓信息。
type Position struct {
	TradeID    int
	Symbol     string
	Side       string
	Amount     float64
	Stake      float64
	Leverage   float64
	EntryPrice float64
	OpenedAt   time.Time
}

// NewManager 创建 freqtrade 执行管理器。
func NewManager(client *Client, cfg brcfg.FreqtradeConfig, horizon string, logStore Logger, orderRec market.Recorder, notifier TextNotifier) *Manager {
	var slClient *http.Client
	if strings.TrimSpace(cfg.StopLossWebhookURL) != "" {
		timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		slClient = &http.Client{Timeout: timeout}
	}
	var tpClient *http.Client
	if strings.TrimSpace(cfg.TakeProfitWebhookURL) != "" {
		timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		tpClient = &http.Client{Timeout: timeout}
	}
	var risk *storage.Store
	if path := strings.TrimSpace(cfg.RiskStorePath); path != "" {
		store, err := storage.Open(path)
		if err != nil {
			logger.Warnf("freqtrade risk store 打开失败: %v", err)
		} else {
			risk = store
		}
	}
	return &Manager{
		client:           client,
		cfg:              cfg,
		logger:           logStore,
		orderRec:         orderRec,
		traceByKey:       make(map[string]int),
		traceByID:        make(map[int]string),
		positions:        make(map[int]Position),
		horizonName:      horizon,
		stopLossClient:   slClient,
		takeProfitClient: tpClient,
		riskStore:        risk,
		riskStorePath:    strings.TrimSpace(cfg.RiskStorePath),
		notifier:         notifier,
	}
}

// DecisionInput 用于执行器的输入。
type DecisionInput struct {
	TraceID  string
	Decision decision.Decision
}

// Execute 根据决策调用 freqtrade ForceEnter/ForceExit。
func (m *Manager) Execute(ctx context.Context, input DecisionInput) error {
	if m.client == nil {
		return fmt.Errorf("freqtrade client not initialized")
	}
	d := input.Decision
	switch d.Action {
	case "open_long", "open_short":
		return m.open(ctx, input.TraceID, d)
	case "close_long", "close_short":
		return m.close(ctx, input.TraceID, d)
	case "adjust_stop_loss":
		return m.adjustStopLoss(ctx, input.TraceID, d)
	case "adjust_take_profit":
		return m.adjustTakeProfit(ctx, input.TraceID, d)
	default:
		return nil
	}
}

type stopLossWebhookPayload struct {
	TradeID    int     `json:"trade_id"`
	Pair       string  `json:"pair"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	StopLoss   float64 `json:"stop_loss"`
	EntryPrice float64 `json:"entry_price,omitempty"`
	Stake      float64 `json:"stake_amount,omitempty"`
	Amount     float64 `json:"amount,omitempty"`
	Leverage   float64 `json:"leverage,omitempty"`
	TraceID    string  `json:"trace_id"`
	Horizon    string  `json:"horizon"`
	Reason     string  `json:"reason,omitempty"`
	Timestamp  int64   `json:"timestamp"`
}

type takeProfitWebhookPayload struct {
	TradeID    int     `json:"trade_id"`
	Pair       string  `json:"pair"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	TakeProfit float64 `json:"take_profit"`
	EntryPrice float64 `json:"entry_price,omitempty"`
	Stake      float64 `json:"stake_amount,omitempty"`
	Amount     float64 `json:"amount,omitempty"`
	Leverage   float64 `json:"leverage,omitempty"`
	TraceID    string  `json:"trace_id"`
	Horizon    string  `json:"horizon"`
	Reason     string  `json:"reason,omitempty"`
	Timestamp  int64   `json:"timestamp"`
}

func (m *Manager) open(ctx context.Context, traceID string, d decision.Decision) error {
	pair := formatFreqtradePair(d.Symbol)
	if pair == "" {
		return fmt.Errorf("unable to convert symbol %s to freqtrade pair", d.Symbol)
	}
	side := deriveSide(d.Action)
	if side == "" {
		return fmt.Errorf("unsupported action: %s", d.Action)
	}
	stake := d.PositionSizeUSD
	if stake <= 0 {
		if m.cfg.DefaultStakeUSD > 0 {
			stake = m.cfg.DefaultStakeUSD
		}
	}
	if stake <= 0 {
		return fmt.Errorf("stake amount missing")
	}
	lev := float64(d.Leverage)
	if lev <= 0 {
		if m.cfg.DefaultLeverage > 0 {
			lev = float64(m.cfg.DefaultLeverage)
		}
	}

	payload := ForceEnterPayload{
		Pair:        pair,
		Side:        side,
		StakeAmount: stake,
		EntryTag:    m.entryTag(),
	}
	if lev > 0 {
		payload.Leverage = lev
	}

	recData := map[string]any{"payload": payload, "status": "forceenter"}
	resp, err := m.client.ForceEnter(ctx, payload)
	if err != nil {
		m.logExecutor(ctx, traceID, d, 0, "forceenter_error", recData, err)
		m.notify("Freqtrade 建仓请求失败 ❌",
			fmt.Sprintf("标的: %s (%s)", strings.ToUpper(d.Symbol), pair),
			fmt.Sprintf("方向: %s  杠杆: x%.2f", strings.ToUpper(side), lev),
			fmt.Sprintf("仓位: %.2f USDT", stake),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
			fmt.Sprintf("错误: %v", err),
		)
		return err
	}
	m.storeTrade(d.Symbol, side, resp.TradeID)
	m.storeTrace(resp.TradeID, traceID)
	m.logExecutor(ctx, traceID, d, resp.TradeID, "forceenter_success", recData, nil)
	initialRisk := storage.RiskRecord{
		TradeID:    resp.TradeID,
		Symbol:     strings.ToUpper(strings.TrimSpace(d.Symbol)),
		Pair:       pair,
		Side:       side,
		Leverage:   lev,
		StopLoss:   d.StopLoss,
		TakeProfit: d.TakeProfit,
		Reason:     strings.TrimSpace(d.Reasoning),
		Source:     "open",
		Status:     "forceenter_success",
		UpdatedAt:  time.Now(),
	}
	m.recordRisk(initialRisk)
	lines := []string{
		fmt.Sprintf("交易ID: %d", resp.TradeID),
		fmt.Sprintf("标的: %s (%s)", strings.ToUpper(d.Symbol), pair),
		fmt.Sprintf("方向: %s  杠杆: x%.2f", strings.ToUpper(side), lev),
		fmt.Sprintf("仓位: %.2f USDT", stake),
		fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
	}
	if reason := shortReason(d.Reasoning); reason != "" {
		lines = append(lines, "理由: "+reason)
	}
	m.notify("Freqtrade 建仓请求已发送 ✅", lines...)
	// 同步初始止盈/止损到 risk server
	go func(sym string, tp, sl float64, trace string) {
		dec := decision.Decision{
			Symbol:     sym,
			TakeProfit: tp,
			StopLoss:   sl,
			Reasoning:  strings.TrimSpace(d.Reasoning),
		}
		if tp > 0 {
			if err := m.adjustTakeProfit(context.Background(), trace, dec); err != nil {
				logger.Warnf("初始同步止盈失败: %v", err)
			}
		}
		if sl > 0 {
			if err := m.adjustStopLoss(context.Background(), trace, dec); err != nil {
				logger.Warnf("初始同步止损失败: %v", err)
			}
		}
	}(strings.ToUpper(strings.TrimSpace(d.Symbol)), d.TakeProfit, d.StopLoss, m.ensureTrace(traceID))
	return nil
}

func (m *Manager) close(ctx context.Context, traceID string, d decision.Decision) error {
	side := deriveSide(d.Action)
	if side == "" {
		return fmt.Errorf("unsupported action: %s", d.Action)
	}
	tradeID, ok := m.lookupTrade(d.Symbol, side)
	if !ok {
		return fmt.Errorf("freqtrade trade_id not found for %s %s", d.Symbol, side)
	}
	payload := ForceExitPayload{TradeID: fmt.Sprintf("%d", tradeID)}
	if ratio := clampCloseRatio(d.CloseRatio); ratio > 0 && ratio < 1 {
		if amt := m.lookupAmount(tradeID); amt > 0 {
			payload.Amount = amt * ratio
		}
	}
	recData := map[string]any{"payload": payload, "status": "forceexit", "close_ratio": d.CloseRatio}
	if err := m.client.ForceExit(ctx, payload); err != nil {
		m.logExecutor(ctx, traceID, d, tradeID, "forceexit_error", recData, err)
		m.notify("Freqtrade 平仓指令失败 ❌",
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", strings.ToUpper(d.Symbol)),
			fmt.Sprintf("方向: %s", strings.ToUpper(side)),
			fmt.Sprintf("平仓比例: %.2f", clampCloseRatio(d.CloseRatio)),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
			fmt.Sprintf("错误: %v", err),
		)
		return err
	}
	m.deleteTrade(d.Symbol, side)
	m.logExecutor(ctx, traceID, d, tradeID, "forceexit_success", recData, nil)
	m.notify("Freqtrade 平仓指令已执行 ✅",
		fmt.Sprintf("交易ID: %d", tradeID),
		fmt.Sprintf("标的: %s", strings.ToUpper(d.Symbol)),
		fmt.Sprintf("方向: %s", strings.ToUpper(side)),
		fmt.Sprintf("平仓比例: %.2f", clampCloseRatio(d.CloseRatio)),
		fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
	)
	return nil
}

func (m *Manager) adjustStopLoss(ctx context.Context, traceID string, d decision.Decision) error {
	if strings.TrimSpace(m.cfg.StopLossWebhookURL) == "" {
		logger.Warnf("freqtrade stop loss webhook 未配置，忽略 adjust_stop_loss")
		return nil
	}
	if d.StopLoss <= 0 {
		return fmt.Errorf("invalid stop_loss for adjust_stop_loss")
	}
	tradeID, side, pos, ok := m.findActiveTrade(d.Symbol)
	if !ok {
		return fmt.Errorf("freqtrade 没有 %s 的持仓，无法调整止损", d.Symbol)
	}
	pair := formatFreqtradePair(d.Symbol)
	if pair == "" {
		return fmt.Errorf("无法转换 symbol %s 为 freqtrade pair", d.Symbol)
	}
	existing, _ := m.loadRiskRecord(ctx, tradeID)
	prevStop := existing.StopLoss
	payload := stopLossWebhookPayload{
		TradeID:    tradeID,
		Pair:       pair,
		Symbol:     strings.ToUpper(strings.TrimSpace(d.Symbol)),
		Side:       side,
		StopLoss:   d.StopLoss,
		EntryPrice: pos.EntryPrice,
		Stake:      pos.Stake,
		Amount:     pos.Amount,
		Leverage:   pos.Leverage,
		TraceID:    m.ensureTrace(traceID),
		Horizon:    m.horizonName,
		Reason:     strings.TrimSpace(d.Reasoning),
		Timestamp:  time.Now().UnixMilli(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := m.stopLossClient
	if client == nil {
		timeout := time.Duration(m.cfg.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(m.cfg.StopLossWebhookURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret := strings.TrimSpace(m.cfg.StopLossSecret); secret != "" {
		req.Header.Set("X-Webhook-Secret", secret)
	}
	resp, err := client.Do(req)
	status := "stoploss_webhook_success"
	if err == nil && resp != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode/100 != 2 {
			err = fmt.Errorf("stop loss webhook status=%d", resp.StatusCode)
		}
	} else if resp == nil {
		err = fmt.Errorf("stop loss webhook 未返回响应")
	}
	if err != nil {
		status = "stoploss_webhook_error"
		logger.Warnf("freqtrade stop-loss webhook 失败: %v", err)
		lines := []string{
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", strings.ToUpper(d.Symbol)),
			fmt.Sprintf("方向: %s", strings.ToUpper(side)),
			fmt.Sprintf("原止损: %s  新止损: %.4f", formatPrice(prevStop), d.StopLoss),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
			fmt.Sprintf("错误: %v", err),
		}
		if reason := shortReason(d.Reasoning); reason != "" {
			lines = append(lines, "理由: "+reason)
		}
		m.notify("止损调整失败 ❌", lines...)
	} else {
		logger.Infof("freqtrade stop-loss webhook 已发送 trade_id=%d %s stop=%.4f", tradeID, d.Symbol, d.StopLoss)
		lines := []string{
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", strings.ToUpper(d.Symbol)),
			fmt.Sprintf("方向: %s", strings.ToUpper(side)),
			fmt.Sprintf("原止损: %s", formatPrice(prevStop)),
			fmt.Sprintf("新止损: %.4f", d.StopLoss),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
		}
		if reason := shortReason(d.Reasoning); reason != "" {
			lines = append(lines, "理由: "+reason)
		}
		m.notify("止损已更新 ✅", lines...)
	}
	recData := map[string]any{
		"payload": payload,
		"url":     strings.TrimSpace(m.cfg.StopLossWebhookURL),
	}
	m.logExecutor(ctx, traceID, d, tradeID, status, recData, err)
	if err == nil {
		m.recordRisk(storage.RiskRecord{
			TradeID:    tradeID,
			Symbol:     strings.ToUpper(strings.TrimSpace(d.Symbol)),
			Pair:       pair,
			Side:       side,
			EntryPrice: pos.EntryPrice,
			Stake:      pos.Stake,
			Amount:     pos.Amount,
			Leverage:   pos.Leverage,
			StopLoss:   d.StopLoss,
			TakeProfit: existing.TakeProfit,
			Reason:     strings.TrimSpace(d.Reasoning),
			Source:     "adjust_stop_loss",
			Status:     status,
			UpdatedAt:  time.Now(),
		})
	}
	return err
}

func (m *Manager) adjustTakeProfit(ctx context.Context, traceID string, d decision.Decision) error {
	if strings.TrimSpace(m.cfg.TakeProfitWebhookURL) == "" {
		logger.Warnf("freqtrade take profit webhook 未配置，忽略 adjust_take_profit")
		return nil
	}
	if d.TakeProfit <= 0 {
		return fmt.Errorf("invalid take_profit for adjust_take_profit")
	}
	tradeID, side, pos, ok := m.findActiveTrade(d.Symbol)
	if !ok {
		return fmt.Errorf("freqtrade 没有 %s 的持仓，无法调整止盈", d.Symbol)
	}
	pair := formatFreqtradePair(d.Symbol)
	if pair == "" {
		return fmt.Errorf("无法转换 symbol %s 为 freqtrade pair", d.Symbol)
	}
	existing, _ := m.loadRiskRecord(ctx, tradeID)
	prevTP := existing.TakeProfit
	payload := takeProfitWebhookPayload{
		TradeID:    tradeID,
		Pair:       pair,
		Symbol:     strings.ToUpper(strings.TrimSpace(d.Symbol)),
		Side:       side,
		TakeProfit: d.TakeProfit,
		EntryPrice: pos.EntryPrice,
		Stake:      pos.Stake,
		Amount:     pos.Amount,
		Leverage:   pos.Leverage,
		TraceID:    m.ensureTrace(traceID),
		Horizon:    m.horizonName,
		Reason:     strings.TrimSpace(d.Reasoning),
		Timestamp:  time.Now().UnixMilli(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := m.takeProfitClient
	if client == nil {
		timeout := time.Duration(m.cfg.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(m.cfg.TakeProfitWebhookURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret := strings.TrimSpace(m.cfg.TakeProfitSecret); secret != "" {
		req.Header.Set("X-Webhook-Secret", secret)
	}
	resp, err := client.Do(req)
	status := "takeprofit_webhook_success"
	if err == nil && resp != nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode/100 != 2 {
			err = fmt.Errorf("take profit webhook status=%d", resp.StatusCode)
		}
	} else if resp == nil {
		err = fmt.Errorf("take profit webhook 未返回响应")
	}
	if err != nil {
		status = "takeprofit_webhook_error"
		logger.Warnf("freqtrade take-profit webhook 失败: %v", err)
		lines := []string{
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", strings.ToUpper(d.Symbol)),
			fmt.Sprintf("方向: %s", strings.ToUpper(side)),
			fmt.Sprintf("原止盈: %s  新止盈: %.4f", formatPrice(prevTP), d.TakeProfit),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
			fmt.Sprintf("错误: %v", err),
		}
		if reason := shortReason(d.Reasoning); reason != "" {
			lines = append(lines, "理由: "+reason)
		}
		m.notify("止盈调整失败 ❌", lines...)
	} else {
		logger.Infof("freqtrade take-profit webhook 已发送 trade_id=%d %s take=%.4f", tradeID, d.Symbol, d.TakeProfit)
		lines := []string{
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", strings.ToUpper(d.Symbol)),
			fmt.Sprintf("方向: %s", strings.ToUpper(side)),
			fmt.Sprintf("原止盈: %s", formatPrice(prevTP)),
			fmt.Sprintf("新止盈: %.4f", d.TakeProfit),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
		}
		if reason := shortReason(d.Reasoning); reason != "" {
			lines = append(lines, "理由: "+reason)
		}
		m.notify("止盈已更新 ✅", lines...)
	}
	recData := map[string]any{
		"payload": payload,
		"url":     strings.TrimSpace(m.cfg.TakeProfitWebhookURL),
	}
	m.logExecutor(ctx, traceID, d, tradeID, status, recData, err)
	if err == nil {
		m.recordRisk(storage.RiskRecord{
			TradeID:    tradeID,
			Symbol:     strings.ToUpper(strings.TrimSpace(d.Symbol)),
			Pair:       pair,
			Side:       side,
			EntryPrice: pos.EntryPrice,
			Stake:      pos.Stake,
			Amount:     pos.Amount,
			Leverage:   pos.Leverage,
			StopLoss:   existing.StopLoss,
			TakeProfit: d.TakeProfit,
			Reason:     strings.TrimSpace(d.Reasoning),
			Source:     "adjust_take_profit",
			Status:     status,
			UpdatedAt:  time.Now(),
		})
	}
	return err
}

// HandleWebhook 由 HTTP 路由调用，负责更新持仓与日志。
func (m *Manager) HandleWebhook(ctx context.Context, msg WebhookMessage) {
	typ := strings.ToLower(strings.TrimSpace(msg.Type))
	switch typ {
	case "entry", "entry_fill":
		m.handleEntry(ctx, msg, typ == "entry_fill")
	case "entry_cancel":
		m.handleEntryCancel(ctx, msg)
	case "exit", "exit_fill":
		m.handleExit(ctx, msg, typ)
	case "exit_cancel":
		m.handleExitCancel(ctx, msg)
	default:
		logger.Debugf("freqtrade manager: ignore webhook type %s", msg.Type)
	}
}

// SyncOpenPositions loads existing open trades from freqtrade to seed the cache.
func (m *Manager) SyncOpenPositions(ctx context.Context) (int, error) {
	if m == nil || m.client == nil {
		return 0, fmt.Errorf("freqtrade client 未初始化")
	}
	trades, err := m.client.ListTrades(ctx)
	if err != nil {
		return 0, err
	}
	type cached struct {
		id     int
		symbol string
		side   string
		pos    Position
	}
	candidates := make([]cached, 0, len(trades))
	for _, tr := range trades {
		if !tr.IsOpen {
			continue
		}
		symbol := freqtradePairToSymbol(tr.Pair)
		side := strings.ToLower(strings.TrimSpace(tr.Side))
		if side == "" {
			if tr.IsShort {
				side = "short"
			} else {
				side = "long"
			}
		}
		if symbol == "" || side == "" {
			continue
		}
		pos := Position{
			TradeID:    tr.ID,
			Symbol:     symbol,
			Side:       side,
			Amount:     tr.Amount,
			Stake:      tr.StakeAmount,
			Leverage:   tr.Leverage,
			EntryPrice: tr.OpenRate,
			OpenedAt:   parseFreqtradeTime(tr.OpenDate),
		}
		candidates = append(candidates, cached{
			id:     tr.ID,
			symbol: symbol,
			side:   side,
			pos:    pos,
		})
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	m.mu.Lock()
	for _, c := range candidates {
		m.positions[c.id] = c.pos
	}
	m.mu.Unlock()
	for _, c := range candidates {
		m.storeTrade(c.symbol, c.side, c.id)
	}
	return len(candidates), nil
}

// Positions 返回当前 freqtrade 持仓快照。
func (m *Manager) Positions() []decision.PositionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.positions) == 0 {
		return nil
	}
	now := time.Now()
	out := make([]decision.PositionSnapshot, 0, len(m.positions))
	for _, pos := range m.positions {
		holding := int64(0)
		if !pos.OpenedAt.IsZero() {
			holding = now.Sub(pos.OpenedAt).Milliseconds()
		}
		out = append(out, decision.PositionSnapshot{
			Symbol:     strings.ToUpper(pos.Symbol),
			Side:       strings.ToUpper(pos.Side),
			EntryPrice: pos.EntryPrice,
			Quantity:   pos.Amount,
			HoldingMs:  holding,
		})
	}
	return out
}

func (m *Manager) entryTag() string {
	tag := fmt.Sprintf("brale_%s", strings.ToLower(strings.TrimSpace(m.horizonName)))
	if tag == "" || tag == "brale_" {
		return "brale"
	}
	return tag
}

func (m *Manager) findActiveTrade(symbol string) (int, string, Position, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return 0, "", Position{}, false
	}
	if id, ok := m.lookupTrade(symbol, "long"); ok {
		if pos, ok := m.positionByID(id); ok {
			return id, "long", pos, true
		}
		return id, "long", Position{}, true
	}
	if id, ok := m.lookupTrade(symbol, "short"); ok {
		if pos, ok := m.positionByID(id); ok {
			return id, "short", pos, true
		}
		return id, "short", Position{}, true
	}
	return 0, "", Position{}, false
}

func (m *Manager) handleEntry(ctx context.Context, msg WebhookMessage, filled bool) {
	tradeID := int(msg.TradeID)
	symbol := freqtradePairToSymbol(msg.Pair)
	side := strings.ToLower(strings.TrimSpace(msg.Direction))
	price := firstNonZero(float64(msg.OpenRate), float64(msg.OrderRate))
	amount := float64(msg.Amount)
	stake := float64(msg.StakeAmount)
	openTime := parseFreqtradeTime(msg.OpenDate)
	traceID := m.lookupTrace(tradeID)
	if traceID == "" {
		traceID = m.ensureTrace(fmt.Sprintf("%d", tradeID))
	}
	pos := Position{
		TradeID:    tradeID,
		Symbol:     symbol,
		Side:       side,
		Amount:     amount,
		Stake:      stake,
		Leverage:   float64(msg.Leverage),
		EntryPrice: price,
		OpenedAt:   openTime,
	}
	if !filled && amount <= 0 {
		m.storeTrade(symbol, side, tradeID)
		logger.Infof("freqtrade manager: entry submitted trade_id=%d %s %s price=%.4f", tradeID, symbol, side, price)
		m.notify("Freqtrade 建仓下单 ⌛",
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s (%s)", strings.ToUpper(symbol), msg.Pair),
			fmt.Sprintf("方向: %s  杠杆: x%.2f", strings.ToUpper(side), float64(msg.Leverage)),
			fmt.Sprintf("委托: %.2f USDT 价格: %s", stake, formatPrice(price)),
			fmt.Sprintf("Trace: %s", traceID),
		)
		return
	}
	m.mu.Lock()
	m.positions[tradeID] = pos
	m.mu.Unlock()
	m.storeTrade(symbol, side, tradeID)
	logger.Infof("freqtrade manager: entry filled trade_id=%d %s %s price=%.4f qty=%.4f", tradeID, symbol, side, price, amount)
	m.logWebhook(ctx, traceID, tradeID, symbol, "entry_fill", msg)
	m.recordOrder(ctx, msg, freqtradeAction(side, false), price, openTime)
	m.notify("Freqtrade 建仓成交 ✅",
		fmt.Sprintf("交易ID: %d", tradeID),
		fmt.Sprintf("标的: %s (%s)", strings.ToUpper(symbol), msg.Pair),
		fmt.Sprintf("方向: %s  杠杆: x%.2f", strings.ToUpper(side), pos.Leverage),
		fmt.Sprintf("数量: %s  仓位: %.2f USDT", formatQty(amount), stake),
		fmt.Sprintf("成交价: %s", formatPrice(price)),
		fmt.Sprintf("开仓时间: %s", formatTime(openTime)),
		fmt.Sprintf("Trace: %s", traceID),
	)
	m.syncInitialRisk(ctx, traceID, tradeID, symbol)
}

func (m *Manager) handleEntryCancel(ctx context.Context, msg WebhookMessage) {
	tradeID := int(msg.TradeID)
	symbol := freqtradePairToSymbol(msg.Pair)
	side := strings.ToLower(strings.TrimSpace(msg.Direction))
	m.deleteTrade(symbol, side)
	m.mu.Lock()
	delete(m.positions, tradeID)
	m.mu.Unlock()
	traceID := m.lookupTrace(tradeID)
	m.deleteTrace(tradeID)
	logger.Infof("freqtrade manager: entry cancel trade_id=%d %s %s reason=%s", tradeID, symbol, side, msg.Reason)
	m.logWebhook(ctx, traceID, tradeID, symbol, "entry_cancel", msg)
	m.notify("Freqtrade 建仓被取消 ⚠️",
		fmt.Sprintf("交易ID: %d", tradeID),
		fmt.Sprintf("标的: %s (%s)", strings.ToUpper(symbol), msg.Pair),
		fmt.Sprintf("方向: %s", strings.ToUpper(side)),
		fmt.Sprintf("原因: %s", strings.TrimSpace(msg.Reason)),
		fmt.Sprintf("Trace: %s", traceID),
	)
}

func (m *Manager) handleExit(ctx context.Context, msg WebhookMessage, event string) {
	tradeID := int(msg.TradeID)
	m.mu.Lock()
	pos, ok := m.positions[tradeID]
	if ok {
		delete(m.positions, tradeID)
	}
	m.mu.Unlock()
	symbol := pos.Symbol
	if symbol == "" {
		symbol = freqtradePairToSymbol(msg.Pair)
	}
	side := pos.Side
	if side == "" {
		side = strings.ToLower(strings.TrimSpace(msg.Direction))
	}
	m.deleteTrade(symbol, side)
	closePrice := float64(msg.CloseRate)
	traceID := m.lookupTrace(tradeID)
	m.deleteTrace(tradeID)
	logger.Infof("freqtrade manager: exit trade_id=%d %s %s price=%.4f reason=%s", tradeID, symbol, side, closePrice, msg.ExitReason)
	m.logWebhook(ctx, traceID, tradeID, symbol, event, msg)
	m.recordOrder(ctx, msg, freqtradeAction(side, true), closePrice, parseFreqtradeTime(msg.CloseDate))
	title := "Freqtrade 平仓完成 ✅"
	if event != "exit_fill" {
		title = "Freqtrade 退出事件"
	}
	duration := "-"
	if !pos.OpenedAt.IsZero() {
		duration = time.Since(pos.OpenedAt).Truncate(time.Second).String()
	}
	lines := []string{
		fmt.Sprintf("交易ID: %d", tradeID),
		fmt.Sprintf("标的: %s (%s)", strings.ToUpper(symbol), msg.Pair),
		fmt.Sprintf("方向: %s", strings.ToUpper(side)),
		fmt.Sprintf("平仓价: %s", formatPrice(closePrice)),
		fmt.Sprintf("理由: %s", strings.TrimSpace(msg.ExitReason)),
		fmt.Sprintf("持仓时长: %s", duration),
		fmt.Sprintf("收益率: %.4f", parseProfitRatio(msg.ProfitRatio)),
		fmt.Sprintf("Trace: %s", traceID),
	}
	m.notify(title, lines...)
}

func (m *Manager) handleExitCancel(ctx context.Context, msg WebhookMessage) {
	tradeID := int(msg.TradeID)
	traceID := m.lookupTrace(tradeID)
	logger.Infof("freqtrade manager: exit cancel trade_id=%d", tradeID)
	m.logWebhook(ctx, traceID, tradeID, freqtradePairToSymbol(msg.Pair), "exit_cancel", msg)
	m.notify("Freqtrade 平仓被取消 ⚠️",
		fmt.Sprintf("交易ID: %d", tradeID),
		fmt.Sprintf("标的: %s", freqtradePairToSymbol(msg.Pair)),
		fmt.Sprintf("原因: %s", strings.TrimSpace(msg.Reason)),
		fmt.Sprintf("Trace: %s", traceID),
	)
}

func (m *Manager) logExecutor(ctx context.Context, traceID string, d decision.Decision, tradeID int, status string, extra map[string]any, execErr error) {
	if m.logger == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sym := strings.ToUpper(strings.TrimSpace(d.Symbol))
	rec := database.DecisionLogRecord{
		TraceID:    m.ensureTrace(traceID),
		Timestamp:  time.Now().UnixMilli(),
		Horizon:    m.horizonName,
		ProviderID: "freqtrade",
		Stage:      "executor",
		Symbols:    []string{sym},
		Decisions:  []decision.Decision{d},
		Note:       status,
	}
	if extra == nil {
		extra = make(map[string]any)
	}
	extra["status"] = status
	extra["trade_id"] = tradeID
	extra["symbol"] = sym
	extra["action"] = d.Action
	rec.RawOutput = fmt.Sprintf("freqtrade %s %s %s", status, sym, d.Action)
	if execErr != nil {
		rec.Error = execErr.Error()
		extra["error"] = execErr.Error()
	}
	if data, err := json.Marshal(extra); err == nil {
		rec.RawJSON = string(data)
	}
	if _, err := m.logger.Insert(ctx, rec); err != nil {
		logger.Warnf("freqtrade executor log failed: %v", err)
	}
}

func (m *Manager) logWebhook(ctx context.Context, traceID string, tradeID int, symbol, event string, msg WebhookMessage) {
	if m.logger == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rec := database.DecisionLogRecord{
		TraceID:    m.ensureTrace(traceID),
		Timestamp:  time.Now().UnixMilli(),
		Horizon:    m.horizonName,
		ProviderID: "freqtrade",
		Stage:      "freqtrade",
		Symbols:    []string{strings.ToUpper(symbol)},
		Note:       strings.ToLower(event),
	}
	rec.RawOutput = fmt.Sprintf("%s trade_id=%d symbol=%s amount=%.4f price=%.4f reason=%s",
		strings.ToUpper(event), tradeID, symbol, float64(msg.Amount), firstNonZero(float64(msg.CloseRate), float64(msg.OpenRate)), strings.TrimSpace(msg.Reason))
	if data, err := json.Marshal(msg); err == nil {
		rec.RawJSON = string(data)
	}
	if _, err := m.logger.Insert(ctx, rec); err != nil {
		logger.Warnf("freqtrade webhook log failed: %v", err)
	}
}

func (m *Manager) PositionsForAPI(symbol string, limit int) []APIPosition {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.positions) == 0 {
		return nil
	}
	now := time.Now()
	list := make([]APIPosition, 0, len(m.positions))
	for _, pos := range m.positions {
		if symbol != "" && !strings.EqualFold(pos.Symbol, symbol) {
			continue
		}
		opened := int64(0)
		holding := int64(0)
		if !pos.OpenedAt.IsZero() {
			opened = pos.OpenedAt.UnixMilli()
			holding = now.Sub(pos.OpenedAt).Milliseconds()
		}
		list = append(list, APIPosition{
			TradeID:    pos.TradeID,
			Symbol:     strings.ToUpper(pos.Symbol),
			Side:       strings.ToUpper(pos.Side),
			EntryPrice: pos.EntryPrice,
			Amount:     pos.Amount,
			Stake:      pos.Stake,
			Leverage:   pos.Leverage,
			OpenedAt:   opened,
			HoldingMs:  holding,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].OpenedAt > list[j].OpenedAt
	})
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

func (m *Manager) storeTrade(symbol, side string, tradeID int) {
	if tradeID == 0 {
		return
	}
	key := freqtradeKey(symbol, side)
	m.mu.Lock()
	m.traceByKey[key] = tradeID
	m.mu.Unlock()
}

func (m *Manager) lookupTrade(symbol, side string) (int, bool) {
	key := freqtradeKey(symbol, side)
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.traceByKey[key]
	return id, ok
}

func (m *Manager) deleteTrade(symbol, side string) {
	key := freqtradeKey(symbol, side)
	m.mu.Lock()
	delete(m.traceByKey, key)
	m.mu.Unlock()
}

func (m *Manager) storeTrace(tradeID int, traceID string) {
	if tradeID <= 0 || traceID == "" {
		return
	}
	m.mu.Lock()
	m.traceByID[tradeID] = traceID
	m.mu.Unlock()
}

func (m *Manager) lookupTrace(tradeID int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.traceByID[tradeID]
}

func (m *Manager) ensureTrace(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	return fmt.Sprintf("freqtrade-%d", time.Now().UnixNano())
}

func (m *Manager) deleteTrace(tradeID int) {
	m.mu.Lock()
	delete(m.traceByID, tradeID)
	m.mu.Unlock()
}

func (m *Manager) positionByID(tradeID int) (Position, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pos, ok := m.positions[tradeID]
	return pos, ok
}

func (m *Manager) ensureRiskStore() *storage.Store {
	m.mu.Lock()
	store := m.riskStore
	path := m.riskStorePath
	m.mu.Unlock()
	if store != nil || strings.TrimSpace(path) == "" {
		return store
	}
	newStore, err := storage.Open(path)
	if err != nil {
		logger.Warnf("freqtrade risk store 初始化失败: %v", err)
		return nil
	}
	m.mu.Lock()
	if m.riskStore == nil {
		m.riskStore = newStore
		store = newStore
	} else {
		store = m.riskStore
		newStore.Close()
	}
	m.mu.Unlock()
	return store
}

func (m *Manager) recordRisk(rec storage.RiskRecord) {
	store := m.ensureRiskStore()
	if store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := store.Upsert(ctx, rec); err != nil {
		logger.Warnf("freqtrade risk store upsert 失败: %v", err)
	}
}

func (m *Manager) loadRiskRecord(ctx context.Context, tradeID int) (storage.RiskRecord, bool) {
	store := m.ensureRiskStore()
	if store == nil {
		return storage.RiskRecord{}, false
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rec, ok, err := store.Get(cctx, tradeID)
	if err != nil {
		logger.Warnf("freqtrade risk store 查询失败: %v", err)
		return storage.RiskRecord{}, false
	}
	return rec, ok
}

func (m *Manager) lookupAmount(tradeID int) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pos, ok := m.positions[tradeID]; ok {
		return pos.Amount
	}
	return 0
}

func (m *Manager) recordOrder(ctx context.Context, msg WebhookMessage, action string, price float64, executedAt time.Time) {
	if m.orderRec == nil || action == "" {
		return
	}
	symbol := freqtradePairToSymbol(msg.Pair)
	if symbol == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if executedAt.IsZero() {
		executedAt = time.Now()
	}
	order := market.Order{
		Symbol:     symbol,
		Action:     action,
		Side:       strings.ToLower(strings.TrimSpace(msg.Direction)),
		Type:       "freqtrade",
		Price:      price,
		Quantity:   float64(msg.Amount),
		Notional:   float64(msg.StakeAmount),
		DecidedAt:  executedAt,
		ExecutedAt: executedAt,
	}
	if data, err := json.Marshal(msg); err == nil {
		order.Meta = data
	}
	if _, err := m.orderRec.RecordOrder(ctx, &order); err != nil {
		logger.Warnf("freqtrade manager: record order failed: %v", err)
	}
}

func (m *Manager) syncInitialRisk(ctx context.Context, traceID string, tradeID int, symbol string) {
	if tradeID <= 0 || strings.TrimSpace(symbol) == "" {
		return
	}
	rec, ok := m.loadRiskRecord(ctx, tradeID)
	if !ok {
		return
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if rec.StopLoss > 0 {
		title := "自动同步止损"
		lines := []string{
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", symbol),
			fmt.Sprintf("目标止损: %.4f", rec.StopLoss),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
		}
		if reason := strings.TrimSpace(rec.Reason); reason != "" {
			lines = append(lines, "理由: "+reason)
		}
		m.notify(title+" ⏳", lines...)
		dec := decision.Decision{
			Symbol:   symbol,
			StopLoss: rec.StopLoss,
			Reasoning: func() string {
				if strings.TrimSpace(rec.Reason) != "" {
					return rec.Reason
				}
				return "初始同步止损"
			}(),
		}
		if err := m.adjustStopLoss(ctx, traceID, dec); err != nil {
			logger.Warnf("自动同步止损失败 trade_id=%d: %v", tradeID, err)
			lines = append(lines, fmt.Sprintf("错误: %v", err))
			m.notify("自动同步止损失败 ❌", lines...)
		} else {
			m.notify("自动同步止损成功 ✅", lines...)
		}
	}
	if rec.TakeProfit > 0 {
		title := "自动同步止盈"
		lines := []string{
			fmt.Sprintf("交易ID: %d", tradeID),
			fmt.Sprintf("标的: %s", symbol),
			fmt.Sprintf("目标止盈: %.4f", rec.TakeProfit),
			fmt.Sprintf("Trace: %s", m.ensureTrace(traceID)),
		}
		if reason := strings.TrimSpace(rec.Reason); reason != "" {
			lines = append(lines, "理由: "+reason)
		}
		m.notify(title+" ⏳", lines...)
		dec := decision.Decision{
			Symbol:     symbol,
			TakeProfit: rec.TakeProfit,
			Reasoning: func() string {
				if strings.TrimSpace(rec.Reason) != "" {
					return rec.Reason
				}
				return "初始同步止盈"
			}(),
		}
		if err := m.adjustTakeProfit(ctx, traceID, dec); err != nil {
			logger.Warnf("自动同步止盈失败 trade_id=%d: %v", tradeID, err)
			lines = append(lines, fmt.Sprintf("错误: %v", err))
			m.notify("自动同步止盈失败 ❌", lines...)
		} else {
			m.notify("自动同步止盈成功 ✅", lines...)
		}
	}
}

func (m *Manager) notify(title string, lines ...string) {
	if m == nil || m.notifier == nil {
		return
	}
	title = strings.TrimSpace(title)
	bodyLines := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		bodyLines = append(bodyLines, ln)
	}
	body := strings.Join(bodyLines, "\n")
	if body == "" {
		body = "-"
	}
	msg := fmt.Sprintf("*%s*\n```text\n%s\n```", title, body)
	if err := m.notifier.SendText(msg); err != nil {
		logger.Warnf("freqtrade notifier 推送失败: %v", err)
	}
}

func shortReason(desc string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return ""
	}
	const maxLen = 200
	runes := []rune(desc)
	if len(runes) <= maxLen {
		return desc
	}
	return string(runes[:maxLen]) + "..."
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func formatQty(val float64) string {
	if val == 0 {
		return "-"
	}
	return fmt.Sprintf("%.4f", val)
}

func formatPrice(val float64) string {
	if val == 0 {
		return "-"
	}
	return fmt.Sprintf("%.4f", val)
}

func parseProfitRatio(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		if t == "" {
			return 0
		}
		if val, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return val
		}
	}
	return 0
}
