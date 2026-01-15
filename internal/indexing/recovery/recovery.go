// Package recovery handles error recovery and retry strategies.
//
// # Design: Resilience
//
// Transient failures (network/RPC) are retried with exponential backoff.
// Permanent failures (invalid data) are marked for manual review.
//
// # Failure Categories
//
//   - Transient: RPC timeout, rate limits (Retry)
//   - Parsing: Invalid JSON/data format (Mark Failed)
//   - Data: Logic error, missing deps (Mark Failed)
//   - Critical: DB down, OOM (Stop System)
package recovery

// FailureCategory classifies validation errors.
type FailureCategory string

const (
	CategoryTransient FailureCategory = "transient"
	CategoryPermanent FailureCategory = "permanent"
	CategoryCritical  FailureCategory = "critical"
)

// Classifier determines the category of an error.
type Classifier func(err error) FailureCategory
