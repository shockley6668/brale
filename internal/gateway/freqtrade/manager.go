package freqtrade

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"brale/internal/config"
	"brale/internal/gateway/database"
	"brale/internal/gateway/exchange"
	"brale/internal/gateway/notifier"
	"brale/internal/logger"
	"brale/internal/store"
	"brale/internal/trader"
)

type Logger interface {
	Insert(ctx context.Context, rec database.DecisionLogRecord) (int64, error)
}

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

	trader *trader.Trader

	openPlanMu    sync.Mutex
	openPlanCache map[string]cachedOpenPlan

	pendingMu sync.Mutex
	pending   map[int]*pendingState
	notifier  notifier.TextNotifier
}

const (
	pendingStageOpening = "opening"
	pendingStageClosing = "closing"
	pendingTimeout      = 11 * time.Minute
	reconcileDelay      = 5 * time.Second
)

func NewManager(client *Client, cfg config.FreqtradeConfig, logStore Logger, posStore database.LivePositionStore, newStore store.Store, textNotifier notifier.TextNotifier, executor exchange.Exchange) (*Manager, error) {

	if posStore == nil {

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
		notifier:      textNotifier,
		openPlanCache: make(map[string]cachedOpenPlan),
	}, nil
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
