package routing

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/vietddude/watcher/internal/infra/rpc/provider"
)

func init() {
	rand.New(rand.NewSource(time.Now().UnixNano()))
}

// RotationStrategy defines how providers are rotated.
type RotationStrategy int

const (
	RotationRoundRobin RotationStrategy = iota // Simple sequential rotation
	RotationWeighted                           // Based on quota remaining
	RotationAdaptive                           // Based on performance + quota
	RotationProactive                          // Actively distributes to prevent hitting limits
)

// ProviderRotator handles provider rotation with multiple strategies.
type ProviderRotator struct {
	mu       sync.RWMutex
	strategy RotationStrategy

	lastUsedIndex   map[string]int     // chainID -> index
	providerWeights map[string]float64 // providerName -> weight
	providerScores  map[string]float64 // providerName -> score
}

// NewProviderRotator creates a new rotator with the given strategy.
func NewProviderRotator(strategy RotationStrategy) *ProviderRotator {
	return &ProviderRotator{
		strategy:        strategy,
		lastUsedIndex:   make(map[string]int),
		providerWeights: make(map[string]float64),
		providerScores:  make(map[string]float64),
	}
}

// SelectProvider chooses next provider based on strategy.
func (pr *ProviderRotator) SelectProvider(
	chainID string,
	providers []provider.Provider,
	_ Router,
	budget BudgetChecker,
) (provider.Provider, error) {
	switch pr.strategy {
	case RotationRoundRobin:
		return pr.roundRobin(chainID, providers)
	case RotationWeighted:
		return pr.weighted(chainID, providers)
	case RotationAdaptive:
		return pr.adaptive(chainID, providers)
	case RotationProactive:
		return pr.proactive(chainID, providers, budget)
	default:
		return pr.roundRobin(chainID, providers)
	}
}

func (pr *ProviderRotator) roundRobin(
	chainID string,
	providers []provider.Provider,
) (provider.Provider, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers available")
	}

	index := pr.lastUsedIndex[chainID]
	p := providers[index]
	pr.lastUsedIndex[chainID] = (index + 1) % len(providers)

	return p, nil
}

func (pr *ProviderRotator) weighted(
	_ string,
	providers []provider.Provider,
) (provider.Provider, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers available")
	}

	type providerWeight struct {
		provider provider.Provider
		weight   float64
	}

	var weighted []providerWeight

	for _, p := range providers {
		if httpProv, ok := p.(*provider.HTTPProvider); ok {
			stats := httpProv.Monitor.GetStats()

			usagePercent := stats.UsagePercentage / 100.0
			remainingQuota := 1.0 - usagePercent

			healthFactor := 1.0
			switch stats.Status {
			case provider.StatusDegraded:
				healthFactor = 0.5
			case provider.StatusThrottled:
				healthFactor = 0.1
			case provider.StatusBlocked:
				continue
			}

			weight := remainingQuota * healthFactor

			if weight > 0 {
				weighted = append(weighted, providerWeight{provider: p, weight: weight})
			}
		}
	}

	if len(weighted) == 0 {
		return nil, fmt.Errorf("no healthy providers with quota")
	}

	sort.Slice(weighted, func(i, j int) bool {
		return weighted[i].weight > weighted[j].weight
	})

	return weighted[0].provider, nil
}

func (pr *ProviderRotator) adaptive(
	_ string,
	providers []provider.Provider,
) (provider.Provider, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	type providerScore struct {
		provider provider.Provider
		score    float64
	}

	var scored []providerScore

	for _, p := range providers {
		httpProv, ok := p.(*provider.HTTPProvider)
		if !ok {
			continue
		}

		stats := httpProv.Monitor.GetStats()
		health := p.GetHealth()

		if stats.Status == provider.StatusBlocked {
			continue
		}

		score := pr.calculateScore(stats, health)

		if score > 0 {
			scored = append(scored, providerScore{provider: p, score: score})
		}
	}

	if len(scored) == 0 {
		return nil, fmt.Errorf("no providers with positive score")
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	topN := max(1, len(scored)/5)
	randomIndex := rand.Intn(topN)

	selected := scored[randomIndex].provider
	pr.providerScores[selected.GetName()] = scored[randomIndex].score

	return selected, nil
}

func (pr *ProviderRotator) proactive(
	chainID string,
	providers []provider.Provider,
	_ BudgetChecker,
) (provider.Provider, error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if len(providers) == 0 {
		return nil, fmt.Errorf("no providers available")
	}

	type candidate struct {
		provider        provider.Provider
		usagePercentage float64
		remainingQuota  int
		healthScore     float64
		finalScore      float64
	}

	var candidates []candidate

	for _, p := range providers {
		c := candidate{
			provider:    p,
			healthScore: 100.0,
		}

		if httpProv, ok := p.(*provider.HTTPProvider); ok {
			stats := httpProv.Monitor.GetStats()

			if stats.Status == provider.StatusBlocked {
				continue
			}

			c.usagePercentage = stats.UsagePercentage
			c.remainingQuota = stats.EstimatedDailyLimit - stats.RequestsLast24Hours

			if c.usagePercentage >= 95 {
				continue
			}

			switch stats.Status {
			case provider.StatusThrottled:
				c.healthScore -= 60
			case provider.StatusDegraded:
				c.healthScore -= 30
			}

			c.healthScore -= float64(stats.ThrottleCount429) * 3
			c.healthScore -= float64(stats.ThrottleCount403) * 8
		}

		usageScore := 100.0 - c.usagePercentage
		quotaScore := float64(c.remainingQuota) / 1000.0
		if quotaScore > 100 {
			quotaScore = 100
		}

		c.finalScore = (usageScore * 0.4) + (c.healthScore * 0.3) + (quotaScore * 0.3)

		if c.finalScore > 0 {
			candidates = append(candidates, c)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available providers with quota")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].finalScore > candidates[j].finalScore
	})

	topN := max(1, len(candidates)/2)
	selected := candidates[rand.Intn(topN)]

	return selected.provider, nil
}

func (pr *ProviderRotator) calculateScore(
	stats provider.MonitorStats,
	health provider.HealthStatus,
) float64 {
	score := 100.0

	usagePercent := stats.UsagePercentage
	if usagePercent > 90 {
		score -= 50
	} else if usagePercent > 75 {
		score -= 30
	} else if usagePercent > 50 {
		score -= 10
	}

	latencyMs := stats.AverageLatency.Milliseconds()
	if latencyMs > 3000 {
		score -= 40
	} else if latencyMs > 1000 {
		score -= 20
	} else if latencyMs > 500 {
		score -= 10
	}

	score -= float64(stats.ThrottleCount429) * 5
	score -= float64(stats.ThrottleCount403) * 10
	score -= health.ErrorRate * 50

	switch stats.Status {
	case provider.StatusDegraded:
		score -= 25
	case provider.StatusThrottled:
		score -= 60
	case provider.StatusBlocked:
		score = -1
	}

	return max(0, score)
}
