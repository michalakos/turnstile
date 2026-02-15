package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/ratelimiter"
	"github.com/michalakos/turnstile/internal/server"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	grpcPort   = ":50051"
	maxTokens  = 10
	refillRate = 1
)

func redisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr(),
	})

	cfg := ratelimiter.Config{
		MaxTokens:  maxTokens,
		RefillRate: refillRate,
	}

	limiter := ratelimiter.New(redisClient, cfg, logger)
	srv := server.New(limiter, cfg, logger)

	listener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		logger.Error("failed to listen", "port", grpcPort, "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterRateLimiterServer(grpcServer, srv)
	reflection.Register(grpcServer)

	// Shut down gracefully when the context is cancelled.
	go func() {
		<-ctx.Done()
		logger.Info("shutting down gracefully...")
		grpcServer.GracefulStop()
		if err := redisClient.Close(); err != nil {
			logger.Error("failed to close redis client", "error", err)
		}
	}()

	logger.Info("starting gRPC server", "port", grpcPort)
	if err := grpcServer.Serve(listener); err != nil {
		logger.Error("gRPC server failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}
