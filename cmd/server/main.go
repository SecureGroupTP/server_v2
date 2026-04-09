package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"server_v2/internal/config"
	appserver "server_v2/internal/server"
)

const (
	shutdownTimeout = 10 * time.Second
)

func main() {
	configPath := os.Getenv("APP_CONFIG_PATH")
	if configPath == "" {
		configPath = config.DefaultPath
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "path", configPath, "error", err)
		os.Exit(1)
	}

	var slogHandler slog.Handler
	if cfg.Logger.HumanReadable {
		slogHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Logger.SlogLevel()})
	} else {
		slogHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Logger.SlogLevel()})
	}

	logger := slog.New(slogHandler)
	slog.SetDefault(logger)

	httpHandler := appserver.NewHandler(logger, cfg.App.OutputPorts)
	runtime := appserver.NewRuntime(cfg.App, httpHandler, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtimeErrCh := make(chan error, 1)
	go func() {
		runErr := runtime.Run(ctx)
		if runErr != nil {
			runtimeErrCh <- runErr
		}
	}()

	slog.Info(
		"server runtime configured",
		"service", cfg.App.Name,
		"host", cfg.App.Host,
		"tcp_port", cfg.App.Ports.TCPPort,
		"tcp_tls_port", cfg.App.Ports.TCPTLSPort,
		"http_port", cfg.App.Ports.HTTPPort,
		"https_port", cfg.App.Ports.HTTPSPort,
		"ws_port", cfg.App.Ports.WSPort,
		"wss_port", cfg.App.Ports.WSSPort,
		"postgres_host", cfg.Postgres.Host,
		"postgres_port", cfg.Postgres.Port,
		"redis_host", cfg.Redis.Host,
		"redis_port", cfg.Redis.Port,
	)

	select {
	case runErr := <-runtimeErrCh:
		slog.Error("runtime failed", "error", runErr)
		stop()
	case <-ctx.Done():
	}

	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if shutdownErr := runtime.Shutdown(shutdownCtx); shutdownErr != nil {
		slog.Error("graceful shutdown failed", "error", shutdownErr)
		os.Exit(1)
	}

	// Give in-flight logs a small chance to flush in container runtimes.
	time.Sleep(100 * time.Millisecond)
	slog.Info("server stopped")
}
