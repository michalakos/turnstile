package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/michalakos/turnstile/config"
	pb "github.com/michalakos/turnstile/gen/proto"
	"github.com/michalakos/turnstile/internal/health"
	"github.com/michalakos/turnstile/internal/interceptors"
	"github.com/michalakos/turnstile/internal/metrics"
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

func buildLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func main() {
	bootstrap := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(configPath())
	if err != nil {
		bootstrap.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := buildLogger(cfg.Logging.Level, cfg.Logging.Format)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisAddr := cfg.Redis.Addr
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		redisAddr = addr
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	m := metrics.New()
	healthHandler := health.New(redisClient)

	limiter := ratelimiter.New(redisClient, logger)
	srv := server.New(limiter, cfg, logger, m)

	listener, err := net.Listen("tcp", cfg.Server.Port)
	if err != nil {
		logger.Error("failed to listen", "port", cfg.Server.Port, "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.LoggingInterceptor(logger),
			interceptors.MetricsInterceptor(m),
		),
	)
	pb.RegisterRateLimiterServer(grpcServer, srv)
	reflection.Register(grpcServer)

	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	mux.HandleFunc("/health/live", healthHandler.Live)
	mux.HandleFunc("/health/ready", healthHandler.Ready)
	httpServer := &http.Server{
		Addr:    cfg.Observability.MetricsPort,
		Handler: mux,
	}

	go func() {
		logger.Info("starting HTTP server", "addr", cfg.Observability.MetricsPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server failed", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		logger.Info("shutting down gracefully...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		grpcServer.GracefulStop()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("HTTP server shutdown failed", "error", err)
		}

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
