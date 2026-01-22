// Package routing handles provider selection, rotation, and failover logic.
//
// This package contains:
//   - Router: interface for provider selection and health tracking
//   - DefaultRouter: implementation with circuit breaker
//   - ProviderRotator: rotation strategies (round-robin, weighted, adaptive, proactive)
//   - Retry: retry logic with exponential backoff and failover
package routing

import (
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"

	"time"

	"github.com/vietddude/watcher/internal/core/domain"
	"github.com/vietddude/watcher/internal/infra/rpc/provider"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Router handles provider selection and health tracking.
type Router interface {
	// AddProvider registers a provider for a specific chain
	AddProvider(chainID domain.ChainID, p provider.Provider)

	// GetProvider returns the best available provider for a chain
	GetProvider(chainID domain.ChainID) (provider.Provider, error)

	// GetProviderWithHint returns the best provider with a preference hint
	GetProviderWithHint(chainID domain.ChainID, preferredProvider string) (provider.Provider, error)

	// RotateProvider forces a provider rotation for a chain
	RotateProvider(chainID domain.ChainID) (provider.Provider, error)

	// GetAllProviders returns all providers for a chain
	GetAllProviders(chainID domain.ChainID) []provider.Provider

	// RecordSuccess tracks successful calls
	RecordSuccess(providerName string, latency time.Duration)

	// RecordFailure tracks failed calls
	RecordFailure(providerName string, err error)
}

// BudgetChecker is a minimal interface for budget checking in routing.
type BudgetChecker interface {
	CanMakeCall(providerName string) bool
}

type providerMetrics struct {
	successCount     int
	failureCount     int
	totalLatency     time.Duration
	lastSuccessAt    time.Time
	lastFailureAt    time.Time
	consecutiveFails int
	circuitOpen      bool
}

// DefaultRouter implements smart provider selection with circuit breaker.
type DefaultRouter struct {
	mu             sync.RWMutex
	chainProviders map[domain.ChainID][]provider.Provider
	providerHealth map[string]*providerMetrics
	rotator        *ProviderRotator
	budget         BudgetChecker
}

// NewRouter creates a new router with round-robin rotation.
func NewRouter(budget BudgetChecker) *DefaultRouter {
	return &DefaultRouter{
		chainProviders: make(map[domain.ChainID][]provider.Provider),
		providerHealth: make(map[string]*providerMetrics),
		rotator:        NewProviderRotator(RotationRoundRobin),
		budget:         budget,
	}
}

// NewRouterWithStrategy creates a router with a specific rotation strategy.
func NewRouterWithStrategy(budget BudgetChecker, strategy RotationStrategy) *DefaultRouter {
	return &DefaultRouter{
		chainProviders: make(map[domain.ChainID][]provider.Provider),
		providerHealth: make(map[string]*providerMetrics),
		rotator:        NewProviderRotator(strategy),
		budget:         budget,
	}
}

// AddProvider registers a provider for a chain.
func (r *DefaultRouter) AddProvider(chainID domain.ChainID, p provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chainProviders[chainID] = append(r.chainProviders[chainID], p)
	r.providerHealth[p.GetName()] = &providerMetrics{
		lastSuccessAt: time.Now(),
	}
}

// GetProvider returns the best available provider for a chain.
func (r *DefaultRouter) GetProvider(chainID domain.ChainID) (provider.Provider, error) {
	r.mu.RLock()
	providers := r.chainProviders[chainID]
	r.mu.RUnlock()

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers for chain %s", chainID)
	}

	// Filter out blocked providers AND circuit broken providers
	var available []provider.Provider
	for _, p := range providers {
		// Check circuit breaker first
		if r.isCircuitOpen(p.GetName()) {
			slog.Debug("Provider circuit open", "chain", chainID, "provider", p.GetName())
			continue
		}

		if httpProv, ok := p.(*provider.HTTPProvider); ok {
			status := httpProv.Monitor.CheckProviderStatus()
			if status == provider.StatusBlocked {
				slog.Debug("Provider blocked by monitor", "chain", chainID, "provider", p.GetName())
				continue
			}
			available = append(available, p)
		} else {

			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no available providers for chain %s", chainID)
	}

	// Filter by budget availability
	if r.budget != nil {
		var budgetAvailable []provider.Provider
		for _, p := range available {
			if r.budget.CanMakeCall(p.GetName()) {
				budgetAvailable = append(budgetAvailable, p)
			}
		}
		if len(budgetAvailable) > 0 {
			available = budgetAvailable
		}
	}

	return r.rotator.SelectProvider(chainID, available, r, r.budget)
}

// GetProviderWithHint returns a provider with preference for the hint.
func (r *DefaultRouter) GetProviderWithHint(
	chainID domain.ChainID,
	preferredProvider string,
) (provider.Provider, error) {
	if preferredProvider != "" {
		r.mu.RLock()
		providers := r.chainProviders[chainID]
		r.mu.RUnlock()

		for _, p := range providers {
			if p.GetName() == preferredProvider {
				if httpProv, ok := p.(*provider.HTTPProvider); ok {
					if httpProv.Monitor.CheckProviderStatus() == provider.StatusHealthy {
						return p, nil
					}
				}
			}
		}
	}

	return r.GetProvider(chainID)
}

// RotateProvider forces a provider rotation.
func (r *DefaultRouter) RotateProvider(chainID domain.ChainID) (provider.Provider, error) {
	r.mu.RLock()
	providers := r.chainProviders[chainID]
	r.mu.RUnlock()

	if len(providers) < 2 {
		return r.GetProvider(chainID)
	}

	// Use random selection for forced rotation to ensure distribution
	// and avoid "sticky first" issues with fresh round-robin rotators.
	// We rely on the caller (Coordinator) to retry if it gets the same provider.
	return providers[rand.Intn(len(providers))], nil
}

// GetAllProviders returns all providers for a chain.
func (r *DefaultRouter) GetAllProviders(chainID domain.ChainID) []provider.Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := r.chainProviders[chainID]
	result := make([]provider.Provider, len(providers))
	copy(result, providers)
	return result
}

// RecordSuccess records a successful call.
func (r *DefaultRouter) RecordSuccess(providerName string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	metrics, ok := r.providerHealth[providerName]
	if !ok {
		return
	}

	metrics.successCount++
	metrics.totalLatency += latency
	metrics.lastSuccessAt = time.Now()
	metrics.consecutiveFails = 0
	metrics.circuitOpen = false
}

// RecordFailure records a failed call.
func (r *DefaultRouter) RecordFailure(providerName string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	metrics, ok := r.providerHealth[providerName]
	if !ok {
		return
	}

	metrics.failureCount++
	metrics.lastFailureAt = time.Now()

	// Do not increment consecutive fails for "Not Found" errors (polling for new blocks)
	isNotFound := false
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		isNotFound = true
	} else if strings.Contains(strings.ToLower(err.Error()), "not found") {
		isNotFound = true
	}

	if !isNotFound {
		metrics.consecutiveFails++
	}

	slog.Warn(
		"Provider call failed",
		"provider",
		providerName,
		"consecutive",
		metrics.consecutiveFails,
		"error",
		err,
		"isNotFound",
		isNotFound,
	)

	if metrics.consecutiveFails >= 5 {
		metrics.circuitOpen = true
		slog.Error(
			"Provider circuit opened",
			"provider",
			providerName,
			"consecutive",
			metrics.consecutiveFails,
		)
	}
}

// SetRotationStrategy updates the rotation strategy.
func (r *DefaultRouter) SetRotationStrategy(strategy RotationStrategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rotator = NewProviderRotator(strategy)
}

// GetRotator returns the current rotator.
func (r *DefaultRouter) GetRotator() *ProviderRotator {
	r.mu.RLock()
	return r.rotator
}

// isCircuitOpen checks if the circuit is open for a provider.
// It includes a recovery cooldown (1 minute) to allow probing.
func (r *DefaultRouter) isCircuitOpen(providerName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics, ok := r.providerHealth[providerName]
	if !ok {
		return false
	}

	if metrics.circuitOpen {
		// Allow retry after 1 minute (Recovery probe)
		if time.Since(metrics.lastFailureAt) > 1*time.Minute {
			return false // Treat as closed (half-open effectively)
		}
		return true
	}
	return false
}
