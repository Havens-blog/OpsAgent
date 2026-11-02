package chat

import (
	"context"
	"errors"
)

// ErrTokenBudgetExceeded is returned by StreamWithTools when the accumulated
// token usage reaches the budget injected into the context before the next
// LLM call.
var ErrTokenBudgetExceeded = errors.New("token budget exceeded")

type tokenBudgetKey struct{}

// WithTokenBudget attaches a token budget to ctx. Values <= 0 mean unlimited.
func WithTokenBudget(ctx context.Context, n int64) context.Context {
	return context.WithValue(ctx, tokenBudgetKey{}, n)
}

// TokenBudgetFrom returns the token budget stored in ctx, or 0 (unlimited)
// when absent.
func TokenBudgetFrom(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	n, _ := ctx.Value(tokenBudgetKey{}).(int64)
	return n
}
