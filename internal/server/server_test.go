package server

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/michalakos/turnstile/config"
	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/ratelimiter"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockLimiter struct {
	result      *ratelimiter.Result
	err         error
	capturedKey string
	capturedMax int64
	capturedRate int64
}

func (m *mockLimiter) Check(_ context.Context, key string, _, maxTokens, refillRate int64) (*ratelimiter.Result, error) {
	m.capturedKey = key
	m.capturedMax = maxTokens
	m.capturedRate = refillRate
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Server.Port = ":50051"
	cfg.Defaults = config.RuleConfig{MaxTokens: 10, RefillRate: 1}
	cfg.Actions = map[string]config.RuleConfig{
		"login":    {MaxTokens: 5, RefillRate: 1},
		"api_call": {MaxTokens: 100, RefillRate: 10},
	}
	return cfg
}

func allowedResult() *ratelimiter.Result {
	return &ratelimiter.Result{Allowed: true, Remaining: 4, Limit: 5, RetryAfter: 0}
}

func newServer(ml *mockLimiter) *Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(ml, testConfig(), logger)
}

func TestMissingIdentifier(t *testing.T) {
	s := newServer(&mockLimiter{result: allowedResult()})
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{Action: "login", Cost: 1})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", code)
	}
}

func TestMissingAction(t *testing.T) {
	s := newServer(&mockLimiter{result: allowedResult()})
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{Identifier: "u1", Cost: 1})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", code)
	}
}

func TestCostExceedsActionMaxTokens(t *testing.T) {
	s := newServer(&mockLimiter{result: allowedResult()})
	// login has max_tokens=5; cost=6 should fail
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "login", Cost: 6,
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", code)
	}
}

func TestCostExceedsDefaultMaxTokens(t *testing.T) {
	s := newServer(&mockLimiter{result: allowedResult()})
	// unknown action uses defaults (max_tokens=10); cost=11 should fail
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "unknown", Cost: 11,
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", code)
	}
}

func TestZeroCostDefaultsToOne(t *testing.T) {
	ml := &mockLimiter{result: allowedResult()}
	s := newServer(ml)
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "login", Cost: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ml.capturedKey == "" {
		t.Fatal("limiter was not called")
	}
}

func TestKnownActionUsesActionLimits(t *testing.T) {
	ml := &mockLimiter{result: allowedResult()}
	s := newServer(ml)
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "login", Cost: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ml.capturedMax != 5 {
		t.Fatalf("expected maxTokens=5 for login, got %d", ml.capturedMax)
	}
	if ml.capturedRate != 1 {
		t.Fatalf("expected refillRate=1 for login, got %d", ml.capturedRate)
	}
}

func TestUnknownActionFallsBackToDefaults(t *testing.T) {
	ml := &mockLimiter{result: &ratelimiter.Result{Allowed: true, Remaining: 9, Limit: 10}}
	s := newServer(ml)
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "unknown", Cost: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ml.capturedMax != 10 {
		t.Fatalf("expected maxTokens=10 (defaults), got %d", ml.capturedMax)
	}
}

func TestKeyFormat(t *testing.T) {
	ml := &mockLimiter{result: allowedResult()}
	s := newServer(ml)
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "user123", Action: "login", Cost: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ml.capturedKey != "user123:login" {
		t.Fatalf("expected key 'user123:login', got %q", ml.capturedKey)
	}
}

func TestLimiterErrorReturnsInternal(t *testing.T) {
	ml := &mockLimiter{err: context.DeadlineExceeded}
	s := newServer(ml)
	_, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "login", Cost: 1,
	})
	if code := status.Code(err); code != codes.Internal {
		t.Fatalf("expected Internal, got %v", code)
	}
}

func TestAllowedResponseMapsFields(t *testing.T) {
	ml := &mockLimiter{result: &ratelimiter.Result{Allowed: true, Remaining: 4, Limit: 5, RetryAfter: 0}}
	s := newServer(ml)
	resp, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "login", Cost: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Allowed {
		t.Fatal("expected allowed=true")
	}
	if resp.Remaining != 4 {
		t.Fatalf("expected remaining=4, got %d", resp.Remaining)
	}
	if resp.Limit != 5 {
		t.Fatalf("expected limit=5, got %d", resp.Limit)
	}
}

func TestDeniedResponseMapsRetryAfter(t *testing.T) {
	ml := &mockLimiter{result: &ratelimiter.Result{Allowed: false, Remaining: 0, Limit: 5, RetryAfter: 3}}
	s := newServer(ml)
	resp, err := s.CheckRateLimit(context.Background(), &pb.RateLimitRequest{
		Identifier: "u1", Action: "login", Cost: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Allowed {
		t.Fatal("expected allowed=false")
	}
	if resp.RetryAfter != 3 {
		t.Fatalf("expected retry_after=3, got %d", resp.RetryAfter)
	}
}
