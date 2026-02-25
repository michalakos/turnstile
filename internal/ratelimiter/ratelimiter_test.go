package ratelimiter

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestLimiter(t *testing.T, mr *miniredis.Miniredis) RateLimiter {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return New(client, discardLogger())
}

func TestFirstRequestAllowed(t *testing.T) {
	mr := miniredis.RunT(t)
	rl := newTestLimiter(t, mr)

	result, err := rl.Check(context.Background(), "user:login", 1, 10, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected first request to be allowed")
	}
	if result.Remaining != 9 {
		t.Fatalf("expected 9 remaining, got %d", result.Remaining)
	}
	if result.Limit != 10 {
		t.Fatalf("expected limit 10, got %d", result.Limit)
	}
}

func TestTokensDecrementAcrossCalls(t *testing.T) {
	mr := miniredis.RunT(t)
	rl := newTestLimiter(t, mr)

	for i := range 3 {
		result, err := rl.Check(context.Background(), "user:api", 1, 10, 1)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !result.Allowed {
			t.Fatalf("call %d: expected allowed", i)
		}
		want := int64(9 - i)
		if result.Remaining != want {
			t.Fatalf("call %d: expected %d remaining, got %d", i, want, result.Remaining)
		}
	}
}

func TestBucketExhaustion(t *testing.T) {
	mr := miniredis.RunT(t)
	rl := newTestLimiter(t, mr)

	for range 5 {
		_, err := rl.Check(context.Background(), "user:login", 1, 5, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	result, err := rl.Check(context.Background(), "user:login", 1, 5, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected 6th request to be denied")
	}
	if result.RetryAfter <= 0 {
		t.Fatalf("expected RetryAfter > 0, got %d", result.RetryAfter)
	}
}

func TestCostGreaterThanOneConsumesMultipleTokens(t *testing.T) {
	mr := miniredis.RunT(t)
	rl := newTestLimiter(t, mr)

	result, err := rl.Check(context.Background(), "user:api", 5, 10, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed")
	}
	if result.Remaining != 5 {
		t.Fatalf("expected 5 remaining, got %d", result.Remaining)
	}
}

func TestTokenRefillAfterElapsedTime(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	fakeNow := time.Now().Unix()
	rl := &rateLimiter{
		client:    client,
		logger:    discardLogger(),
		now:       func() int64 { return fakeNow },
	}
	sha, _ := client.ScriptLoad(context.Background(), luaScript).Result()
	rl.scriptSHA = sha

	for range 5 {
		_, _ = rl.Check(context.Background(), "user:login", 1, 5, 1)
	}

	result, _ := rl.Check(context.Background(), "user:login", 1, 5, 1)
	if result.Allowed {
		t.Fatal("bucket should be empty before time advance")
	}

	fakeNow += 3

	result, err := rl.Check(context.Background(), "user:login", 1, 5, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected request allowed after token refill")
	}
}

func TestFailOpenWhenRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rl := newTestLimiter(t, mr)

	mr.Close()

	result, err := rl.Check(context.Background(), "user:login", 1, 10, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected fail-open (allowed) when Redis is down")
	}
}

func TestNoscriptFallbackToEval(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	rl := &rateLimiter{
		client:    client,
		scriptSHA: "0000000000000000000000000000000000000000",
		logger:    discardLogger(),
		now:       func() int64 { return time.Now().Unix() },
	}

	result, err := rl.Check(context.Background(), "user:api", 1, 10, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed after EVAL fallback")
	}
}
