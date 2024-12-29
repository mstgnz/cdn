package circuitbreaker

import (
	"errors"
	"sync"
	"time"

	"github.com/mstgnz/cdn/pkg/observability"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

var (
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrTooManyRequests    = errors.New("too many requests")
)

type CircuitBreaker struct {
	name                  string
	state                 State
	failureThreshold      int
	successThreshold      int
	timeout               time.Duration
	failureCount          int
	successCount          int
	lastStateChangeTime   time.Time
	mutex                 sync.RWMutex
	maxConcurrentRequests int
	activeRequests        int
}

func NewCircuitBreaker(name string, failureThreshold, successThreshold int, timeout time.Duration, maxConcurrentRequests int) *CircuitBreaker {
	return &CircuitBreaker{
		name:                  name,
		state:                 StateClosed,
		failureThreshold:      failureThreshold,
		successThreshold:      successThreshold,
		timeout:               timeout,
		lastStateChangeTime:   time.Now(),
		maxConcurrentRequests: maxConcurrentRequests,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.beforeExecute(); err != nil {
		return err
	}

	defer cb.afterExecute()

	err := fn()
	cb.handleResult(err)

	return err
}

func (cb *CircuitBreaker) beforeExecute() error {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()

	switch cb.state {
	case StateOpen:
		if now.Sub(cb.lastStateChangeTime) >= cb.timeout {
			cb.setState(StateHalfOpen)
		} else {
			return ErrCircuitBreakerOpen
		}

	case StateHalfOpen:
		if cb.activeRequests >= cb.maxConcurrentRequests {
			return ErrTooManyRequests
		}
	}

	if cb.activeRequests >= cb.maxConcurrentRequests {
		return ErrTooManyRequests
	}

	cb.activeRequests++
	return nil
}

func (cb *CircuitBreaker) afterExecute() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.activeRequests--
}

func (cb *CircuitBreaker) handleResult(err error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}

	// Update metrics
	observability.CircuitBreakerState.WithLabelValues(cb.name).Set(float64(cb.state))
	observability.CircuitBreakerFailures.WithLabelValues(cb.name).Set(float64(cb.failureCount))
	observability.CircuitBreakerSuccesses.WithLabelValues(cb.name).Set(float64(cb.successCount))
}

func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		cb.failureCount = 0

	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.setState(StateClosed)
		}
	}
}

func (cb *CircuitBreaker) onFailure() {
	switch cb.state {
	case StateClosed:
		cb.failureCount++
		if cb.failureCount >= cb.failureThreshold {
			cb.setState(StateOpen)
		}

	case StateHalfOpen:
		cb.setState(StateOpen)
	}
}

func (cb *CircuitBreaker) setState(state State) {
	cb.state = state
	cb.lastStateChangeTime = time.Now()
	cb.failureCount = 0
	cb.successCount = 0

	// Log state change
	logger := observability.Logger()
	logger.Info().
		Str("circuit_breaker", cb.name).
		Str("state", stateToString(state)).
		Msg("Circuit breaker state changed")
}

func (cb *CircuitBreaker) State() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

func stateToString(state State) string {
	switch state {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}
