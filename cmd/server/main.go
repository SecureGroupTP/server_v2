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
	appoutbox "server_v2/internal/application/outbox"
	apppush "server_v2/internal/application/push"
	"server_v2/internal/config"
	"server_v2/internal/delivery/clientrpc"
	"server_v2/internal/integrations/push/fcm"
	"server_v2/internal/platform/clock"
	"server_v2/internal/platform/eventbus"
	"server_v2/internal/platform/logging"
	"server_v2/internal/platform/randombytes"
	"server_v2/internal/platform/uuidx"
	"server_v2/internal/repository/postgres"
	appserver "server_v2/internal/server"
)

const (
	shutdownTimeout = 10 * time.Second
)

func main() {
	logger := logging.WithSource(logging.NewLogger(os.Stdout, slog.LevelInfo), "server_v2/cmd/server")
	slog.SetDefault(logger)

	configPath := os.Getenv("APP_CONFIG_PATH")
	if configPath == "" {
		configPath = config.DefaultPath
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "path", configPath, "error", err)
		os.Exit(1)
	}

	logger = logging.WithSource(logging.NewLogger(os.Stdout, cfg.Logger.SlogLevel()), "server_v2/cmd/server")
	slog.SetDefault(logger)
	logger.Info(
		"logger configured",
		"level", cfg.Logger.Level,
		"human_readable_requested", cfg.Logger.HumanReadable,
		"format", "serilog_compact_json",
	)

	store, err := postgres.Open(context.Background(), cfg.Postgres.DSN())
	if err != nil {
		logger.Error("failed to open postgres", "host", cfg.Postgres.Host, "port", cfg.Postgres.Port, "database", cfg.Postgres.Database, "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = store.Close()
	}()
	logger.Info("postgres connection established", "host", cfg.Postgres.Host, "port", cfg.Postgres.Port, "database", cfg.Postgres.Database)

	authRepository := postgres.NewAuthRepository(store.DB())
	clientRepository := postgres.NewClientRepository(store.DB())
	txManager := postgres.NewTxManager(store.DB())
	bus := eventbus.New()
	fcmClient, err := fcm.NewClient(context.Background(), cfg.Push.FCM)
	if err != nil {
		logger.Error("failed to initialize fcm client", "error", err)
		os.Exit(1)
	}
	pushNotifier := apppush.NewNotifierWithLogger(clientRepository, fcmClient, 256, logging.WithSource(logger, "server_v2/internal/application/push.Notifier"))
	outboxNotifier := appoutbox.NewMultiNotifier(bus, pushNotifier)
	outboxService, err := appoutbox.NewService(appoutbox.Config{
		PollInterval:      cfg.App.OutboxPollInterval,
		BatchSizeSegments: cfg.App.OutboxBatchSizeSegments,
		AckTimeout:        cfg.App.OutboxAckTimeout,
		MaxAttempts:       cfg.App.OutboxMaxAttempts,
		JanitorInterval:   cfg.App.OutboxJanitorInterval,
		AckRetention:      cfg.App.OutboxAckRetention,
		DropRetention:     cfg.App.OutboxDropRetention,
	}, clock.Real{}, txManager, authRepository, outboxNotifier)
	if err != nil {
		logger.Error("failed to initialize outbox service", "error", err)
		os.Exit(1)
	}
	notifyingEvents := eventbus.NewNotifyingEventRepository(authRepository, bus)
	authService, err := appauth.NewService(
		appauth.Config{
			ChallengeTTL:            cfg.App.SessionChallengeTTL,
			EventRetention:          cfg.App.EventRetention,
			EventBatchSize:          cfg.App.EventBatchSize,
			EventRedeliveryCooldown: cfg.App.EventRedeliveryCooldown,
		},
		clock.Real{},
		uuidx.DefaultGenerator{},
		randombytes.CryptoReader{},
		txManager,
		authRepository,
		authRepository,
		notifyingEvents,
	)
	if err != nil {
		logger.Error("failed to initialize auth service", "error", err)
		os.Exit(1)
	}
	logger.Info("auth service initialized")

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
		notifyingEvents,
		authService,
	)
	if err != nil {
		logger.Error("failed to initialize client api service", "error", err)
		os.Exit(1)
	}
	logger.Info("client api service initialized")

	clientHandler := clientrpc.NewHandler(logger, authService, clientService, outboxService, bus)
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
	go func() {
		if runErr := outboxService.RunDispatcher(ctx); runErr != nil && ctx.Err() == nil {
			runtimeErrCh <- runErr
		}
	}()
	go func() {
		if runErr := outboxService.RunJanitor(ctx); runErr != nil && ctx.Err() == nil {
			runtimeErrCh <- runErr
		}
	}()
	go func() {
		if runErr := pushNotifier.Run(ctx); runErr != nil && ctx.Err() == nil {
			runtimeErrCh <- runErr
		}
	}()

	logger.Info(
		"server runtime configured",
		"service", cfg.App.Name,
		"host", cfg.App.Host,
		"tcp_port", cfg.App.Ports.TCPPort,
		"http_port", cfg.App.Ports.HTTPPort,
		"ws_port", cfg.App.Ports.WSPort,
		"output_tcp_port", cfg.App.OutputPorts.TCPPort,
		"output_tcp_tls_port", cfg.App.OutputPorts.TCPTLSPort,
		"output_http_port", cfg.App.OutputPorts.HTTPPort,
		"output_https_port", cfg.App.OutputPorts.HTTPSPort,
		"output_ws_port", cfg.App.OutputPorts.WSPort,
		"output_wss_port", cfg.App.OutputPorts.WSSPort,
		"postgres_host", cfg.Postgres.Host,
		"postgres_port", cfg.Postgres.Port,
		"redis_host", cfg.Redis.Host,
		"redis_port", cfg.Redis.Port,
	)

	select {
	case runErr := <-runtimeErrCh:
		logger.Error("runtime failed", "error", runErr)
		stop()
	case <-ctx.Done():
	}

	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if shutdownErr := runtime.Shutdown(shutdownCtx); shutdownErr != nil {
		logger.Error("graceful shutdown failed", "error", shutdownErr)
		os.Exit(1)
	}

	// Give in-flight logs a small chance to flush in container runtimes.
	time.Sleep(100 * time.Millisecond)
	logger.Info("server stopped")
}
