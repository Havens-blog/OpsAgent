// Package core provides retry logic for data source operations.
package core

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// RetryableFunc is a function that can be retried.
type RetryableFunc func() (interface{}, error)

// Retryer handles retry logic for data source operations.
type Retryer struct {
	config *RetryConfig
}

// NewRetryer creates a new retryer with the given configuration.
func NewRetryer(config *RetryConfig) *Retryer {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &Retryer{
		config: config,
	}
}

// Do executes the given function with retry logic.
func (r *Retryer) Do(ctx context.Context, fn RetryableFunc) (interface{}, error) {
	var lastErr error
	var backoff time.Duration

	for attempt := 0; attempt < r.config.MaxAttempts; attempt++ {
		// Check context
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		// Execute the function
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable
		if dsErr, ok := err.(*DataSourceError); ok {
			if !dsErr.IsRetryable() && !r.config.ShouldRetry(string(dsErr.Type)) {
				// Non-retryable error, return immediately
				return nil, err
			}
		} else if !r.config.ShouldRetry("") {
			// Non-DataSourceError and not configured to retry unknown errors
			return nil, err
		}

		// Last attempt, don't sleep
		if attempt == r.config.MaxAttempts-1 {
			break
		}

		// Calculate backoff with jitter
		backoff = r.calculateBackoff(attempt)

		// Wait before retry
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	return nil, fmt.Errorf("max retry attempts reached: %w", lastErr)
}

// DoWithRetry executes the given function with retry logic using a custom config.
func (r *Retryer) DoWithRetry(ctx context.Context, maxAttempts int, fn RetryableFunc) (interface{}, error) {
	originalMaxAttempts := r.config.MaxAttempts
	r.config.MaxAttempts = maxAttempts
	defer func() {
		r.config.MaxAttempts = originalMaxAttempts
	}()

	return r.Do(ctx, fn)
}

// calculateBackoff calculates the backoff duration for a given attempt.
func (r *Retryer) calculateBackoff(attempt int) time.Duration {
	// Calculate exponential backoff
	backoff := r.config.InitialBackoff * time.Duration(float64(r.config.InitialBackoff) * float64(attempt))

	// Apply multiplier
	for i := 0; i < attempt; i++ {
		backoff = time.Duration(float64(backoff) * r.config.BackoffMultiplier)
	}

	// Cap at max backoff
	if backoff > r.config.MaxBackoff {
		backoff = r.config.MaxBackoff
	}

	// Add jitter (±25%)
	jitter := time.Duration(float64(backoff) * (0.25 * (rand.Float64()*2 - 1)))
	backoff += jitter

	// Ensure backoff is positive
	if backoff < 0 {
		backoff = r.config.InitialBackoff
	}

	return backoff
}

// ExecuteWithTimeout executes a function with a timeout and retry logic.
func (r *Retryer) ExecuteWithTimeout(ctx context.Context, timeout time.Duration, fn func() (interface{}, error)) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return r.Do(ctx, fn)
}

// RetryCondition is a function that determines if an error should be retried.
type RetryCondition func(error) bool

// RetryerWithCondition is a retryer that uses a custom retry condition.
type RetryerWithCondition struct {
	*Retryer
	condition RetryCondition
}

// NewRetryerWithCondition creates a new retryer with a custom retry condition.
func NewRetryerWithCondition(config *RetryConfig, condition RetryCondition) *RetryerWithCondition {
	return &RetryerWithCondition{
		Retryer:  NewRetryer(config),
		condition: condition,
	}
}

// Do executes the given function with retry logic using the custom condition.
func (r *RetryerWithCondition) Do(ctx context.Context, fn RetryableFunc) (interface{}, error) {
	wrappedFn := func() (interface{}, error) {
		result, err := fn()
		if err != nil && r.condition != nil {
			if !r.condition(err) {
				return nil, err
			}
		}
		return result, err
	}

	return r.Retryer.Do(ctx, wrappedFn)
}

// ExecuteFunc is a generic function that can be executed with retry logic.
func ExecuteFunc[T any](ctx context.Context, config *RetryConfig, fn func() (T, error)) (T, error) {
	retryer := NewRetryer(config)

	var zero T
	result, err := retryer.Do(ctx, func() (interface{}, error) {
		return fn()
	})

	if err != nil {
		return zero, err
	}

	return result.(T), nil
}

// ExecuteFuncWithContext is a generic function that can be executed with retry logic and context.
func ExecuteFuncWithContext[T any](ctx context.Context, config *RetryConfig, fn func(context.Context) (T, error)) (T, error) {
	retryer := NewRetryer(config)

	var zero T
	result, err := retryer.Do(ctx, func() (interface{}, error) {
		return fn(ctx)
	})

	if err != nil {
		return zero, err
	}

	return result.(T), nil
}

// RetryConditions provides common retry condition functions.
var RetryConditions = struct {
	// IsRetryableError returns true for retryable errors.
	IsRetryableError func(error) bool
	// IsTemporaryError returns true for temporary errors.
	IsTemporaryError func(error) bool
	// IsNetworkOrTimeout returns true for network or timeout errors.
	IsNetworkOrTimeout func(error) bool
}{
	IsRetryableError: func(err error) bool {
		return IsRetryable(err)
	},
	IsTemporaryError: func(err error) bool {
		return IsTemporary(err)
	},
	IsNetworkOrTimeout: func(err error) bool {
		if dsErr, ok := err.(*DataSourceError); ok {
			return dsErr.Type == ErrTypeNetwork || dsErr.Type == ErrTypeTimeout
		}
		return false
	},
}

// RetryStats contains statistics about retry attempts.
type RetryStats struct {
	// Attempts is the number of attempts made.
	Attempts int
	// TotalDuration is the total duration including backoff.
	TotalDuration time.Duration
	// LastError is the last error encountered.
	LastError error
}

// RetryerWithStats is a retryer that tracks statistics.
type RetryerWithStats struct {
	*Retryer
	stats *RetryStats
}

// NewRetryerWithStats creates a new retryer that tracks statistics.
func NewRetryerWithStats(config *RetryConfig) *RetryerWithStats {
	return &RetryerWithStats{
		Retryer: NewRetryer(config),
		stats:   &RetryStats{},
	}
}

// Do executes the given function with retry logic and tracks statistics.
func (r *RetryerWithStats) Do(ctx context.Context, fn RetryableFunc) (interface{}, error) {
	start := time.Now()
	r.stats = &RetryStats{}

	for attempt := 0; attempt < r.config.MaxAttempts; attempt++ {
		r.stats.Attempts = attempt + 1

		// Execute the function
		result, err := fn()
		if err == nil {
			r.stats.TotalDuration = time.Since(start)
			return result, nil
		}

		r.stats.LastError = err

		// Check if error is retryable
		if dsErr, ok := err.(*DataSourceError); ok {
			if !dsErr.IsRetryable() && !r.config.ShouldRetry(string(dsErr.Type)) {
				r.stats.TotalDuration = time.Since(start)
				return nil, err
			}
		}

		// Last attempt, don't sleep
		if attempt == r.config.MaxAttempts-1 {
			break
		}

		// Wait before retry
		backoff := r.calculateBackoff(attempt)
		select {
		case <-ctx.Done():
			r.stats.TotalDuration = time.Since(start)
			return nil, fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	r.stats.TotalDuration = time.Since(start)
	return nil, fmt.Errorf("max retry attempts reached: %w", r.stats.LastError)
}

// Stats returns the retry statistics.
func (r *RetryerWithStats) Stats() *RetryStats {
	return r.stats
}
