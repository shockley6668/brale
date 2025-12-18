package freqtrade

import (
	"sync"
	"time"

	"brale/internal/gateway/database"
	"brale/internal/logger"
)

// PendingStateManager manages the pending state for trades (opening/closing).
// This encapsulates the synchronization and timeout logic.
type PendingStateManager struct {
	mu      sync.Mutex
	states  map[int]*pendingState
	timeout time.Duration

	// Callback for status updates on timeout
	onTimeout func(tradeID int, status database.LiveOrderStatus)
}

// pendingState tracks the stage and timeout timer for a trade.
type pendingState struct {
	stage string
	timer *time.Timer
}

// Pending stage constants
const (
	PendingStageOpening   = "opening"
	PendingStageClosing   = "closing"
	DefaultPendingTimeout = 11 * time.Minute
)

// NewPendingStateManager creates a new pending state manager.
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

// Start marks a trade as pending with a timeout.
func (m *PendingStateManager) Start(tradeID int, stage string) {
	if tradeID <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel any existing timer
	if prev, ok := m.states[tradeID]; ok {
		if prev.timer != nil {
			prev.timer.Stop()
		}
	}

	// Start a new timer
	timer := time.AfterFunc(m.timeout, func() {
		m.handleTimeout(tradeID, stage)
	})
	m.states[tradeID] = &pendingState{stage: stage, timer: timer}
}

// Clear removes the pending state for a trade.
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

// IsPending checks if a trade is in a pending state.
func (m *PendingStateManager) IsPending(tradeID int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.states[tradeID]
	return ok
}

// handleTimeout is called when a pending operation times out.
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

	// Clean up state
	m.mu.Lock()
	delete(m.states, tradeID)
	m.mu.Unlock()

	// Notify callback
	if m.onTimeout != nil {
		m.onTimeout(tradeID, status)
	}
}
