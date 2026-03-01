package interceptors

import (
	"context"
	"log/slog"
	"time"

	pb "github.com/michalakos/turnstile/gen/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func LoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		code := status.Code(err).String()

		attrs := []any{
			"method", info.FullMethod,
			"duration", duration,
			"grpc_code", code,
		}

		if r, ok := req.(*pb.RateLimitRequest); ok {
			attrs = append(attrs, "action", r.GetAction())
		}

		if err != nil {
			attrs = append(attrs, "error", err)
			logger.Error("request failed", attrs...)
			return resp, err
		}

		if r, ok := resp.(*pb.RateLimitResponse); ok {
			attrs = append(attrs, "allowed", r.GetAllowed())
			if r.GetAllowed() {
				logger.Debug("request allowed", attrs...)
			} else {
				logger.Info("request denied", attrs...)
			}
			return resp, nil
		}

		logger.Info("request completed", attrs...)
		return resp, nil
	}
}
