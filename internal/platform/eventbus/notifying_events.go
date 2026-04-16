package eventbus

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainauth "server_v2/internal/domain/auth"
)

// EventRepository is the minimal interface needed by auth.Service.
type EventRepository interface {
	Append(ctx context.Context, event domainauth.Event) error
	ListPending(ctx context.Context, userPublicKey []byte, now time.Time, redeliverBefore time.Time, limit int) ([]domainauth.Event, error)
	MarkDelivered(ctx context.Context, eventIDs []uuid.UUID, deliveredAt time.Time) error
	Acknowledge(ctx context.Context, userPublicKey []byte, eventID uuid.UUID) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// NotifyingEventRepository decorates an EventRepository and notifies the bus
// after a successful Append.
type NotifyingEventRepository struct {
	inner EventRepository
	bus   *Bus
}

func NewNotifyingEventRepository(inner EventRepository, bus *Bus) *NotifyingEventRepository {
	return &NotifyingEventRepository{inner: inner, bus: bus}
}

func (r *NotifyingEventRepository) Append(ctx context.Context, event domainauth.Event) error {
	if err := r.inner.Append(ctx, event); err != nil {
		return err
	}
	if r.bus != nil && len(event.UserPublicKey) > 0 {
		r.bus.Notify(event.UserPublicKey)
	}
	return nil
}

func (r *NotifyingEventRepository) ListPending(ctx context.Context, userPublicKey []byte, now time.Time, redeliverBefore time.Time, limit int) ([]domainauth.Event, error) {
	return r.inner.ListPending(ctx, userPublicKey, now, redeliverBefore, limit)
}

func (r *NotifyingEventRepository) MarkDelivered(ctx context.Context, eventIDs []uuid.UUID, deliveredAt time.Time) error {
	return r.inner.MarkDelivered(ctx, eventIDs, deliveredAt)
}

func (r *NotifyingEventRepository) Acknowledge(ctx context.Context, userPublicKey []byte, eventID uuid.UUID) error {
	return r.inner.Acknowledge(ctx, userPublicKey, eventID)
}

func (r *NotifyingEventRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	return r.inner.DeleteExpired(ctx, now)
}
