//go:build integration

package app

import (
	"context"
	"errors"
	"testing"
)

func TestRateLimitRetryAfter(t *testing.T) {
	a, _ := invitationSecurityFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := a.limit(ctx, "retry-after-test", 2); err != nil {
			t.Fatalf("attempt %d limited early: %v", i+1, err)
		}
	}
	err := a.limit(ctx, "retry-after-test", 2)
	var p *Problem
	if !errors.As(err, &p) || p.Status != 429 {
		t.Fatalf("third attempt: %v, want 429", err)
	}
	// The window is 15 minutes and just started, so the hint is just under 900 seconds.
	if p.RetryAfter < 890 || p.RetryAfter > 900 {
		t.Fatalf("RetryAfter %d, want about 900", p.RetryAfter)
	}
	// An aged window reports only the remaining time.
	if _, err := a.DB.Exec(ctx, `UPDATE auth_limits SET window_start=window_start-interval '14 minutes' WHERE bucket=$1`, digest("retry-after-test")); err != nil {
		t.Fatal(err)
	}
	if err = a.limit(ctx, "retry-after-test", 2); !errors.As(err, &p) || p.RetryAfter < 50 || p.RetryAfter > 60 {
		t.Fatalf("aged window: %v RetryAfter %d, want about 60", err, p.RetryAfter)
	}
}
