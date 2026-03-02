package interceptors

import (
	"context"
	"time"

	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/metrics"
	"google.golang.org/grpc"
)

func MetricsInterceptor(m *metrics.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		m.InflightInc()
		defer m.InflightDec()
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		action := ""
		if r, ok := req.(*pb.RateLimitRequest); ok {
			action = r.GetAction()
		}

		result := "error"
		if err == nil {
			if r, ok := resp.(*pb.RateLimitResponse); ok {
				if r.GetAllowed() {
					result = "allowed"
				} else {
					result = "denied"
				}
			}
		}
		m.RecordRequest(action, result, duration)
		return resp, err
	}
}
