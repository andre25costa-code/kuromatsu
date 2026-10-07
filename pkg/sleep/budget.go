package sleep

import (
	"context"
	"errors"
)

var ErrBudgetExceeded = errors.New("sleep token budget exhausted")

type tokenBudgetKey struct{}

func withTokenBudget(ctx context.Context, remaining int) context.Context {
	return context.WithValue(ctx, tokenBudgetKey{}, remaining)
}

// OutputTokenLimit reserves a conservative byte-based input estimate plus
// framing before authorizing output. The provider must enforce max_tokens.
func OutputTokenLimit(ctx context.Context, system, user string) (int, error) {
	remaining, ok := ctx.Value(tokenBudgetKey{}).(int)
	if !ok {
		return 2048, nil
	}
	available := remaining - len(system) - len(user) - 512
	if available <= 0 {
		return 0, ErrBudgetExceeded
	}
	return min(2048, available), nil
}
