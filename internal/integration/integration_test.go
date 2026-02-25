//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/michalakos/turnstile/internal/ratelimiter"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("get container port: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: host + ":" + port.Port(),
	})
	t.Cleanup(func() { client.Close() })
	return client
}

func newLimiter(t *testing.T, client *redis.Client) ratelimiter.RateLimiter {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return ratelimiter.New(client, logger)
}

func TestFullStackAllowed(t *testing.T) {
	client := startRedis(t)
	rl := newLimiter(t, client)

	result, err := rl.Check(context.Background(), "u1:login", 1, 10, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed")
	}
	if result.Remaining != 9 {
		t.Fatalf("expected 9 remaining, got %d", result.Remaining)
	}
}

func TestBucketExhaustionIntegration(t *testing.T) {
	client := startRedis(t)
	rl := newLimiter(t, client)

	for i := range 5 {
		result, err := rl.Check(context.Background(), "u1:login", 1, 5, 1)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !result.Allowed {
			t.Fatalf("call %d: expected allowed", i)
		}
	}

	result, err := rl.Check(context.Background(), "u1:login", 1, 5, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied after bucket exhaustion")
	}
	if result.RetryAfter <= 0 {
		t.Fatalf("expected RetryAfter > 0, got %d", result.RetryAfter)
	}
}

func TestTwoActionsOnSameIdentifierAreIsolated(t *testing.T) {
	client := startRedis(t)
	rl := newLimiter(t, client)

	for range 5 {
		_, _ = rl.Check(context.Background(), "u1:login", 1, 5, 1)
	}

	result, err := rl.Check(context.Background(), "u1:api_call", 1, 100, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("api_call bucket should be independent of login bucket")
	}
}

func TestTwoIdentifiersSameActionAreIsolated(t *testing.T) {
	client := startRedis(t)
	rl := newLimiter(t, client)

	for range 5 {
		_, _ = rl.Check(context.Background(), "u1:login", 1, 5, 1)
	}

	result, err := rl.Check(context.Background(), "u2:login", 1, 5, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("u2 bucket should be independent of u1 bucket")
	}
}
