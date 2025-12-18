package circuit

import (
	"brale/internal/logger"
	"sync"
	"time"
)

// State defines the circuit breaker state
type State int

const (
	StateClosed   State = iota // Normal operation
	StateOpen                  // Circuit is open (failing)
	StateHalfOpen              // Recovering, testing if service is back
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreaker implements a simple state machine to prevent cascading failures.
type CircuitBreaker struct {
	mu            sync.Mutex
	state         State
	failures      int
	threshold     int
	timeout       time.Duration
	lastFailure   time.Time
	name          string
	onStateChange func(name string, from, to State)
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(name string, threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:      name,
		threshold: threshold,
		timeout:   timeout,
		state:     StateClosed,
	}
}

// SetStateChangeHandler sets a callback for state changes.
func (cb *CircuitBreaker) SetStateChangeHandler(handler func(name string, from, to State)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = handler
}

// Allow limits whether execution should proceed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateClosed {
		return true
	}

	if cb.state == StateOpen {
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.transition(StateHalfOpen)
			return true
		}
		return false
	}

	// Half-Open: Allow one request to probe
	return true
}

// RecordSuccess should be called when an operation succeeds.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.transition(StateClosed)
		cb.failures = 0
	} else if cb.state == StateClosed {
		cb.failures = 0
	}
}

// RecordFailure should be called when an operation fails.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == StateClosed {
		if cb.failures >= cb.threshold {
			cb.transition(StateOpen)
		}
	} else if cb.state == StateHalfOpen {
		cb.transition(StateOpen)
	}
}

func (cb *CircuitBreaker) transition(to State) {
	from := cb.state
	cb.state = to
	if cb.onStateChange != nil {
		go cb.onStateChange(cb.name, from, to)
	} else {
		logger.Warnf("CircuitBreaker %s state change: %s -> %s (failures=%d/%d, timeout=%s, lastFailure=%s ago)",
			cb.name, from, to, cb.failures, cb.threshold, cb.timeout, time.Since(cb.lastFailure).Round(time.Second))
	}
}
