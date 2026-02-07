// Package server implements the gRPC handler for the rate limiter service.
package server

import (
	"context"
	"fmt"
	"log/slog"

	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/ratelimiter"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the RateLimiterServer gRPC interface.
type Server struct {
	pb.UnimplementedRateLimiterServer
	limiter ratelimiter.RateLimiter
	config  ratelimiter.Config
	logger  *slog.Logger
}

// New creates a Server with the given rate limiter, config, and logger.
func New(limiter ratelimiter.RateLimiter, cfg ratelimiter.Config, logger *slog.Logger) *Server {
	return &Server{
		limiter: limiter,
		config:  cfg,
		logger:  logger,
	}
}

// CheckRateLimit handles a rate limit check request.
func (s *Server) CheckRateLimit(ctx context.Context, req *pb.RateLimitRequest) (*pb.RateLimitResponse, error) {
	// Default cost to 1 if not provided (proto3 zero value).
	cost := req.GetCost()
	if cost == 0 {
		cost = 1
	}

	// Input validation.
	if req.GetIdentifier() == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier is required")
	}
	if req.GetAction() == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}
	if cost < 1 {
		return nil, status.Error(codes.InvalidArgument, "cost must be at least 1")
	}
	if cost > s.config.MaxTokens {
		return nil, status.Errorf(codes.InvalidArgument, "cost %d exceeds maximum tokens %d", cost, s.config.MaxTokens)
	}

	// Compose the Redis key from identifier and action.
	key := fmt.Sprintf("%s:%s", req.GetIdentifier(), req.GetAction())

	result, err := s.limiter.Check(ctx, key, cost)
	if err != nil {
		s.logger.Error("rate limiter check failed", "error", err, "key", key)
		return nil, status.Error(codes.Internal, "internal rate limiter error")
	}

	return &pb.RateLimitResponse{
		Allowed:    result.Allowed,
		Remaining:  result.Remaining,
		Limit:      result.Limit,
		RetryAfter: result.RetryAfter,
	}, nil
}
