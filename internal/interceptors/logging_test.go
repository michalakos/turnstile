package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	pb "github.com/michalakos/turnstile/gen/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type capturedLog struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

type capturingHandler struct {
	logs []capturedLog
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.logs = append(h.logs, capturedLog{level: r.Level, message: r.Message, attrs: attrs})
	return nil
}

func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(name string) slog.Handler       { return h }

func info() *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: "/ratelimiter.RateLimiter/CheckRateLimit"}
}

func TestLoggingInterceptor_Allowed(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	req := &pb.RateLimitRequest{Identifier: "u1", Action: "login"}
	resp := &pb.RateLimitResponse{Allowed: true}

	handler := func(_ context.Context, _ any) (any, error) { return resp, nil }

	_, err := LoggingInterceptor(logger)(context.Background(), req, info(), handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(h.logs))
	}

	log := h.logs[0]
	if log.level != slog.LevelDebug {
		t.Errorf("expected Debug level, got %v", log.level)
	}
	if log.attrs["action"] != "login" {
		t.Errorf("expected action=login, got %v", log.attrs["action"])
	}
	if log.attrs["allowed"] != true {
		t.Errorf("expected allowed=true, got %v", log.attrs["allowed"])
	}
	if _, hasErr := log.attrs["error"]; hasErr {
		t.Error("error field should not be present on success")
	}
}

func TestLoggingInterceptor_Denied(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	req := &pb.RateLimitRequest{Identifier: "u1", Action: "login"}
	resp := &pb.RateLimitResponse{Allowed: false}

	handler := func(_ context.Context, _ any) (any, error) { return resp, nil }

	_, err := LoggingInterceptor(logger)(context.Background(), req, info(), handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(h.logs))
	}

	log := h.logs[0]
	if log.level != slog.LevelInfo {
		t.Errorf("expected Info level, got %v", log.level)
	}
	if log.attrs["allowed"] != false {
		t.Errorf("expected allowed=false, got %v", log.attrs["allowed"])
	}
}

func TestLoggingInterceptor_HandlerError(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	req := &pb.RateLimitRequest{Identifier: "u1", Action: "login"}
	grpcErr := status.Error(codes.Internal, "redis down")

	handler := func(_ context.Context, _ any) (any, error) { return nil, grpcErr }

	_, err := LoggingInterceptor(logger)(context.Background(), req, info(), handler)
	if !errors.Is(err, grpcErr) {
		t.Fatalf("expected grpc error to be propagated, got %v", err)
	}
	if len(h.logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(h.logs))
	}

	log := h.logs[0]
	if log.level != slog.LevelError {
		t.Errorf("expected Error level, got %v", log.level)
	}
	if log.attrs["grpc_code"] != codes.Internal.String() {
		t.Errorf("expected grpc_code=Internal, got %v", log.attrs["grpc_code"])
	}
	if log.attrs["error"] == nil {
		t.Error("expected error field to be present")
	}
}

func TestLoggingInterceptor_NonRateLimitRequest(t *testing.T) {
	h := &capturingHandler{}
	logger := slog.New(h)

	req := "not-a-proto-message"
	resp := "some-response"

	handler := func(_ context.Context, _ any) (any, error) { return resp, nil }

	result, err := LoggingInterceptor(logger)(context.Background(), req, info(), handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != resp {
		t.Errorf("expected response to be passed through")
	}
	if len(h.logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(h.logs))
	}

	log := h.logs[0]
	if _, hasAction := log.attrs["action"]; hasAction {
		t.Error("action field should not be present for unknown request type")
	}
	if _, hasAllowed := log.attrs["allowed"]; hasAllowed {
		t.Error("allowed field should not be present for unknown response type")
	}
}
