package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/michalakos/turnstile/config"
	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/interceptors"
	"github.com/michalakos/turnstile/internal/ratelimiter"
	"github.com/michalakos/turnstile/internal/server"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func configPath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "config/config.yaml"
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(configPath())
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisAddr := cfg.Redis.Addr
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		redisAddr = addr
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	limiter := ratelimiter.New(redisClient, logger)
	srv := server.New(limiter, cfg, logger)

	listener, err := net.Listen("tcp", cfg.Server.Port)
	if err != nil {
		logger.Error("failed to listen", "port", cfg.Server.Port, "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.LoggingInterceptor(logger),
			// metrics interceptor added in step 2
		),
	)
	pb.RegisterRateLimiterServer(grpcServer, srv)
	reflection.Register(grpcServer)

	go func() {
		<-ctx.Done()
		logger.Info("shutting down gracefully...")
		grpcServer.GracefulStop()
		if err := redisClient.Close(); err != nil {
			logger.Error("failed to close redis client", "error", err)
		}
	}()

	logger.Info("starting gRPC server", "port", cfg.Server.Port)
	if err := grpcServer.Serve(listener); err != nil {
		logger.Error("gRPC server failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}
