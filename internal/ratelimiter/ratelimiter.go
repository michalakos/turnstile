// Package ratelimiter implements token bucket rate limiting backed by Redis.
package ratelimiter

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed token_bucket.lua
var luaScript string

// Result holds the output from a rate limit check.
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	RetryAfter int64
}

// RateLimiter checks rate limits against Redis.
type RateLimiter interface {
	Check(ctx context.Context, key string, cost, maxTokens, refillRate int64) (*Result, error)
}

// rateLimiter implements RateLimiter using Redis and the token bucket Lua script.
type rateLimiter struct {
	client    *redis.Client
	scriptSHA string
	logger    *slog.Logger
	now       func() int64
}

// New creates a RateLimiter and preloads the Lua script into Redis.
// If the preload fails (e.g., Redis is down), the limiter still works
// by falling back to EVAL at runtime.
func New(client *redis.Client, logger *slog.Logger) RateLimiter {
	rl := &rateLimiter{
		client: client,
		logger: logger,
		now:    func() int64 { return time.Now().Unix() },
	}

	// Preload the Lua script to get its SHA hash for EVALSHA.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sha, err := client.ScriptLoad(ctx, luaScript).Result()
	if err != nil {
		logger.Warn("failed to preload Lua script, will use EVAL fallback", "error", err)
	} else {
		rl.scriptSHA = sha
	}

	return rl
}

// Check executes the rate limit check for the given key and cost.
// It tries EVALSHA first, falls back to EVAL, and fails open on Redis errors.
func (rl *rateLimiter) Check(ctx context.Context, key string, cost, maxTokens, refillRate int64) (*Result, error) {
	now := rl.now()
	keys := []string{key}
	args := []any{maxTokens, refillRate, cost, now}

	var raw any
	var err error

	// Try EVALSHA first (fast path: script already cached in Redis).
	if rl.scriptSHA != "" {
		raw, err = rl.client.EvalSha(ctx, rl.scriptSHA, keys, args...).Result()
	}

	// Fall back to EVAL if EVALSHA failed with NOSCRIPT or SHA was never set.
	if rl.scriptSHA == "" || isNoScriptErr(err) {
		if isNoScriptErr(err) {
			rl.logger.Warn("EVALSHA returned NOSCRIPT, falling back to EVAL")
		}
		raw, err = rl.client.Eval(ctx, luaScript, keys, args...).Result()
	}

	// Fail-open: if Redis is unreachable, allow the request.
	if err != nil {
		rl.logger.Error("redis error, failing open", "error", err, "key", key)
		return &Result{
			Allowed:   true,
			Remaining: maxTokens,
			Limit:     maxTokens,
		}, nil
	}

	return parseResult(raw)
}

// parseResult converts the Lua script's return value (a 4-element array)
// into a *Result. Redis returns Lua tables as []interface{} with int64 elements.
func parseResult(raw any) (*Result, error) {
	res, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", raw)
	}
	if len(res) != 4 {
		return nil, fmt.Errorf("unexpected result length: %d", len(res))
	}

	allowed, ok := res[0].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected type for allowed: %T", res[0])
	}
	remaining, ok := res[1].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected type for remaining: %T", res[1])
	}
	limit, ok := res[2].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected type for limit: %T", res[2])
	}
	retryAfter, ok := res[3].(int64)
	if !ok {
		return nil, fmt.Errorf("unexpected type for retry_after: %T", res[3])
	}

	return &Result{
		Allowed:    allowed == 1,
		Remaining:  remaining,
		Limit:      limit,
		RetryAfter: retryAfter,
	}, nil
}

// isNoScriptErr checks if a Redis error is a NOSCRIPT error,
// which means the script SHA is not cached and we need to use EVAL.
func isNoScriptErr(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "NOSCRIPT")
}
