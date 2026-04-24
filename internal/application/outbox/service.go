package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	domaintx "server_v2/internal/domain/tx"
)

const (
	StatusPending  int16 = 0
	StatusInFlight int16 = 1
	StatusAcked    int16 = 2
	StatusDropped  int16 = 3
)

var ErrInvalidDeviceID = errors.New("invalid device id")

type Clock interface {
	Now() time.Time
}

type Notifier interface {
	NotifyKey(key string)
	NotifyOutboxEvent(event Event)
}

type Repository interface {
	ClaimPending(ctx context.Context, now time.Time, batchSize int, ackTimeout time.Duration, maxAttempts int) ([]Event, error)
	ListInflight(ctx context.Context, deviceID string, now time.Time, limit int) ([]Event, error)
	AcknowledgeOutbox(ctx context.Context, now time.Time, eventID uuid.UUID, deviceID string, segmentID string) error
	DropExpiredHeads(ctx context.Context, now time.Time, batchSize int) (int, error)
	DeleteTerminal(ctx context.Context, status int16, olderThan time.Time, limit int) (int64, error)
}

type Config struct {
	PollInterval      time.Duration
	BatchSizeSegments int
	AckTimeout        time.Duration
	MaxAttempts       int
	JanitorInterval   time.Duration
	DeleteBatchSize   int
	AckRetention      time.Duration
	DropRetention     time.Duration
}

type Event struct {
	EventID       uuid.UUID
	DeviceID      string
	SegmentID     string
	EventType     string
	Payload       map[string]any
	CreatedAt     time.Time
	ExpiresAt     time.Time
	InflightUntil *time.Time
	LastSentAt    *time.Time
	Attempts      int
}

type Service struct {
	cfg       Config
	clock     Clock
	txManager domaintx.Manager
	repo      Repository
	notifier  Notifier
}

func NewService(cfg Config, clock Clock, txManager domaintx.Manager, repo Repository, notifier Notifier) (*Service, error) {
	if clock == nil || txManager == nil || repo == nil {
		return nil, fmt.Errorf("all dependencies are required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 300 * time.Millisecond
	}
	if cfg.BatchSizeSegments <= 0 {
		cfg.BatchSizeSegments = 32
	}
	if cfg.AckTimeout <= 0 {
		cfg.AckTimeout = 15 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.JanitorInterval <= 0 {
		cfg.JanitorInterval = 2 * time.Minute
	}
	if cfg.DeleteBatchSize <= 0 {
		cfg.DeleteBatchSize = 500
	}
	if cfg.AckRetention <= 0 {
		cfg.AckRetention = 24 * time.Hour
	}
	if cfg.DropRetention <= 0 {
		cfg.DropRetention = 24 * time.Hour
	}
	return &Service{cfg: cfg, clock: clock, txManager: txManager, repo: repo, notifier: notifier}, nil
}

func (s *Service) DispatchOnce(ctx context.Context) (int, error) {
	now := s.clock.Now()
	var events []Event
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		events, err = s.repo.ClaimPending(txCtx, now, s.cfg.BatchSizeSegments, s.cfg.AckTimeout, s.cfg.MaxAttempts)
		return err
	}); err != nil {
		return 0, err
	}
	for _, event := range events {
		if s.notifier != nil {
			s.notifier.NotifyOutboxEvent(event)
			s.notifier.NotifyKey(event.DeviceID)
		}
	}
	return len(events), nil
}

func (s *Service) ListInflight(ctx context.Context, deviceID string, limit int) ([]Event, error) {
	if deviceID == "" {
		return nil, ErrInvalidDeviceID
	}
	if limit <= 0 {
		limit = s.cfg.BatchSizeSegments
	}
	return s.repo.ListInflight(ctx, deviceID, s.clock.Now(), limit)
}

func (s *Service) Acknowledge(ctx context.Context, eventID uuid.UUID, deviceID string, segmentID string) error {
	if eventID == uuid.Nil {
		return fmt.Errorf("invalid event id")
	}
	if deviceID == "" {
		return ErrInvalidDeviceID
	}
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.AcknowledgeOutbox(txCtx, s.clock.Now(), eventID, deviceID, segmentID)
	}); err != nil {
		return err
	}
	if s.notifier != nil {
		s.notifier.NotifyKey(deviceID)
	}
	return nil
}

func (s *Service) RunDispatcher(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if _, err := s.DispatchOnce(ctx); err != nil && ctx.Err() == nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunJanitor(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.JanitorInterval)
	defer ticker.Stop()

	run := func() error {
		now := s.clock.Now()
		if _, err := s.repo.DropExpiredHeads(ctx, now, s.cfg.BatchSizeSegments); err != nil {
			return err
		}
		if _, err := s.repo.DeleteTerminal(ctx, StatusAcked, now.Add(-s.cfg.AckRetention), s.cfg.DeleteBatchSize); err != nil {
			return err
		}
		if _, err := s.repo.DeleteTerminal(ctx, StatusDropped, now.Add(-s.cfg.DropRetention), s.cfg.DeleteBatchSize); err != nil {
			return err
		}
		return nil
	}

	for {
		if err := run(); err != nil && ctx.Err() == nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
