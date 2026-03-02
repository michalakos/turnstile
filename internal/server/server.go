package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/michalakos/turnstile/config"
	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/metrics"
	"github.com/michalakos/turnstile/internal/ratelimiter"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the RateLimiterServer gRPC interface.
type Server struct {
	pb.UnimplementedRateLimiterServer
	limiter   ratelimiter.RateLimiter
	appConfig *config.Config
	logger    *slog.Logger
	metrics   *metrics.Metrics
}

// New creates a Server with the given rate limiter, config, logger, and metrics.
func New(limiter ratelimiter.RateLimiter, cfg *config.Config, logger *slog.Logger, m *metrics.Metrics) *Server {
	return &Server{
		limiter:   limiter,
		appConfig: cfg,
		logger:    logger,
		metrics:   m,
	}
}

// CheckRateLimit handles a rate limit check request.
func (s *Server) CheckRateLimit(ctx context.Context, req *pb.RateLimitRequest) (*pb.RateLimitResponse, error) {
	cost := req.GetCost()
	if cost == 0 {
		cost = 1
	}

	if req.GetIdentifier() == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier is required")
	}
	if req.GetAction() == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}
	if cost < 1 {
		return nil, status.Error(codes.InvalidArgument, "cost must be at least 1")
	}

	rule := s.appConfig.RuleFor(req.GetAction())

	if cost > rule.MaxTokens {
		return nil, status.Errorf(codes.InvalidArgument, "cost %d exceeds maximum tokens %d", cost, rule.MaxTokens)
	}

	key := fmt.Sprintf("%s:%s", req.GetIdentifier(), req.GetAction())

	result, err := s.limiter.Check(ctx, key, cost, rule.MaxTokens, rule.RefillRate)
	if err != nil {
		s.logger.Error("rate limiter check failed", "error", err, "key", key)
		if s.metrics != nil {
			s.metrics.RecordRedisError()
		}
		return nil, status.Error(codes.Internal, "internal rate limiter error")
	}

	return &pb.RateLimitResponse{
		Allowed:    result.Allowed,
		Remaining:  result.Remaining,
		Limit:      result.Limit,
		RetryAfter: result.RetryAfter,
	}, nil
}
