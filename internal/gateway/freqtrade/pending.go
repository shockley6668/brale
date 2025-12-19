package freqtrade

import (
	"sync"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/logger"
)

type PendingStateManager struct {
	mu      sync.Mutex
	states  map[int]*pendingState
	timeout time.Duration

	onTimeout func(tradeID int, status database.LiveOrderStatus)
}

type pendingState struct {
	stage string
	timer *time.Timer
}

const (
	PendingStageOpening   = "opening"
	PendingStageClosing   = "closing"
	DefaultPendingTimeout = 11 * time.Minute
)

func NewPendingStateManager(timeout time.Duration, onTimeout func(tradeID int, status database.LiveOrderStatus)) *PendingStateManager {
	if timeout <= 0 {
		timeout = DefaultPendingTimeout
	}
	return &PendingStateManager{
		states:    make(map[int]*pendingState),
		timeout:   timeout,
		onTimeout: onTimeout,
	}
}

func (m *PendingStateManager) Start(tradeID int, stage string) {
	if tradeID <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if prev, ok := m.states[tradeID]; ok {
		if prev.timer != nil {
			prev.timer.Stop()
		}
	}

	timer := time.AfterFunc(m.timeout, func() {
		m.handleTimeout(tradeID, stage)
	})
	m.states[tradeID] = &pendingState{stage: stage, timer: timer}
}

func (m *PendingStateManager) Clear(tradeID int, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ps, ok := m.states[tradeID]; ok {
		if ps.stage == stage && ps.timer != nil {
			ps.timer.Stop()
		}
		delete(m.states, tradeID)
	}
}

func (m *PendingStateManager) IsPending(tradeID int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.states[tradeID]
	return ok
}

func (m *PendingStateManager) handleTimeout(tradeID int, stage string) {
	var status database.LiveOrderStatus
	switch stage {
	case PendingStageOpening:
		logger.Warnf("freqtrade: opening timeout trade=%d, reverting to retrying", tradeID)
		status = database.LiveOrderStatusRetrying
	case PendingStageClosing:
		logger.Warnf("freqtrade: closing timeout trade=%d, reverting to open", tradeID)
		status = database.LiveOrderStatusOpen
	default:
		return
	}

	m.mu.Lock()
	delete(m.states, tradeID)
	m.mu.Unlock()

	if m.onTimeout != nil {
		m.onTimeout(tradeID, status)
	}
}
