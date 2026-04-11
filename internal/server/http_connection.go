package server

import (
	"context"
	"net"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

type httpConnectionIDKey struct{}

type HTTPConnectionObserver interface {
	OnHTTPConnectionClosed(connectionID string)
}

type HTTPConnectionBinder struct {
	observer HTTPConnectionObserver

	mu      sync.Mutex
	connIDs map[net.Conn]string
}

func NewHTTPConnectionBinder(observer HTTPConnectionObserver) *HTTPConnectionBinder {
	return &HTTPConnectionBinder{
		observer: observer,
		connIDs:  make(map[net.Conn]string),
	}
}

func (b *HTTPConnectionBinder) ConnContext(ctx context.Context, conn net.Conn) context.Context {
	if b == nil {
		return ctx
	}

	connectionID := uuid.NewString()

	b.mu.Lock()
	b.connIDs[conn] = connectionID
	b.mu.Unlock()

	return context.WithValue(ctx, httpConnectionIDKey{}, connectionID)
}

func (b *HTTPConnectionBinder) ConnState(conn net.Conn, state http.ConnState) {
	if b == nil {
		return
	}

	if state != http.StateClosed && state != http.StateHijacked {
		return
	}

	b.mu.Lock()
	connectionID, ok := b.connIDs[conn]
	if ok {
		delete(b.connIDs, conn)
	}
	b.mu.Unlock()

	if ok && b.observer != nil {
		b.observer.OnHTTPConnectionClosed(connectionID)
	}
}

func HTTPConnectionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	connectionID, ok := ctx.Value(httpConnectionIDKey{}).(string)
	if !ok || connectionID == "" {
		return "", false
	}

	return connectionID, true
}
