package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	appauth "server_v2/internal/application/auth"
	clientapi "server_v2/internal/application/clientapi"
	"server_v2/internal/config"
	"server_v2/internal/delivery/clientrpc"
	"server_v2/internal/platform/clock"
	"server_v2/internal/platform/randombytes"
	"server_v2/internal/platform/uuidx"
	"server_v2/internal/repository/postgres"
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

	store, err := postgres.Open(context.Background(), cfg.Postgres.DSN())
	if err != nil {
		slog.Error("failed to open postgres", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = store.Close()
	}()

	authRepository := postgres.NewAuthRepository(store.DB())
	clientRepository := postgres.NewClientRepository(store.DB())
	txManager := postgres.NewTxManager(store.DB())
	authService, err := appauth.NewService(
		appauth.Config{
			ChallengeTTL:   cfg.App.SessionChallengeTTL,
			EventRetention: cfg.App.EventRetention,
			EventBatchSize: cfg.App.EventBatchSize,
		},
		clock.Real{},
		uuidx.DefaultGenerator{},
		randombytes.CryptoReader{},
		txManager,
		authRepository,
		authRepository,
		authRepository,
	)
	if err != nil {
		slog.Error("failed to initialize auth service", "error", err)
		os.Exit(1)
	}

	clientService, err := clientapi.NewService(
		clientapi.Config{
			AppName:             cfg.App.Name,
			Version:             "2",
			SessionChallengeTTL: cfg.App.SessionChallengeTTL,
			EventRetention:      cfg.App.EventRetention,
			EventBatchSize:      cfg.App.EventBatchSize,
		},
		clock.Real{},
		uuidx.DefaultGenerator{},
		txManager,
		clientRepository,
		authRepository,
		authService,
	)
	if err != nil {
		slog.Error("failed to initialize client api service", "error", err)
		os.Exit(1)
	}

	clientHandler := clientrpc.NewHandler(logger, authService, clientService)
	httpConnectionBinder := appserver.NewHTTPConnectionBinder(clientHandler)
	httpHandler := appserver.NewHandler(logger, cfg.App.OutputPorts, clientHandler)
	runtime := appserver.NewRuntime(cfg.App, httpHandler, clientHandler, httpConnectionBinder, logger)

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
